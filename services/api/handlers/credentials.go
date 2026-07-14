package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/crypto"
	"github.com/praetordev/models"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/dto"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
)

// CredentialStore is the credentials-domain data access the handler depends on.
type CredentialStore interface {
	ListAll(ctx context.Context) ([]models.Credential, error)
	ListByIDs(ctx context.Context, ids []int64) ([]models.Credential, error)
	Get(ctx context.Context, id int64) (models.Credential, error)
	Create(ctx context.Context, input models.Credential) (models.Credential, error)
	Update(ctx context.Context, id int64, input models.Credential) (models.Credential, error)
	Delete(ctx context.Context, id int64) error
	CredentialTypeInputs(ctx context.Context, typeID int64) (json.RawMessage, error)
}

type CredentialsResource struct {
	DB *sqlx.DB
	*Authorizer
	store CredentialStore
}

func NewCredentialsResource(db *sqlx.DB, authz *Authorizer) *CredentialsResource {
	return &CredentialsResource{DB: db, Authorizer: authz, store: store.NewCredentialStore(db)}
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
	viewAll, verr := rs.canViewAll(r, rbac.Credential)
	if verr != nil {
		render.ErrInternal(verr).Render(w, r)
		return
	}
	if viewAll {
		var err error
		if creds, err = rs.store.ListAll(r.Context()); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
	} else {
		ids, err := rs.readableIDs(r, rbac.Credential)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if creds, err = rs.store.ListByIDs(r.Context(), ids); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
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
		rs.maskCredentialSecrets(r.Context(), &creds[i])
	}

	render.JSON(w, r, dto.FromCredentials(creds))
}

func (rs *CredentialsResource) GetCredential(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.Credential, id, actRead) {
		return
	}

	cred, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.ErrNotFound(err).Render(w, r)
		return
	}

	rs.maskCredentialSecrets(r.Context(), &cred)
	render.JSON(w, r, dto.FromCredential(cred))
}

func (rs *CredentialsResource) CreateCredential(w http.ResponseWriter, r *http.Request) {
	var body dto.Credential
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	input := body.ToModel()

	// Validation
	if input.Name == "" || input.CredentialTypeID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if input.OrganizationID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r) // organization_id is required
		return
	}

	// Creating a credential requires the org's credential_admin_role (org admins
	// and superusers inherit it through the role hierarchy).
	if !rs.authorizeOrgRole(w, r, input.OrganizationID, rbac.CredentialAdminRole) {
		return
	}

	if err := rs.processSecrets(r.Context(), &input, nil); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	created, err := rs.store.Create(r.Context(), input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	rs.grantCreatorAdmin(r.Context(), rbac.Credential, created.ID, currentUser(r))
	rs.maskCredentialSecrets(r.Context(), &created)
	render.Created(w, r, dto.FromCredential(created))
}

func (rs *CredentialsResource) UpdateCredential(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.Credential, id, actAdmin) {
		return
	}

	existing, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.ErrNotFound(err).Render(w, r)
		return
	}

	var body dto.Credential
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	input := body.ToModel()
	input.ID = id
	input.CredentialTypeID = existing.CredentialTypeID // Cannot change type

	if err := rs.processSecrets(r.Context(), &input, &existing); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	updated, err := rs.store.Update(r.Context(), id, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	rs.maskCredentialSecrets(r.Context(), &updated)
	render.JSON(w, r, dto.FromCredential(updated))
}

func (rs *CredentialsResource) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.Credential, id, actAdmin) {
		return
	}

	if err := rs.store.Delete(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.NoContent(w, r)
}

// Helpers

func (rs *CredentialsResource) processSecrets(ctx context.Context, input *models.Credential, existing *models.Credential) error {
	// Fetch Type Definition
	typeInputs, err := rs.store.CredentialTypeInputs(ctx, input.CredentialTypeID)
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
	if err := json.Unmarshal(typeInputs, &schema); err != nil {
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
				enc, err := crypto.EncryptSecret(strVal)
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

// maskCredentialSecrets redacts secret input values before a credential is
// serialized to a client. It fails CLOSED: if we cannot positively identify and
// mask the secret fields (missing type, unparseable schema or inputs), we redact
// the entire Inputs blob rather than leak plaintext. Non-secret field values are
// preserved only on the happy path where the type schema is known.
func (rs *CredentialsResource) maskCredentialSecrets(ctx context.Context, cred *models.Credential) {
	if len(cred.Inputs) == 0 {
		return
	}
	// Any inability to mask precisely => drop the raw inputs entirely.
	redactAll := func() { cred.Inputs = json.RawMessage(`{}`) }

	typeInputs, err := rs.store.CredentialTypeInputs(ctx, cred.CredentialTypeID)
	if err != nil {
		redactAll() // cannot determine which fields are secret
		return
	}

	type SchemaField struct {
		ID     string `json:"id"`
		Secret bool   `json:"secret"`
	}
	var schema struct {
		Fields []SchemaField `json:"fields"`
	}
	if err := json.Unmarshal(typeInputs, &schema); err != nil {
		redactAll()
		return
	}

	var inputMap map[string]interface{}
	if err := json.Unmarshal(cred.Inputs, &inputMap); err != nil {
		redactAll()
		return
	}

	for _, f := range schema.Fields {
		if f.Secret {
			if _, ok := inputMap[f.ID]; ok {
				inputMap[f.ID] = "$encrypted$"
			}
		}
	}

	out, err := json.Marshal(inputMap)
	if err != nil {
		redactAll()
		return
	}
	cred.Inputs = out
}
