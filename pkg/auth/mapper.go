package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
)

// UserIdentity is what an LDAP bind + group resolution yields for one user.
type UserIdentity struct {
	DN        string
	Username  string
	Email     string
	FirstName string
	LastName  string
	Custom    map[string]string   // user_attr_map custom attributes
	Groups    map[string]struct{} // NORMALIZED group DNs the user belongs to
}

// GroupResolver authenticates a user against the directory and returns their
// identity + group membership. Implemented by *LDAPClient for real LDAP; a fake
// implementation is used in tests so no live server is needed.
type GroupResolver interface {
	AuthenticateAndResolve(username, password string) (*UserIdentity, error)
}

// ErrInvalidCredentials is returned when the directory rejects the bind.
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrLocalAccount is returned when an LDAP login collides with an existing
// local-password account (a break-glass superuser). Such rows are never
// LDAP-managed; the caller must not treat this as a successful LDAP login.
var ErrLocalAccount = errors.New("username belongs to a local (non-LDAP) account")

// Authenticate performs the AAP/AWX login flow: bind to LDAP (via the resolver),
// then apply user_flags_by_group + organization_map + team_map inside one tx,
// creating orgs/teams as needed and reconciling role grants. Returns the persisted
// user for JWT issuance. All LDAP I/O happens before the tx opens.
func Authenticate(ctx context.Context, db *sqlx.DB, cfg *LDAPConfig, resolver GroupResolver, username, password string) (*models.User, error) {
	id, err := resolver.AuthenticateAndResolve(username, password)
	if err != nil {
		return nil, err
	}
	if id.Groups == nil {
		id.Groups = map[string]struct{}{}
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	userID, err := upsertLDAPUser(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := applyUserFlags(ctx, tx, cfg, userID, id.Groups); err != nil {
		return nil, err
	}
	if err := applyOrganizationMap(ctx, tx, cfg, userID, id.Groups); err != nil {
		return nil, err
	}
	if err := applyTeamMap(ctx, tx, cfg, userID, id.Groups); err != nil {
		return nil, err
	}

	var u models.User
	if err := tx.GetContext(ctx, &u,
		`SELECT id, username, first_name, last_name, email, is_superuser, is_system_auditor, is_active FROM users WHERE id=$1`,
		userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &u, nil
}

// upsertLDAPUser creates or updates the user row from the LDAP identity. It refuses
// to attach ldap_dn to a break-glass local account (non-empty password_hash and
// ldap_dn IS NULL), returning ErrLocalAccount.
func upsertLDAPUser(ctx context.Context, tx *sqlx.Tx, id *UserIdentity) (int64, error) {
	var meta []byte
	if len(id.Custom) > 0 {
		meta, _ = json.Marshal(id.Custom)
	}

	var existing struct {
		ID     int64   `db:"id"`
		Hash   string  `db:"password_hash"`
		LdapDN *string `db:"ldap_dn"`
	}
	err := tx.GetContext(ctx, &existing,
		`SELECT id, password_hash, ldap_dn FROM users WHERE username=$1`, id.Username)
	if errors.Is(err, sql.ErrNoRows) {
		var newID int64
		insErr := tx.GetContext(ctx, &newID, `
			INSERT INTO users (username, password_hash, email, first_name, last_name, ldap_dn, ldap_synced_at, ldap_metadata)
			VALUES ($1, '', $2, $3, $4, $5, NOW(), $6)
			RETURNING id`,
			id.Username, nullString(id.Email), nullString(id.FirstName), nullString(id.LastName), id.DN, nullBytes(meta))
		return newID, insErr
	}
	if err != nil {
		return 0, err
	}
	// Break-glass guard: a row with a local password and no ldap_dn is a local
	// account and is never taken over by LDAP.
	if existing.Hash != "" && existing.LdapDN == nil {
		return 0, ErrLocalAccount
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE users
		SET email=$2, first_name=$3, last_name=$4, ldap_dn=$5, ldap_synced_at=NOW(), ldap_metadata=$6, modified_at=NOW()
		WHERE id=$1`,
		existing.ID, nullString(id.Email), nullString(id.FirstName), nullString(id.LastName), id.DN, nullBytes(meta))
	return existing.ID, err
}

// applyUserFlags sets is_superuser / is_system_auditor from group membership, but
// only when the mapping is configured (unset ≠ false — never demote a manually
// promoted user just because no group mapping exists).
func applyUserFlags(ctx context.Context, tx *sqlx.Tx, cfg *LDAPConfig, userID int64, groups map[string]struct{}) error {
	if v, assign := cfg.UserFlags.IsSuperuser.Resolve(groups); assign {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET is_superuser=$2, modified_at=NOW() WHERE id=$1`, userID, v); err != nil {
			return err
		}
	}
	if v, assign := cfg.UserFlags.IsSystemAuditor.Resolve(groups); assign {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET is_system_auditor=$2, modified_at=NOW() WHERE id=$1`, userID, v); err != nil {
			return err
		}
	}
	return nil
}

// applyOrganizationMap grants/revokes org admin/member/auditor roles per the map.
func applyOrganizationMap(ctx context.Context, tx *sqlx.Tx, cfg *LDAPConfig, userID int64, groups map[string]struct{}) error {
	for name, entry := range cfg.OrganizationMap {
		orgID, err := selectOrCreateOrg(ctx, tx, name)
		if err != nil {
			return fmt.Errorf("organization_map %q: %w", name, err)
		}
		bindings := []struct {
			match  GroupMatch
			remove bool
			field  string
		}{
			{entry.Admins, entry.RemoveAdmins, "admin_role"},
			{entry.Users, entry.RemoveUsers, "member_role"},
			{entry.Auditors, entry.RemoveAuditors, "auditor_role"},
		}
		for _, b := range bindings {
			grant, revoke := decideRole(b.match.Matches(groups), b.match.Configured(), b.remove)
			if grant {
				if err := grantRole(ctx, tx, "organization", orgID, b.field, userID); err != nil {
					return fmt.Errorf("organization_map %q %s: %w", name, b.field, err)
				}
			}
			if revoke {
				if err := revokeRole(ctx, tx, "organization", orgID, b.field, userID); err != nil {
					return fmt.Errorf("organization_map %q %s: %w", name, b.field, err)
				}
			}
		}

		// Named RoleDefinition bindings (#98): bind a directory group to any capability
		// role, scoped to this org. Resolve the definition whenever configured so a
		// mistyped role name fails loudly on login rather than silently no-op'ing.
		for roleName, match := range entry.Roles {
			if !match.Configured() {
				continue
			}
			defID, err := resolveRoleDefinition(ctx, tx, roleName)
			if err != nil {
				return fmt.Errorf("organization_map %q roles %q: %w", name, roleName, err)
			}
			grant, revoke := decideRole(match.Matches(groups), true, entry.RemoveRoles)
			if grant {
				if err := grantRoleDefinition(ctx, tx, orgID, defID, userID); err != nil {
					return fmt.Errorf("organization_map %q roles %q: %w", name, roleName, err)
				}
			}
			if revoke {
				if err := revokeRoleDefinition(ctx, tx, orgID, defID, userID); err != nil {
					return fmt.Errorf("organization_map %q roles %q: %w", name, roleName, err)
				}
			}
		}
	}
	return nil
}

// resolveRoleDefinition looks up a RoleDefinition id by name, returning a clear error if
// it does not exist so a misconfigured organization_map fails loudly.
func resolveRoleDefinition(ctx context.Context, tx *sqlx.Tx, name string) (int64, error) {
	var id int64
	err := tx.GetContext(ctx, &id, `SELECT id FROM role_definitions WHERE name = $1`, name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("role definition %q does not exist", name)
	}
	return id, err
}

// grantRoleDefinition assigns a user a RoleDefinition scoped to an organization,
// creating the object_role if needed and refreshing the evaluation cache. Idempotent.
func grantRoleDefinition(ctx context.Context, tx *sqlx.Tx, orgID, defID, userID int64) error {
	orID, err := ensureOrgObjectRole(ctx, tx, defID, orgID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO role_user_assignments (role_definition_id, user_id, object_role_id)
		VALUES ($1, $2, $3) ON CONFLICT (user_id, object_role_id) DO NOTHING`, defID, userID, orID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `SELECT rebuild_object_role_evaluations($1)`, orID)
	return err
}

// revokeRoleDefinition removes a user's org-scoped assignment of a RoleDefinition. The
// object_role and its evaluation rows are left in place (shared with other actors).
func revokeRoleDefinition(ctx context.Context, tx *sqlx.Tx, orgID, defID, userID int64) error {
	var orID int64
	err := tx.GetContext(ctx, &orID,
		`SELECT id FROM object_roles WHERE role_definition_id = $1 AND content_type = 'organization' AND object_id = $2`,
		defID, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // nothing assigned
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`DELETE FROM role_user_assignments WHERE object_role_id = $1 AND user_id = $2`, orID, userID)
	return err
}

// ensureOrgObjectRole returns the object_role id for (definition, organization), creating
// it if absent.
func ensureOrgObjectRole(ctx context.Context, tx *sqlx.Tx, defID, orgID int64) (int64, error) {
	var orID int64
	err := tx.GetContext(ctx, &orID,
		`SELECT id FROM object_roles WHERE role_definition_id = $1 AND content_type = 'organization' AND object_id = $2`,
		defID, orgID)
	if err == nil {
		return orID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = tx.GetContext(ctx, &orID,
		`INSERT INTO object_roles (role_definition_id, content_type, object_id) VALUES ($1, 'organization', $2) RETURNING id`,
		defID, orgID)
	return orID, err
}

// applyTeamMap grants/revokes team member_role per the map, resolving each team
// through its organization (never name-only).
func applyTeamMap(ctx context.Context, tx *sqlx.Tx, cfg *LDAPConfig, userID int64, groups map[string]struct{}) error {
	for name, entry := range cfg.TeamMap {
		if entry.Organization == "" {
			return fmt.Errorf("team_map %q: organization is required", name)
		}
		orgID, err := selectOrCreateOrg(ctx, tx, entry.Organization)
		if err != nil {
			return fmt.Errorf("team_map %q org %q: %w", name, entry.Organization, err)
		}
		teamID, err := selectOrCreateTeam(ctx, tx, orgID, name)
		if err != nil {
			return fmt.Errorf("team_map %q: %w", name, err)
		}
		grant, revoke := decideRole(entry.Users.Matches(groups), entry.Users.Configured(), entry.Remove)
		if grant {
			if err := grantRole(ctx, tx, "team", teamID, "member_role", userID); err != nil {
				return fmt.Errorf("team_map %q: %w", name, err)
			}
		}
		if revoke {
			if err := revokeRole(ctx, tx, "team", teamID, "member_role", userID); err != nil {
				return fmt.Errorf("team_map %q: %w", name, err)
			}
		}
	}
	return nil
}

func selectOrCreateOrg(ctx context.Context, tx *sqlx.Tx, name string) (int64, error) {
	var id int64
	err := tx.GetContext(ctx, &id, `SELECT id FROM organizations WHERE name=$1`, name)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	// Insert; ON CONFLICT handles a concurrent first-login race (unique name).
	err = tx.GetContext(ctx, &id,
		`INSERT INTO organizations (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, name)
	return id, err
}

func selectOrCreateTeam(ctx context.Context, tx *sqlx.Tx, orgID int64, name string) (int64, error) {
	var id int64
	err := tx.GetContext(ctx, &id, `SELECT id FROM teams WHERE organization_id=$1 AND name=$2`, orgID, name)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = tx.GetContext(ctx, &id,
		`INSERT INTO teams (organization_id, name) VALUES ($1, $2) ON CONFLICT (organization_id, name) DO UPDATE SET name=EXCLUDED.name RETURNING id`,
		orgID, name)
	return id, err
}

// grantRole grants a user the RoleDefinition mirroring a legacy org/team role_field,
// scoped to the object, via the capability assignment tables.
func grantRole(ctx context.Context, tx *sqlx.Tx, contentType string, objectID int64, roleField string, userID int64) error {
	_, err := rbac.GrantCapabilityForLegacyFields(ctx, tx, contentType, objectID, roleField, userID, true)
	return err
}

// revokeRole is grantRole's inverse.
func revokeRole(ctx context.Context, tx *sqlx.Tx, contentType string, objectID int64, roleField string, userID int64) error {
	_, err := rbac.RevokeCapabilityForLegacyFields(ctx, tx, contentType, objectID, roleField, userID, true)
	return err
}

func nullBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}

// nullString maps "" to a SQL NULL so empty attributes aren't stored as empty
// strings.
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
