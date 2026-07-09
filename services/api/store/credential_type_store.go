package store

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
)

// CredentialTypeStore is the data-access layer for the credential-types domain.
type CredentialTypeStore struct {
	db *sqlx.DB
}

func NewCredentialTypeStore(db *sqlx.DB) *CredentialTypeStore { return &CredentialTypeStore{db: db} }

// ListAll returns all credential types.
func (s *CredentialTypeStore) ListAll(ctx context.Context) ([]models.CredentialType, error) {
	types := []models.CredentialType{}
	err := s.db.SelectContext(ctx, &types, "SELECT "+CredentialTypeCols+" FROM credential_types ORDER BY id ASC")
	return types, wrap("CredentialTypeStore.ListAll", err)
}

// Get returns a single credential type by id.
func (s *CredentialTypeStore) Get(ctx context.Context, id int64) (models.CredentialType, error) {
	var ct models.CredentialType
	err := s.db.GetContext(ctx, &ct, "SELECT "+CredentialTypeCols+" FROM credential_types WHERE id = $1", id)
	return ct, wrap("CredentialTypeStore.Get", err)
}

// Create inserts a new user-defined credential type (managed=false).
func (s *CredentialTypeStore) Create(ctx context.Context, in models.CredentialType) (models.CredentialType, error) {
	var ct models.CredentialType
	err := s.db.GetContext(ctx, &ct, `
		INSERT INTO credential_types (name, description, inputs, injectors, managed)
		VALUES ($1, $2, $3::jsonb, $4::jsonb, false)
		RETURNING `+CredentialTypeCols,
		in.Name, in.Description, in.Inputs, in.Injectors)
	return ct, wrap("CredentialTypeStore.Create", err)
}

// Update edits a user-defined credential type. The `AND NOT managed` guard makes
// a built-in type untouchable even under a race (returns sql.ErrNoRows).
func (s *CredentialTypeStore) Update(ctx context.Context, id int64, in models.CredentialType) (models.CredentialType, error) {
	var ct models.CredentialType
	err := s.db.GetContext(ctx, &ct, `
		UPDATE credential_types
		SET name = $2, description = $3, inputs = $4::jsonb, injectors = $5::jsonb, modified_at = now()
		WHERE id = $1 AND NOT managed
		RETURNING `+CredentialTypeCols,
		id, in.Name, in.Description, in.Inputs, in.Injectors)
	return ct, wrap("CredentialTypeStore.Update", err)
}

// Delete removes a user-defined credential type. `AND NOT managed` protects the
// built-ins; the FK from credentials.credential_type_id blocks deletion while any
// credential still uses the type (caller surfaces that as a conflict). Returns the
// number of rows deleted (0 = not found or managed).
func (s *CredentialTypeStore) Delete(ctx context.Context, id int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM credential_types WHERE id = $1 AND NOT managed`, id)
	if err != nil {
		return 0, wrap("CredentialTypeStore.Delete", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
