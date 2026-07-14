package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/praetordev/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/dto"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
)

// CredentialTypeStore is the credential-types data access the handler depends on.
type CredentialTypeStore interface {
	ListAll(ctx context.Context) ([]models.CredentialType, error)
	Get(ctx context.Context, id int64) (models.CredentialType, error)
	Create(ctx context.Context, in models.CredentialType) (models.CredentialType, error)
	Update(ctx context.Context, id int64, in models.CredentialType) (models.CredentialType, error)
	Delete(ctx context.Context, id int64) (int64, error)
}

type CredentialTypesResource struct {
	*Authorizer
	DB    *sqlx.DB
	store CredentialTypeStore
}

func NewCredentialTypesResource(db *sqlx.DB, authz *Authorizer) *CredentialTypesResource {
	return &CredentialTypesResource{Authorizer: authz, DB: db, store: store.NewCredentialTypeStore(db)}
}

func (rs *CredentialTypesResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListCredentialTypes)
	r.Get("/{id}", rs.GetCredentialType)
	// User-defined credential types (superuser only). Built-in "managed" types are
	// read-only. The engine (pkg/credentials) is already data-driven, so a new type
	// is usable immediately with no code change.
	r.Post("/", rs.CreateCredentialType)
	r.Put("/{id}", rs.UpdateCredentialType)
	r.Delete("/{id}", rs.DeleteCredentialType)
	return r
}

// credentialTypeInput is the create/update payload.
type credentialTypeInput struct {
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Inputs      json.RawMessage `json:"inputs"`
	Injectors   json.RawMessage `json:"injectors"`
}

var templateRefRe = regexp.MustCompile(`{{\s*([a-zA-Z0-9_]+)\s*}}`)

// validateCredentialTypeSpec checks a type's inputs/injectors are well-formed and
// self-consistent before it's stored (and later fed to the injector renderer):
// every field needs an id + supported type, and every {{ ref }} in the injectors
// must name a declared field — otherwise a credential of this type would render a
// broken/empty injector at run time.
func validateCredentialTypeSpec(inputsRaw, injectorsRaw json.RawMessage) error {
	var inputs struct {
		Fields []struct {
			ID     string `json:"id"`
			Label  string `json:"label"`
			Type   string `json:"type"`
			Secret bool   `json:"secret"`
		} `json:"fields"`
	}
	if len(inputsRaw) > 0 {
		if err := json.Unmarshal(inputsRaw, &inputs); err != nil {
			return fmt.Errorf("inputs is not valid JSON: %w", err)
		}
	}
	fieldIDs := map[string]bool{}
	for i, f := range inputs.Fields {
		if f.ID == "" {
			return fmt.Errorf("inputs.fields[%d]: id is required", i)
		}
		switch f.Type {
		case "text", "password", "textarea":
		default:
			return fmt.Errorf("inputs.fields[%d] (%q): type must be text|password|textarea", i, f.ID)
		}
		if fieldIDs[f.ID] {
			return fmt.Errorf("inputs.fields: duplicate id %q", f.ID)
		}
		fieldIDs[f.ID] = true
	}

	if len(injectorsRaw) > 0 {
		var injectors struct {
			Env  map[string]string `json:"env"`
			File map[string]string `json:"file"`
		}
		if err := json.Unmarshal(injectorsRaw, &injectors); err != nil {
			return fmt.Errorf("injectors is not valid JSON: %w", err)
		}
		for _, m := range []map[string]string{injectors.Env, injectors.File} {
			for k, v := range m {
				for _, ref := range templateRefRe.FindAllStringSubmatch(v, -1) {
					if !fieldIDs[ref[1]] {
						return fmt.Errorf("injector %q references undefined field %q", k, ref[1])
					}
				}
			}
		}
	}
	return nil
}

