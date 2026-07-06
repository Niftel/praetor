package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
)

// ProjectStore is the data-access layer for the projects domain.
type ProjectStore struct {
	db *sqlx.DB
}

func NewProjectStore(db *sqlx.DB) *ProjectStore { return &ProjectStore{db: db} }

// ListAll returns a page of all projects.
func (s *ProjectStore) ListAll(ctx context.Context, limit, offset int) ([]models.Project, error) {
	projects := []models.Project{}
	err := s.db.SelectContext(ctx, &projects, `SELECT `+ProjectCols+` FROM projects ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
	return projects, wrap("ProjectStore.ListAll", err)
}

// CountAll returns the total number of projects.
func (s *ProjectStore) CountAll(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.GetContext(ctx, &total, "SELECT count(*) FROM projects")
	return total, wrap("ProjectStore.CountAll", err)
}

// ListByIDs returns a page of the projects whose id is in ids.
func (s *ProjectStore) ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]models.Project, error) {
	projects := []models.Project{}
	if len(ids) == 0 {
		return projects, nil
	}
	q, args, err := sqlx.In(`SELECT `+ProjectCols+` FROM projects WHERE id IN (?) ORDER BY id LIMIT ? OFFSET ?`, ids, limit, offset)
	if err != nil {
		return nil, wrap("ProjectStore.ListByIDs", err)
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &projects, q, args...)
	return projects, wrap("ProjectStore.ListByIDs", err)
}

// Get returns a single project by id.
func (s *ProjectStore) Get(ctx context.Context, id int64) (models.Project, error) {
	var project models.Project
	err := s.db.GetContext(ctx, &project, "SELECT "+ProjectCols+" FROM projects WHERE id = $1", id)
	return project, wrap("ProjectStore.Get", err)
}

// Create inserts a project and returns the persisted row.
func (s *ProjectStore) Create(ctx context.Context, input models.Project) (models.Project, error) {
	query := `
		INSERT INTO projects (organization_id, name, description, scm_type, scm_url, scm_branch)
		VALUES (:organization_id, :name, :description, :scm_type, :scm_url, :scm_branch)
		RETURNING ` + ProjectCols
	var created models.Project
	rows, err := s.db.NamedQuery(query, input)
	if err != nil {
		return created, wrap("ProjectStore.Create", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.StructScan(&created); err != nil {
			return created, wrap("ProjectStore.Create", err)
		}
		return created, nil
	}
	return created, sql.ErrNoRows
}

// TouchModified stamps modified_at to signal a completed SCM sync.
func (s *ProjectStore) TouchModified(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE projects SET modified_at = NOW() WHERE id = $1", id)
	return wrap("ProjectStore.TouchModified", err)
}
