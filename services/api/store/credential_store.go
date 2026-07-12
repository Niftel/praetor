package store

import (
	"context"
	"encoding/json"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
)

// CredentialStore is the data-access layer for the credentials domain. It also
// reads credential_types (the type schema the handler needs to encrypt/mask
// secret fields), so the credentials domain stays self-contained.
type CredentialStore struct {
	db *sqlx.DB
}

func NewCredentialStore(db *sqlx.DB) *CredentialStore { return &CredentialStore{db: db} }

// ListAll returns all credentials (superuser/auditor view).
func (s *CredentialStore) ListAll(ctx context.Context) ([]models.Credential, error) {
	creds := []models.Credential{}
	err := s.db.SelectContext(ctx, &creds, "SELECT "+CredentialCols+" FROM credentials ORDER BY id ASC")
	return creds, wrap("CredentialStore.ListAll", err)
}

// ListByIDs returns the credentials whose id is in ids.
func (s *CredentialStore) ListByIDs(ctx context.Context, ids []int64) ([]models.Credential, error) {
	creds := []models.Credential{}
	if len(ids) == 0 {
		return creds, nil
	}
	q, args, err := sqlx.In("SELECT "+CredentialCols+" FROM credentials WHERE id IN (?) ORDER BY id ASC", ids)
	if err != nil {
		return nil, wrap("CredentialStore.ListByIDs", err)
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &creds, q, args...)
	return creds, wrap("CredentialStore.ListByIDs", err)
}

// Get returns a single credential by id.
func (s *CredentialStore) Get(ctx context.Context, id int64) (models.Credential, error) {
	var cred models.Credential
	err := s.db.GetContext(ctx, &cred, "SELECT "+CredentialCols+" FROM credentials WHERE id = $1", id)
	return cred, wrap("CredentialStore.Get", err)
}

// Create inserts a credential (inputs already processed/encrypted by the caller).
func (s *CredentialStore) Create(ctx context.Context, input models.Credential) (models.Credential, error) {
	query := `
		INSERT INTO credentials (organization_id, credential_type_id, name, description, inputs)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + CredentialCols
	var created models.Credential
	err := s.db.QueryRowxContext(ctx, query,
		input.OrganizationID, input.CredentialTypeID, input.Name, input.Description, input.Inputs,
	).StructScan(&created)
	return created, wrap("CredentialStore.Create", err)
}

// Update applies name/description/inputs to a credential and returns the row.
func (s *CredentialStore) Update(ctx context.Context, id int64, input models.Credential) (models.Credential, error) {
	query := `
		UPDATE credentials
		SET name = $1, description = $2, inputs = $3, modified_at = NOW()
		WHERE id = $4
		RETURNING ` + CredentialCols
	var updated models.Credential
	err := s.db.QueryRowxContext(ctx, query,
		input.Name, input.Description, input.Inputs, id,
	).StructScan(&updated)
	return updated, wrap("CredentialStore.Update", err)
}

// Delete removes a credential by id.
func (s *CredentialStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM credentials WHERE id = $1", id)
	return wrap("CredentialStore.Delete", err)
}

// CredentialTypeInputs returns the `inputs` schema JSON of a credential type —
// what the handler needs to identify and encrypt/mask secret fields.
func (s *CredentialStore) CredentialTypeInputs(ctx context.Context, typeID int64) (json.RawMessage, error) {
	var ct models.CredentialType
	err := s.db.GetContext(ctx, &ct, "SELECT "+CredentialTypeCols+" FROM credential_types WHERE id = $1", typeID)
	if err != nil {
		return nil, wrap("CredentialStore.CredentialTypeInputs", err)
	}
	return ct.Inputs, nil
}
