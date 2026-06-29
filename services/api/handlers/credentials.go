package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/crypto"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/services/api/render"
)

type CredentialsResource struct {
	DB *sqlx.DB
}

func NewCredentialsResource(db *sqlx.DB) *CredentialsResource {
	return &CredentialsResource{DB: db}
}

func (rs *CredentialsResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListCredentials)
	r.Post("/", rs.CreateCredential)
	r.Get("/{id}", rs.GetCredential)
	r.Put("/{id}", rs.UpdateCredential)
	r.Delete("/{id}", rs.DeleteCredential)
	return r
}

func (rs *CredentialsResource) ListCredentials(w http.ResponseWriter, r *http.Request) {
	var creds []models.Credential
	err := rs.DB.SelectContext(r.Context(), &creds, "SELECT * FROM credentials ORDER BY id ASC")
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	// Mask secrets for list view is expensive if we do full logic,
	// typically list view doesn't return full details, but for now we will mask.
	// Actually, doing schema validation for every item in list is slow.
	// Let's assume for LIST we might not need inputs, or we return them encrypted/masked blindly?
	// For simplicity, let's just mask everything that looks like a secret if we can,
	// OR better: fetch type cache.
	// Optimization: For now, just return them as is (encrypted strings are opaque anyway).
	// But UI expects masked. Let's iterate and mask.

	for i := range creds {
		rs.maskCredentialSecrets(&creds[i])
	}

	render.JSON(w, r, creds)
}

func (rs *CredentialsResource) GetCredential(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var cred models.Credential
	err = rs.DB.GetContext(r.Context(), &cred, "SELECT * FROM credentials WHERE id = $1", id)
	if err != nil {
		render.ErrNotFound(err).Render(w, r)
		return
	}

	rs.maskCredentialSecrets(&cred)
	render.JSON(w, r, cred)
}

func (rs *CredentialsResource) CreateCredential(w http.ResponseWriter, r *http.Request) {
	var input models.Credential
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Validation
	if input.Name == "" || input.CredentialTypeID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if input.OrganizationID == 0 {
		input.OrganizationID = 1
	}

	if err := rs.processSecrets(&input, nil); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	query := `
		INSERT INTO credentials (organization_id, credential_type_id, name, description, inputs)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING *`

	var created models.Credential
	err := rs.DB.QueryRowxContext(r.Context(), query,
		input.OrganizationID, input.CredentialTypeID, input.Name, input.Description, input.Inputs,
	).StructScan(&created)

	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	rs.maskCredentialSecrets(&created)
	render.Created(w, r, created)
}

func (rs *CredentialsResource) UpdateCredential(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var existing models.Credential
	err = rs.DB.GetContext(r.Context(), &existing, "SELECT * FROM credentials WHERE id = $1", id)
	if err != nil {
		render.ErrNotFound(err).Render(w, r)
		return
	}

	var input models.Credential
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	input.ID = id
	input.CredentialTypeID = existing.CredentialTypeID // Cannot change type

	if err := rs.processSecrets(&input, &existing); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	query := `
		UPDATE credentials 
		SET name = $1, description = $2, inputs = $3, modified_at = NOW()
		WHERE id = $4
		RETURNING *`

	var updated models.Credential
	err = rs.DB.QueryRowxContext(r.Context(), query,
		input.Name, input.Description, input.Inputs, id,
	).StructScan(&updated)

	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	rs.maskCredentialSecrets(&updated)
	render.JSON(w, r, updated)
}

func (rs *CredentialsResource) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	_, err = rs.DB.ExecContext(r.Context(), "DELETE FROM credentials WHERE id = $1", id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.NoContent(w, r)
}

// Helpers

func (rs *CredentialsResource) processSecrets(input *models.Credential, existing *models.Credential) error {
	secretKey := os.Getenv("PRAETOR_SECRET_KEY")
	if secretKey == "" {
		// Fallback for dev only!
		secretKey = "12345678901234567890123456789012"
	}

	// Fetch Type Definition
	var ct models.CredentialType
	err := rs.DB.Get(&ct, "SELECT * FROM credential_types WHERE id = $1", input.CredentialTypeID)
	if err != nil {
		return err
	}

	// Parse Type Schema to find secret fields
	type SchemaField struct {
		ID     string `json:"id"`
		Secret bool   `json:"secret"`
	}
	var schema struct {
		Fields []SchemaField `json:"fields"`
	}
	if err := json.Unmarshal(ct.Inputs, &schema); err != nil {
		return err
	}
	secrets := make(map[string]bool)
	for _, f := range schema.Fields {
		if f.Secret {
			secrets[f.ID] = true
		}
	}

	// Parse Inputs
	var inputMap map[string]interface{}
	if err := json.Unmarshal(input.Inputs, &inputMap); err != nil {
		return err
	}

	var existingMap map[string]interface{}
	if existing != nil {
		json.Unmarshal(existing.Inputs, &existingMap)
	}

	// Encrypt secrets
	for k, v := range inputMap {
		if secrets[k] {
			strVal, ok := v.(string)
			if !ok {
				continue
			}

			if strVal == "$encrypted$" {
				// Keep existing value
				if existingMap != nil {
					inputMap[k] = existingMap[k]
				}
			} else {
				// Encrypt new value
				enc, err := crypto.Encrypt(strVal, secretKey)
				if err != nil {
					return err
				}
				inputMap[k] = enc
			}
		}
	}

	// Marshal back
	updatedJson, err := json.Marshal(inputMap)
	if err != nil {
		return err
	}
	input.Inputs = updatedJson
	return nil
}

func (rs *CredentialsResource) maskCredentialSecrets(cred *models.Credential) {
	// Fetch Type Definition (Optimization: Could assume all inputs are opaque or fetch cache)
	// For correctness we fetch type.
	var ct models.CredentialType
	err := rs.DB.Get(&ct, "SELECT * FROM credential_types WHERE id = $1", cred.CredentialTypeID)
	if err != nil {
		return // Cannot mask if can't find type
	}

	type SchemaField struct {
		ID     string `json:"id"`
		Secret bool   `json:"secret"`
	}
	var schema struct {
		Fields []SchemaField `json:"fields"`
	}
	if err := json.Unmarshal(ct.Inputs, &schema); err != nil {
		return
	}

	var inputMap map[string]interface{}
	if err := json.Unmarshal(cred.Inputs, &inputMap); err != nil {
		return
	}

	masked := false
	for _, f := range schema.Fields {
		if f.Secret {
			if _, ok := inputMap[f.ID]; ok {
				inputMap[f.ID] = "$encrypted$"
				masked = true
			}
		}
	}

	if masked {
		out, _ := json.Marshal(inputMap)
		cred.Inputs = out
	}
}
