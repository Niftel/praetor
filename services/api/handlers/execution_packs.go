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
	// Git source: when set, the packbuilder pulls the spec from the repo and a push
	// webhook rebuilds it. WebhookKey is write-only (accepted, never returned).
	SCMURL     *string `json:"scm_url,omitempty" db:"scm_url"`
	SCMBranch  *string `json:"scm_branch,omitempty" db:"scm_branch"`
	SpecPath   *string `json:"spec_path,omitempty" db:"spec_path"`
	WebhookKey *string `json:"webhook_key,omitempty" db:"webhook_key"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

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
	packs := []executionPack{}
	if err := rs.DB.SelectContext(r.Context(), &packs,
		`SELECT id, name, description, spec, status, build_log, scm_url, scm_branch, spec_path, created_at FROM execution_packs ORDER BY name`); err != nil {
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
	// A pack with a spec OR a git source is queued for the packbuilder; one
	// registered without either (a pre-built artifact) is immediately usable.
	hasGit := in.SCMURL != nil && strings.TrimSpace(*in.SCMURL) != ""
	status := "ready"
	if hasGit || (in.Spec != nil && strings.TrimSpace(*in.Spec) != "") {
		status = "pending"
	}
	var created executionPack
	if err := rs.DB.QueryRowxContext(r.Context(),
		`INSERT INTO execution_packs (name, description, spec, status, scm_url, scm_branch, spec_path, webhook_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, name, description, spec, status, build_log, scm_url, scm_branch, spec_path, created_at`,
		in.Name, in.Description, in.Spec, status, in.SCMURL, in.SCMBranch, in.SpecPath, in.WebhookKey).StructScan(&created); err != nil {
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
	hasGit := in.SCMURL != nil && strings.TrimSpace(*in.SCMURL) != ""
	status := "ready"
	if hasGit || (in.Spec != nil && strings.TrimSpace(*in.Spec) != "") {
		status = "pending"
	}
	var updated executionPack
	if err := rs.DB.QueryRowxContext(r.Context(),
		`UPDATE execution_packs SET
		   name=$2, description=$3, spec=$4, scm_url=$5, scm_branch=$6, spec_path=$7,
		   webhook_key=COALESCE(NULLIF($8,''), webhook_key),
		   status=$9, build_log=NULL
		 WHERE id=$1
		 RETURNING id, name, description, spec, status, build_log, scm_url, scm_branch, spec_path, created_at`,
		id, in.Name, in.Description, in.Spec, in.SCMURL, in.SCMBranch, in.SpecPath, in.WebhookKey, status,
	).StructScan(&updated); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, updated)
}

// Rebuild POST /execution-packs/{id}/rebuild — manually re-queue a pack for the
// packbuilder (pulls from git if git-backed, else rebuilds the stored spec).
func (rs *ExecutionPacksResource) Rebuild(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	res, err := rs.DB.ExecContext(r.Context(),
		`UPDATE execution_packs SET status='pending', build_log=NULL
		 WHERE id=$1 AND (spec IS NOT NULL OR scm_url IS NOT NULL)`, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		render.ErrInvalidRequest(nil).Render(w, r) // nothing buildable (no spec/git source)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
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
