package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
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
	}
	return nil
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

func grantRole(ctx context.Context, tx *sqlx.Tx, contentType string, objectID int64, roleField string, userID int64) error {
	var roleID int64
	if err := tx.GetContext(ctx, &roleID,
		`SELECT id FROM roles WHERE content_type=$1 AND object_id=$2 AND role_field=$3`,
		contentType, objectID, roleField); err != nil {
		return fmt.Errorf("lookup role %s/%d/%s: %w", contentType, objectID, roleField, err)
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO role_members (role_id, user_id) VALUES ($1, $2) ON CONFLICT (role_id, user_id) DO NOTHING`,
		roleID, userID)
	return err
}

func revokeRole(ctx context.Context, tx *sqlx.Tx, contentType string, objectID int64, roleField string, userID int64) error {
	var roleID int64
	err := tx.GetContext(ctx, &roleID,
		`SELECT id FROM roles WHERE content_type=$1 AND object_id=$2 AND role_field=$3`,
		contentType, objectID, roleField)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM role_members WHERE role_id=$1 AND user_id=$2`, roleID, userID)
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
