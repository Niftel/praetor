package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/packspec"
	"github.com/praetordev/praetor/pkg/render"
	"github.com/praetordev/praetor/services/api/store"
)

// validatePackSpec rejects a malformed or unsafe inline pack spec before it's
// stored (and later fed to the packbuilder). An empty/absent spec is fine — the
// pack is a pre-built artifact or git-backed (whose spec the packbuilder
// validates when it fetches it). See pkg/packspec.
func validatePackSpec(spec *string) error {
	if spec == nil || strings.TrimSpace(*spec) == "" {
		return nil
	}
	s, err := packspec.Parse(*spec)
	if err != nil {
		return err
	}
	return s.Validate()
}

// ExecutionPacksResource manages the registry of Execution Packs — the named,
// self-contained Python+Ansible runtimes Praetor pushes onto hosts. Packs are
// built from a YAML spec via `make execpack`; this registry lets templates pick
// which pack a job runs in.
// ExecutionPackStore is the execution-packs data access the handler depends on.
type ExecutionPackStore interface {
	List(ctx context.Context) ([]store.ExecutionPack, error)
	Create(ctx context.Context, in store.ExecutionPack, status string) (store.ExecutionPack, error)
	Update(ctx context.Context, id int64, in store.ExecutionPack, status string) (store.ExecutionPack, error)
	Rebuild(ctx context.Context, id int64) (int64, error)
	Delete(ctx context.Context, id int64) error
}

type ExecutionPacksResource struct {
	DB    *sqlx.DB
	store ExecutionPackStore
}

func NewExecutionPacksResource(db *sqlx.DB) *ExecutionPacksResource {
	return &ExecutionPacksResource{DB: db, store: store.NewExecutionPackStore(db)}
}

// executionPack aliases the store DTO so existing handler code reads unchanged.
type executionPack = store.ExecutionPack

func (rs *ExecutionPacksResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.List)
	r.Post("/", rs.Create)
	r.Put("/{id}", rs.Update)
	r.Post("/{id}/rebuild", rs.Rebuild)
	r.Delete("/{id}", rs.Delete)
	return r
}

func (rs *ExecutionPacksResource) List(w http.ResponseWriter, r *http.Request) {
	packs, err := rs.store.List(r.Context())
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, packs)
}

func (rs *ExecutionPacksResource) Create(w http.ResponseWriter, r *http.Request) {
	if !requireSuperuser(w, r) {
		return
	}
	var in executionPack
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if err := validatePackSpec(in.Spec); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	// A pack with a spec OR a git source is queued for the packbuilder; one
	// registered without either (a pre-built artifact) is immediately usable.
	hasGit := in.SCMURL != nil && strings.TrimSpace(*in.SCMURL) != ""
	status := "ready"
	if hasGit || (in.Spec != nil && strings.TrimSpace(*in.Spec) != "") {
		status = "pending"
	}
	created, err := rs.store.Create(r.Context(), in, status)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, created)
}

// Update PUT /execution-packs/{id} — edit a pack's config. Editable fields are
// replaced; webhook_key is preserved unless a new non-empty value is supplied
// (it's never returned, so a client can't round-trip it). A pack with a spec or a
// git source is re-queued so the change rebuilds.
func (rs *ExecutionPacksResource) Update(w http.ResponseWriter, r *http.Request) {
	if !requireSuperuser(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	var in executionPack
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if err := validatePackSpec(in.Spec); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	hasGit := in.SCMURL != nil && strings.TrimSpace(*in.SCMURL) != ""
	status := "ready"
	if hasGit || (in.Spec != nil && strings.TrimSpace(*in.Spec) != "") {
		status = "pending"
	}
	updated, err := rs.store.Update(r.Context(), id, in, status)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, updated)
}

// Rebuild POST /execution-packs/{id}/rebuild — manually re-queue a pack for the
// packbuilder (pulls from git if git-backed, else rebuilds the stored spec).
func (rs *ExecutionPacksResource) Rebuild(w http.ResponseWriter, r *http.Request) {
	if !requireSuperuser(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	n, err := rs.store.Rebuild(r.Context(), id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if n == 0 {
		// The pack exists but has no spec and no git source — it was registered as
		// a pre-built artifact, so there's nothing for the packbuilder to rebuild.
		render.ErrInvalidRequest(fmt.Errorf("this pack was registered as a pre-built artifact (no spec or git source), so there is nothing to rebuild")).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
}

func (rs *ExecutionPacksResource) Delete(w http.ResponseWriter, r *http.Request) {
	if !requireSuperuser(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if err := rs.store.Delete(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
