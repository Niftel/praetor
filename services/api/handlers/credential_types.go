package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/services/api/render"
	"github.com/praetordev/praetor/services/api/store"
)

// CredentialTypeStore is the credential-types data access the handler depends on.
type CredentialTypeStore interface {
	ListAll(ctx context.Context) ([]models.CredentialType, error)
	Get(ctx context.Context, id int64) (models.CredentialType, error)
}

type CredentialTypesResource struct {
	DB    *sqlx.DB
	store CredentialTypeStore
}

func NewCredentialTypesResource(db *sqlx.DB) *CredentialTypesResource {
	return &CredentialTypesResource{DB: db, store: store.NewCredentialTypeStore(db)}
}

func (rs *CredentialTypesResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListCredentialTypes)
	r.Get("/{id}", rs.GetCredentialType)
	return r
}

func (rs *CredentialTypesResource) ListCredentialTypes(w http.ResponseWriter, r *http.Request) {
	types, err := rs.store.ListAll(r.Context())
	if err != nil {
		log.Printf("Failed to list credential types: %v", err)
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, types)
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
	render.JSON(w, r, ct)
}
