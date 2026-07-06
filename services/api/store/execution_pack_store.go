package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
)

// ExecutionPack is a registered Execution Pack (webhook_key is write-only and
// never populated on read).
type ExecutionPack struct {
	ID          int64     `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description *string   `json:"description,omitempty" db:"description"`
	Spec        *string   `json:"spec,omitempty" db:"spec"`
	Status      string    `json:"status" db:"status"`
	BuildLog    *string   `json:"build_log,omitempty" db:"build_log"`
	SCMURL      *string   `json:"scm_url,omitempty" db:"scm_url"`
	SCMBranch   *string   `json:"scm_branch,omitempty" db:"scm_branch"`
	SpecPath    *string   `json:"spec_path,omitempty" db:"spec_path"`
	WebhookKey  *string   `json:"webhook_key,omitempty" db:"webhook_key"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// execPackReadCols is the projection returned on read (no webhook_key).
const execPackReadCols = `id, name, description, spec, status, build_log, scm_url, scm_branch, spec_path, created_at`

// ExecutionPackStore is the data-access layer for the execution-packs registry.
type ExecutionPackStore struct {
	db *sqlx.DB
}

func NewExecutionPackStore(db *sqlx.DB) *ExecutionPackStore { return &ExecutionPackStore{db: db} }

// List returns all packs, name-ordered.
func (s *ExecutionPackStore) List(ctx context.Context) ([]ExecutionPack, error) {
	packs := []ExecutionPack{}
	err := s.db.SelectContext(ctx, &packs, `SELECT `+execPackReadCols+` FROM execution_packs ORDER BY name`)
	return packs, wrap("ExecutionPackStore.List", err)
}

// Create inserts a pack (status decided by the caller) and returns it.
func (s *ExecutionPackStore) Create(ctx context.Context, in ExecutionPack, status string) (ExecutionPack, error) {
	var created ExecutionPack
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO execution_packs (name, description, spec, status, scm_url, scm_branch, spec_path, webhook_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING `+execPackReadCols,
		in.Name, in.Description, in.Spec, status, in.SCMURL, in.SCMBranch, in.SpecPath, in.WebhookKey).StructScan(&created)
	return created, wrap("ExecutionPackStore.Create", err)
}

// Update replaces a pack's editable fields (webhook_key preserved unless a new
// non-empty value is supplied; build_log cleared) and returns the row.
func (s *ExecutionPackStore) Update(ctx context.Context, id int64, in ExecutionPack, status string) (ExecutionPack, error) {
	var updated ExecutionPack
	err := s.db.QueryRowxContext(ctx,
		`UPDATE execution_packs SET
		   name=$2, description=$3, spec=$4, scm_url=$5, scm_branch=$6, spec_path=$7,
		   webhook_key=COALESCE(NULLIF($8,''), webhook_key),
		   status=$9, build_log=NULL
		 WHERE id=$1
		 RETURNING `+execPackReadCols,
		id, in.Name, in.Description, in.Spec, in.SCMURL, in.SCMBranch, in.SpecPath, in.WebhookKey, status).StructScan(&updated)
	return updated, wrap("ExecutionPackStore.Update", err)
}

// Rebuild re-queues a buildable pack (has a spec or git source); returns rows
// affected (0 = nothing buildable).
func (s *ExecutionPackStore) Rebuild(ctx context.Context, id int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE execution_packs SET status='pending', build_log=NULL
		 WHERE id=$1 AND (spec IS NOT NULL OR scm_url IS NOT NULL)`, id)
	if err != nil {
		return 0, wrap("ExecutionPackStore.Rebuild", err)
	}
	return res.RowsAffected()
}

// Delete removes a pack by id.
func (s *ExecutionPackStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM execution_packs WHERE id = $1`, id)
	return wrap("ExecutionPackStore.Delete", err)
}
