package store

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/rbac"
)

// CapabilityStore is the data-access layer for the DAB-style capability RBAC tables
// (dab_permissions, role_definitions, role_definition_permissions). The assignment +
// evaluation machinery lives in capability_access_store.go.
type CapabilityStore struct {
	db *sqlx.DB
}

func NewCapabilityStore(db *sqlx.DB) *CapabilityStore { return &CapabilityStore{db: db} }

const roleDefinitionCols = `id, name, description, managed, content_type, created_at, modified_at`

// GetRoleDefinitionByName returns a single role definition by its unique name.
func (s *CapabilityStore) GetRoleDefinitionByName(ctx context.Context, name string) (rbac.RoleDefinition, error) {
	var d rbac.RoleDefinition
	err := s.db.GetContext(ctx, &d,
		`SELECT `+roleDefinitionCols+` FROM role_definitions WHERE name = $1`, name)
	return d, wrap("CapabilityStore.GetRoleDefinitionByName", err)
}
