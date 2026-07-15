package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	secretsclient "github.com/Niftel/praetor-secrets/client"
	"github.com/google/uuid"
	"github.com/praetordev/models"
	"github.com/praetordev/store"
)

var errUnsupportedSecretsCredentialType = errors.New("credential type is not supported by the secrets service")

func (rs *CredentialsResource) isServiceBackedCredential(ctx context.Context, id int64) (bool, error) {
	var serviceID *string
	if err := rs.DB.GetContext(ctx, &serviceID, `SELECT secrets_service_id::text FROM credentials WHERE id = $1`, id); err != nil {
		return false, err
	}
	return serviceID != nil && *serviceID != "", nil
}

// createServiceBackedCredential keeps submitted plaintext out of Praetor's
// credentials table. The database transaction does not become visible until it
// contains only placeholders plus the external UUID/version reference.
func (rs *CredentialsResource) createServiceBackedCredential(r *http.Request, input models.Credential) (models.Credential, error) {
	ctx := r.Context()
	tx, err := rs.DB.BeginTxx(ctx, nil)
	if err != nil {
		return models.Credential{}, err
	}
	defer tx.Rollback()

	var typeName string
	var schemaJSON json.RawMessage
	if err := tx.QueryRowxContext(ctx, `SELECT name, inputs FROM credential_types WHERE id = $1`, input.CredentialTypeID).Scan(&typeName, &schemaJSON); err != nil {
		return models.Credential{}, err
	}
	credentialType := strings.ToLower(strings.TrimSpace(typeName))
	if credentialType != "machine" {
		return models.Credential{}, errUnsupportedSecretsCredentialType
	}

	serviceInputs, placeholders, err := splitCredentialInputs(schemaJSON, input.Inputs)
	if err != nil {
		return models.Credential{}, err
	}
	defer clear(serviceInputs)
	placeholderJSON, err := json.Marshal(placeholders)
	if err != nil {
		return models.Credential{}, err
	}

	var created models.Credential
	query := `INSERT INTO credentials (organization_id, credential_type_id, name, description, inputs)
		VALUES ($1, $2, $3, $4, $5) RETURNING ` + store.CredentialCols
	if err := tx.QueryRowxContext(ctx, query, input.OrganizationID, input.CredentialTypeID, input.Name, input.Description, placeholderJSON).StructScan(&created); err != nil {
		return models.Credential{}, err
	}

	user := currentUser(r)
	metadata, err := rs.secrets.CreateCredential(ctx, secretsclient.CreateCredentialRequest{
		OrganizationID: strconv.FormatInt(input.OrganizationID, 10),
		Name:           input.Name, CredentialType: credentialType, SchemaVersion: 1,
		Inputs: serviceInputs, IdempotencyKey: "praetor-api-" + uuid.NewString(),
		Actor: secretsclient.Actor{UserID: strconv.FormatInt(user.UserID, 10), Username: user.Username},
	})
	if err != nil {
		return models.Credential{}, fmt.Errorf("create credential in secrets service: %w", err)
	}
	if metadata.OrganizationID != strconv.FormatInt(input.OrganizationID, 10) || metadata.Version == 0 {
		return models.Credential{}, errors.New("secrets service returned inconsistent credential metadata")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE credentials SET secrets_service_id = $1, secrets_service_version = $2 WHERE id = $3`, metadata.ID, metadata.Version, created.ID); err != nil {
		return models.Credential{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.Credential{}, err
	}
	return created, nil
}

func splitCredentialInputs(schemaJSON, inputsJSON json.RawMessage) (map[string]string, map[string]any, error) {
	var schema struct {
		Fields []struct {
			ID     string `json:"id"`
			Secret bool   `json:"secret"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return nil, nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(inputsJSON, &raw); err != nil {
		return nil, nil, err
	}
	secretFields := make(map[string]bool, len(schema.Fields))
	for _, field := range schema.Fields {
		secretFields[field.ID] = field.Secret
	}
	serviceInputs := make(map[string]string, len(raw))
	placeholders := make(map[string]any, len(raw))
	for name, value := range raw {
		text, ok := value.(string)
		if !ok {
			return nil, nil, fmt.Errorf("credential input %q must be a string", name)
		}
		serviceInputs[name] = text
		if secretFields[name] {
			placeholders[name] = "$encrypted$"
		} else {
			placeholders[name] = text
		}
	}
	return serviceInputs, placeholders, nil
}
