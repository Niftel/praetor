package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/services/api/render"
)

// ExecutionPacksResource manages the registry of Execution Packs — the named,
// self-contained Python+Ansible runtimes Praetor pushes onto hosts. Packs are
// built from a YAML spec via `make execpack`; this registry lets templates pick
// which pack a job runs in.
type ExecutionPacksResource struct{ DB *sqlx.DB }

func NewExecutionPacksResource(db *sqlx.DB) *ExecutionPacksResource {
	return &ExecutionPacksResource{DB: db}
}

type executionPack struct {
	ID          int64     `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description *string   `json:"description,omitempty" db:"description"`
	Spec        *string   `json:"spec,omitempty" db:"spec"`
	Status      string    `json:"status" db:"status"`
	BuildLog    *string   `json:"build_log,omitempty" db:"build_log"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

func (rs *ExecutionPacksResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.List)
	r.Post("/", rs.Create)
	r.Delete("/{id}", rs.Delete)
	return r
}

func (rs *ExecutionPacksResource) List(w http.ResponseWriter, r *http.Request) {
	packs := []executionPack{}
	if err := rs.DB.SelectContext(r.Context(), &packs,
		`SELECT id, name, description, spec, status, build_log, created_at FROM execution_packs ORDER BY name`); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, packs)
}

func (rs *ExecutionPacksResource) Create(w http.ResponseWriter, r *http.Request) {
	var in executionPack
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	// A pack with a spec is queued for the packbuilder to build; one registered
	// without a spec (a pre-built artifact) is immediately usable.
	status := "ready"
	if in.Spec != nil && strings.TrimSpace(*in.Spec) != "" {
		status = "pending"
	}
	var created executionPack
	if err := rs.DB.QueryRowxContext(r.Context(),
		`INSERT INTO execution_packs (name, description, spec, status) VALUES ($1, $2, $3, $4)
		 RETURNING id, name, description, spec, status, build_log, created_at`,
		in.Name, in.Description, in.Spec, status).StructScan(&created); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, created)
}

func (rs *ExecutionPacksResource) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if _, err := rs.DB.ExecContext(r.Context(), `DELETE FROM execution_packs WHERE id = $1`, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
