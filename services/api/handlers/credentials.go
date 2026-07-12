package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/crypto"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/render"
	"github.com/praetordev/praetor/services/api/store"
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

func NewCredentialsResource(db *sqlx.DB) *CredentialsResource {
	return &CredentialsResource{DB: db, Authorizer: NewAuthorizer(db), store: store.NewCredentialStore(db)}
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
	uc := currentUser(r)

	var creds []models.Credential
	if uc.IsSuperuser || uc.IsSystemAuditor {
		var err error
		if creds, err = rs.store.ListAll(r.Context()); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
	} else {
		ids, err := rs.readableIDs(r, rbac.ContentTypeCredential)
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

	render.JSON(w, r, creds)
}

func (rs *CredentialsResource) GetCredential(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeCredential, id, actRead) {
		return
	}

	cred, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.ErrNotFound(err).Render(w, r)
		return
	}

	rs.maskCredentialSecrets(r.Context(), &cred)
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
		render.ErrInvalidRequest(nil).Render(w, r) // organization_id is required
		return
	}

	// Creating a credential requires the org's credential_admin_role (org admins
	// and superusers inherit it through the role hierarchy).
	if !rs.authorizeOrgRole(w, r, input.OrganizationID, rbac.RoleFieldCredentialAdmin) {
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

	rs.grantCreatorAdmin(r.Context(), rbac.ContentTypeCredential, created.ID, currentUser(r))
	rs.maskCredentialSecrets(r.Context(), &created)
	render.Created(w, r, created)
}

func (rs *CredentialsResource) UpdateCredential(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeCredential, id, actAdmin) {
		return
	}

	existing, err := rs.store.Get(r.Context(), id)
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
	render.JSON(w, r, updated)
}

func (rs *CredentialsResource) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeCredential, id, actAdmin) {
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

func (rs *CredentialsResource) maskCredentialSecrets(ctx context.Context, cred *models.Credential) {
	// Fetch Type Definition (Optimization: Could assume all inputs are opaque or fetch cache)
	// For correctness we fetch type.
	typeInputs, err := rs.store.CredentialTypeInputs(ctx, cred.CredentialTypeID)
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
	if err := json.Unmarshal(typeInputs, &schema); err != nil {
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
