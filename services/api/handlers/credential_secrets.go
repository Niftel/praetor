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
var errAmbiguousCredentialUpdate = errors.New("change either credential metadata or provide a complete replacement for every secret input")

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

func (rs *CredentialsResource) updateServiceBackedCredential(r *http.Request, existing, input models.Credential) (models.Credential, error) {
	ctx := r.Context()
	var serviceID string
	var serviceVersion uint64
	var schemaJSON json.RawMessage
	if err := rs.DB.QueryRowxContext(ctx, `SELECT c.secrets_service_id::text, c.secrets_service_version, ct.inputs
		FROM credentials c JOIN credential_types ct ON ct.id = c.credential_type_id WHERE c.id = $1`, existing.ID).Scan(&serviceID, &serviceVersion, &schemaJSON); err != nil {
		return models.Credential{}, err
	}
	serviceInputs, placeholders, err := splitCredentialInputs(schemaJSON, input.Inputs)
	if err != nil {
		return models.Credential{}, err
	}
	defer clear(serviceInputs)
	metadataChanged := input.Name != existing.Name
	descriptionChanged := !sameOptionalString(input.Description, existing.Description)
	inputsChanged, completeReplacement, err := credentialInputChange(schemaJSON, existing.Inputs, input.Inputs)
	if err != nil {
		return models.Credential{}, err
	}
	if (metadataChanged && inputsChanged) || (inputsChanged && !completeReplacement) {
		return models.Credential{}, errAmbiguousCredentialUpdate
	}
	actor := secretsclient.Actor{UserID: strconv.FormatInt(currentUser(r).UserID, 10), Username: currentUser(r).Username}
	var metadataVersion uint64
	switch {
	case inputsChanged:
		metadata, err := rs.secrets.ReplaceInputs(ctx, secretsclient.ReplaceInputsRequest{OrganizationID: strconv.FormatInt(existing.OrganizationID, 10), CredentialID: serviceID, ExpectedVersion: serviceVersion, Inputs: serviceInputs, Actor: actor})
		if err != nil {
			return models.Credential{}, fmt.Errorf("replace credential inputs in secrets service: %w", err)
		}
		if metadata.ID != serviceID || metadata.OrganizationID != strconv.FormatInt(existing.OrganizationID, 10) || metadata.Version <= serviceVersion {
			return models.Credential{}, errors.New("secrets service returned inconsistent replacement metadata")
		}
		metadataVersion = metadata.Version
	case metadataChanged:
		metadata, err := rs.secrets.UpdateMetadata(ctx, secretsclient.UpdateMetadataRequest{OrganizationID: strconv.FormatInt(existing.OrganizationID, 10), CredentialID: serviceID, ExpectedVersion: serviceVersion, Name: input.Name, Actor: actor})
		if err != nil {
			return models.Credential{}, fmt.Errorf("update credential metadata in secrets service: %w", err)
		}
		if metadata.ID != serviceID || metadata.OrganizationID != strconv.FormatInt(existing.OrganizationID, 10) || metadata.Version <= serviceVersion {
			return models.Credential{}, errors.New("secrets service returned inconsistent update metadata")
		}
		metadataVersion = metadata.Version
	default:
		if !descriptionChanged {
			return existing, nil
		}
		var updated models.Credential
		query := `UPDATE credentials SET description = $1, modified_at = NOW() WHERE id = $2 RETURNING ` + store.CredentialCols
		if err := rs.DB.QueryRowxContext(ctx, query, input.Description, existing.ID).StructScan(&updated); err != nil {
			return models.Credential{}, err
		}
		return updated, nil
	}
	placeholderJSON, err := json.Marshal(placeholders)
	if err != nil {
		return models.Credential{}, err
	}
	var updated models.Credential
	query := `UPDATE credentials SET name = $1, description = $2, inputs = $3, secrets_service_version = $4, modified_at = NOW()
		WHERE id = $5 AND secrets_service_version = $6 RETURNING ` + store.CredentialCols
	if err := rs.DB.QueryRowxContext(ctx, query, input.Name, input.Description, placeholderJSON, metadataVersion, existing.ID, serviceVersion).StructScan(&updated); err != nil {
		return models.Credential{}, err
	}
	return updated, nil
}

func credentialInputChange(schemaJSON, existingJSON, inputJSON json.RawMessage) (changed, complete bool, err error) {
	var schema struct {
		Fields []struct {
			ID     string `json:"id"`
			Secret bool   `json:"secret"`
		} `json:"fields"`
	}
	var existing, input map[string]any
	if err = json.Unmarshal(schemaJSON, &schema); err != nil {
		return
	}
	if err = json.Unmarshal(existingJSON, &existing); err != nil {
		return
	}
	if err = json.Unmarshal(inputJSON, &input); err != nil {
		return
	}
	complete = true
	for _, field := range schema.Fields {
		value, present := input[field.ID]
		if field.Secret && present && value == "$encrypted$" {
			complete = false
		}
	}
	existingBytes, _ := json.Marshal(existing)
	inputBytes, _ := json.Marshal(input)
	changed = string(existingBytes) != string(inputBytes)
	return
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (rs *CredentialsResource) retireServiceBackedCredential(r *http.Request, id int64) error {
	ctx := r.Context()
	tx, err := rs.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var organizationID int64
	var serviceID string
	var serviceVersion uint64
	if err := tx.QueryRowxContext(ctx, `DELETE FROM credentials WHERE id = $1
		RETURNING organization_id, secrets_service_id::text, secrets_service_version`, id).Scan(&organizationID, &serviceID, &serviceVersion); err != nil {
		return err
	}
	user := currentUser(r)
	if _, err := rs.secrets.RetireCredential(ctx, secretsclient.RetireCredentialRequest{
		OrganizationID: strconv.FormatInt(organizationID, 10), CredentialID: serviceID, ExpectedVersion: serviceVersion,
		Actor: secretsclient.Actor{UserID: strconv.FormatInt(user.UserID, 10), Username: user.Username},
	}); err != nil {
		return fmt.Errorf("retire credential in secrets service: %w", err)
	}
	return tx.Commit()
}
