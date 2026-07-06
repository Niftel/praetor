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
	return types, err
}

// Get returns a single credential type by id.
func (s *CredentialTypeStore) Get(ctx context.Context, id int64) (models.CredentialType, error) {
	var ct models.CredentialType
	err := s.db.GetContext(ctx, &ct, "SELECT "+CredentialTypeCols+" FROM credential_types WHERE id = $1", id)
	return ct, err
}