// CreateCredentialType POST /credential-types — define a new user credential type.
func (rs *CredentialTypesResource) CreateCredentialType(w http.ResponseWriter, r *http.Request) {
	if !rs.requireGlobal(w, r, rbac.CapManageCredentialType) {
		return
	}
	var in credentialTypeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		render.ErrInvalidRequest(fmt.Errorf("name is required")).Render(w, r)
		return
	}
	if err := validateCredentialTypeSpec(in.Inputs, in.Injectors); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	ct, err := rs.store.Create(r.Context(), models.CredentialType{
		Name: strings.TrimSpace(in.Name), Description: in.Description,
		Inputs: defaultJSON(in.Inputs), Injectors: defaultJSON(in.Injectors),
	})
	if err != nil {
		if isUniqueViolation(err) {
			render.ErrConflict(fmt.Errorf("a credential type named %q already exists", in.Name)).Render(w, r)
			return
		}
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, dto.FromCredentialType(ct))
}

// UpdateCredentialType PUT /credential-types/{id} — edit a user credential type.
func (rs *CredentialTypesResource) UpdateCredentialType(w http.ResponseWriter, r *http.Request) {
	if !rs.requireGlobal(w, r, rbac.CapManageCredentialType) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	existing, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.ErrNotFound(fmt.Errorf("unknown credential type")).Render(w, r)
		return
	}
	if existing.Managed {
		render.ErrForbidden(fmt.Errorf("%q is a built-in credential type and cannot be edited", existing.Name)).Render(w, r)
		return
	}
	var in credentialTypeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		render.ErrInvalidRequest(fmt.Errorf("name is required")).Render(w, r)
		return
	}
	if err := validateCredentialTypeSpec(in.Inputs, in.Injectors); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	ct, err := rs.store.Update(r.Context(), id, models.CredentialType{
		Name: strings.TrimSpace(in.Name), Description: in.Description,
		Inputs: defaultJSON(in.Inputs), Injectors: defaultJSON(in.Injectors),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) { // lost a race to a managed-flag/delete
			render.ErrForbidden(fmt.Errorf("credential type is not editable")).Render(w, r)
			return
		}
		if isUniqueViolation(err) {
			render.ErrConflict(fmt.Errorf("a credential type named %q already exists", in.Name)).Render(w, r)
			return
		}
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, dto.FromCredentialType(ct))
}

// DeleteCredentialType DELETE /credential-types/{id} — remove a user credential type.
func (rs *CredentialTypesResource) DeleteCredentialType(w http.ResponseWriter, r *http.Request) {
	if !rs.requireGlobal(w, r, rbac.CapManageCredentialType) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	existing, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.ErrNotFound(fmt.Errorf("unknown credential type")).Render(w, r)
		return
	}
	if existing.Managed {
		render.ErrForbidden(fmt.Errorf("%q is a built-in credential type and cannot be deleted", existing.Name)).Render(w, r)
		return
	}
	n, err := rs.store.Delete(r.Context(), id)
	if err != nil {
		if isForeignKeyViolation(err) {
			render.ErrConflict(fmt.Errorf("this credential type is still used by one or more credentials")).Render(w, r)
			return
		}
		render.ErrInternal(err).Render(w, r)
		return
	}
	if n == 0 {
		render.ErrForbidden(fmt.Errorf("credential type is not deletable")).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// defaultJSON returns raw, or "{}" when empty, so a missing inputs/injectors is
// stored as an empty object (matching the column default) rather than SQL NULL.
func defaultJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23503"
}

func (rs *CredentialTypesResource) ListCredentialTypes(w http.ResponseWriter, r *http.Request) {
	types, err := rs.store.ListAll(r.Context())
	if err != nil {
		logger.Error("list credential types failed", "err", err)
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, dto.FromCredentialTypes(types))
}

func (rs *CredentialTypesResource) GetCredentialType(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	ct, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.ErrNotFound(err).Render(w, r)
		return
	}
	render.JSON(w, r, dto.FromCredentialType(ct))
}
