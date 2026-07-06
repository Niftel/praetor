package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
)

// orgUserCols is the deliberately reduced user projection returned by the
// org-membership listings — no password_hash and no LDAP bookkeeping fields.
const orgUserCols = `u.id, u.username, u.first_name, u.last_name, u.email,
	u.is_superuser, u.is_system_auditor, u.is_active, u.created_at, u.modified_at`

// OrgStore is the data-access layer for the organizations domain, including the
// org-scoped sub-listings (teams/projects/inventories/members) the org handlers
// expose.
type OrgStore struct {
	db *sqlx.DB
}

func NewOrgStore(db *sqlx.DB) *OrgStore { return &OrgStore{db: db} }

// ListAll returns a page of all organizations.
func (s *OrgStore) ListAll(ctx context.Context, limit, offset int) ([]models.Organization, error) {
	orgs := []models.Organization{}
	err := s.db.SelectContext(ctx, &orgs, `SELECT `+OrganizationCols+` FROM organizations ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
	return orgs, err
}

// CountAll returns the total number of organizations.
func (s *OrgStore) CountAll(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.GetContext(ctx, &total, "SELECT count(*) FROM organizations")
	return total, err
}

// ListByIDs returns a page of the organizations whose id is in ids.
func (s *OrgStore) ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]models.Organization, error) {
	orgs := []models.Organization{}
	if len(ids) == 0 {
		return orgs, nil
	}
	q, args, err := sqlx.In(`SELECT `+OrganizationCols+` FROM organizations WHERE id IN (?) ORDER BY id LIMIT ? OFFSET ?`, ids, limit, offset)
	if err != nil {
		return nil, err
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &orgs, q, args...)
	return orgs, err
}

// Get returns a single organization by id.
func (s *OrgStore) Get(ctx context.Context, id int64) (models.Organization, error) {
	var org models.Organization
	err := s.db.GetContext(ctx, &org, "SELECT "+OrganizationCols+" FROM organizations WHERE id = $1", id)
	return org, err
}

// Create inserts an organization and returns the persisted row.
func (s *OrgStore) Create(ctx context.Context, input models.Organization) (models.Organization, error) {
	query := `
		INSERT INTO organizations (name, description)
		VALUES (:name, :description)
		RETURNING ` + OrganizationCols
	return s.namedReturning(query, input)
}

// Update applies an edit to an organization (input.ID must be set) and returns it.
func (s *OrgStore) Update(ctx context.Context, input models.Organization) (models.Organization, error) {
	query := `
		UPDATE organizations
		SET name=:name, description=:description, modified_at=NOW()
		WHERE id=:id
		RETURNING ` + OrganizationCols
	return s.namedReturning(query, input)
}

// namedReturning runs a NamedQuery that RETURNs one organization row.
func (s *OrgStore) namedReturning(query string, arg models.Organization) (models.Organization, error) {
	var out models.Organization
	rows, err := s.db.NamedQuery(query, arg)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.StructScan(&out); err != nil {
			return out, err
		}
		return out, nil
	}
	return out, sql.ErrNoRows
}

// Delete removes an organization by id, returning the number of rows affected
// (0 means it did not exist).
func (s *OrgStore) Delete(ctx context.Context, id int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM organizations WHERE id = $1", id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UsersByRoleField returns the users holding a given org role_field (e.g.
// member_role, admin_role) for the organization.
func (s *OrgStore) UsersByRoleField(ctx context.Context, orgID int64, roleField string) ([]models.User, error) {
	users := []models.User{}
	err := s.db.SelectContext(ctx, &users, `
		SELECT DISTINCT `+orgUserCols+`
		FROM users u
		JOIN role_members rm ON u.id = rm.user_id
		JOIN roles r ON rm.role_id = r.id
		WHERE r.content_type = 'organization' AND r.object_id = $1 AND r.role_field = $2`, orgID, roleField)
	return users, err
}

// ListTeams returns an organization's teams.
func (s *OrgStore) ListTeams(ctx context.Context, orgID int64) ([]models.Team, error) {
	teams := []models.Team{}
	err := s.db.SelectContext(ctx, &teams, `SELECT `+TeamCols+` FROM teams WHERE organization_id = $1 ORDER BY id`, orgID)
	return teams, err
}

// ListProjects returns an organization's projects.
func (s *OrgStore) ListProjects(ctx context.Context, orgID int64) ([]models.Project, error) {
	projects := []models.Project{}
	err := s.db.SelectContext(ctx, &projects, `SELECT `+ProjectCols+` FROM projects WHERE organization_id = $1 ORDER BY id`, orgID)
	return projects, err
}

// ListInventories returns an organization's inventories.
func (s *OrgStore) ListInventories(ctx context.Context, orgID int64) ([]models.Inventory, error) {
	inventories := []models.Inventory{}
	err := s.db.SelectContext(ctx, &inventories, `SELECT `+InventoryCols+` FROM inventories WHERE organization_id = $1 ORDER BY id`, orgID)
	return inventories, err
}

// OrgGalaxyCredential is a Galaxy credential attached to an organization.
type OrgGalaxyCredential struct {
	ID           int64  `json:"id" db:"id"`
	CredentialID int64  `json:"credential_id" db:"credential_id"`
	Name         string `json:"name" db:"name"`
	Position     int    `json:"position" db:"position"`
}

// GalaxyCredentials lists an organization's Galaxy credentials, in order.
func (s *OrgStore) GalaxyCredentials(ctx context.Context, orgID int64) ([]OrgGalaxyCredential, error) {
	creds := []OrgGalaxyCredential{}
	err := s.db.SelectContext(ctx, &creds, `
		SELECT ogc.id, ogc.credential_id, c.name, ogc.position
		FROM organization_galaxy_credentials ogc
		JOIN credentials c ON c.id = ogc.credential_id
		WHERE ogc.organization_id = $1
		ORDER BY ogc.position, ogc.id`, orgID)
	return creds, err
}

// AddGalaxyCredential attaches (or repositions) a Galaxy credential on an org.
func (s *OrgStore) AddGalaxyCredential(ctx context.Context, orgID, credentialID int64, position int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO organization_galaxy_credentials (organization_id, credential_id, position)
		VALUES ($1, $2, $3)
		ON CONFLICT (organization_id, credential_id) DO UPDATE SET position = EXCLUDED.position`,
		orgID, credentialID, position)
	return err
}

// RemoveGalaxyCredential detaches a Galaxy credential from an org.
func (s *OrgStore) RemoveGalaxyCredential(ctx context.Context, orgID, credentialID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM organization_galaxy_credentials WHERE organization_id = $1 AND credential_id = $2`, orgID, credentialID)
	return err
}
