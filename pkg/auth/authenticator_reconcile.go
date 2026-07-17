package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

var ErrAuthenticatorMapDenied = errors.New("access denied by authenticator mapping")

type mappingDecision struct {
	grant AuthenticatorGrant
	on    bool
}

// evaluateAuthenticatorMaps applies rules in ascending order. Later rules for
// the same target replace earlier decisions. A non-match is a no-op unless the
// rule is authoritative (revoke=true), in which case it produces a denial for
// that target. Login remains allowed unless an allow rule explicitly denies it.
func evaluateAuthenticatorMaps(maps []AuthenticatorMap, claims IdentityClaims) (bool, map[string]mappingDecision) {
	allow := true
	decisions := map[string]mappingDecision{}
	for _, rule := range sortedAuthenticatorMaps(maps) {
		matched := rule.When.Matches(claims)
		if rule.Map.Type == MapAllow {
			if matched {
				allow = true
			} else if rule.Revoke {
				allow = false
			}
			continue
		}
		if !matched && !rule.Revoke {
			continue
		}
		decisions[grantKey(rule.Map)] = mappingDecision{grant: rule.Map, on: matched}
	}
	return allow, decisions
}

func grantKey(g AuthenticatorGrant) string {
	return g.Type + "\x00" + g.Organization + "\x00" + g.Team + "\x00" + g.Role
}

func applyAuthenticatorMaps(ctx context.Context, tx *sqlx.Tx, cfg *LDAPConfig, userID int64, claims IdentityClaims) error {
	_, decisions := evaluateAuthenticatorMaps(cfg.AuthenticatorMaps, claims)
	for _, decision := range decisions {
		g := decision.grant
		switch g.Type {
		case MapSuperuser:
			if _, err := tx.ExecContext(ctx, `UPDATE users SET is_superuser=$2, modified_at=NOW() WHERE id=$1`, userID, decision.on); err != nil {
				return err
			}
			if err := syncSystemRole(ctx, tx, "System Administrator", userID, decision.on); err != nil {
				return err
			}
		case MapRole:
			if g.Role != "System Auditor" {
				return fmt.Errorf("unsupported global role %q", g.Role)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET is_system_auditor=$2, modified_at=NOW() WHERE id=$1`, userID, decision.on); err != nil {
				return err
			}
			if err := syncSystemRole(ctx, tx, "System Auditor", userID, decision.on); err != nil {
				return err
			}
		case MapOrganization:
			orgID, ok, err := mappingOrganization(ctx, tx, g.Organization, decision.on)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			field := map[string]string{"Organization Admin": "admin_role", "Organization Member": "member_role", "Organization Auditor": "auditor_role"}[g.Role]
			if decision.on {
				err = grantRole(ctx, tx, "organization", orgID, field, userID)
			} else {
				err = revokeRole(ctx, tx, "organization", orgID, field, userID)
			}
			if err != nil {
				return fmt.Errorf("authenticator map organization %q: %w", g.Organization, err)
			}
		case MapTeam:
			orgID, ok, err := mappingOrganization(ctx, tx, g.Organization, decision.on)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			teamID, ok, err := mappingTeam(ctx, tx, orgID, g.Team, decision.on)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			field := map[string]string{"Team Admin": "admin_role", "Team Member": "member_role"}[g.Role]
			if decision.on {
				err = grantRole(ctx, tx, "team", teamID, field, userID)
			} else {
				err = revokeRole(ctx, tx, "team", teamID, field, userID)
			}
			if err != nil {
				return fmt.Errorf("authenticator map team %q/%q: %w", g.Organization, g.Team, err)
			}
			// team_members is the canonical membership relation used by team
			// principal RBAC and team-scoped workflow approvals. Keep it in sync
			// with LDAP-created Team Member / Team Admin assignments.
			if decision.on {
				if _, err := tx.ExecContext(ctx, `INSERT INTO team_members (team_id,user_id)
					VALUES ($1,$2) ON CONFLICT (team_id,user_id) DO NOTHING`, teamID, userID); err != nil {
					return fmt.Errorf("authenticator map team membership %q/%q: %w", g.Organization, g.Team, err)
				}
			} else {
				if _, err := tx.ExecContext(ctx, `DELETE FROM team_members tm
					WHERE tm.team_id=$1 AND tm.user_id=$2 AND NOT EXISTS (
						SELECT 1 FROM role_user_assignments rua
						JOIN object_roles orl ON orl.id=rua.object_role_id
						JOIN role_definitions rd ON rd.id=rua.role_definition_id
						WHERE rua.user_id=$2 AND orl.content_type='team' AND orl.object_id=$1
						  AND rd.name IN ('Team Member','Team Admin')
					)`, teamID, userID); err != nil {
					return fmt.Errorf("authenticator unmap team membership %q/%q: %w", g.Organization, g.Team, err)
				}
			}
		}
	}
	return nil
}

func mappingOrganization(ctx context.Context, tx *sqlx.Tx, name string, create bool) (int64, bool, error) {
	var id int64
	err := tx.GetContext(ctx, &id, `SELECT id FROM organizations WHERE name=$1`, name)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	if !create {
		return 0, false, nil
	}
	id, err = selectOrCreateOrg(ctx, tx, name)
	return id, err == nil, err
}

func mappingTeam(ctx context.Context, tx *sqlx.Tx, orgID int64, name string, create bool) (int64, bool, error) {
	var id int64
	err := tx.GetContext(ctx, &id, `SELECT id FROM teams WHERE organization_id=$1 AND name=$2`, orgID, name)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	if !create {
		return 0, false, nil
	}
	id, err = selectOrCreateTeam(ctx, tx, orgID, name)
	return id, err == nil, err
}
