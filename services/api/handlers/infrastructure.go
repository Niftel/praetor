package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
)

type InfrastructureResource struct {
	DB *sqlx.DB
}

func NewInfrastructureResource(db *sqlx.DB) *InfrastructureResource {
	return &InfrastructureResource{DB: db}
}

// Routes creates a REST router for the infrastructure resource
func (rs *InfrastructureResource) Routes() chi.Router {
	r := chi.NewRouter()

	// Instances
	r.Get("/instances", rs.ListInstances)
	r.Post("/instances", rs.CreateInstance)
	r.Post("/instances/register", rs.RegisterInstance) // Auto-discovery registration
	r.Route("/instances/{id}", func(r chi.Router) {
		r.Put("/", rs.UpdateInstance)
		r.Delete("/", rs.DeleteInstance)
		r.Post("/heartbeat", rs.HeartbeatInstance) // Heartbeat endpoint
	})

	// Instance Groups
	r.Get("/instance_groups", rs.ListInstanceGroups)
	r.Post("/instance_groups", rs.CreateInstanceGroup)
	r.Route("/instance_groups/{id}", func(r chi.Router) {
		r.Delete("/", rs.DeleteInstanceGroup)
	})

	// Execution targets: the runner hosts jobs bootstrap onto, with the health
	// the host-runner reports via /hosts/{id}/runner-heartbeat.
	r.Get("/infrastructure/runner-hosts", rs.ListRunnerHosts)

	return r
}

// RunnerHostView is a runner host (an execution target) plus its owning
// inventory's name, for the infrastructure view.
type RunnerHostView struct {
	ID             int64      `db:"id" json:"id"`
	Name           string     `db:"name" json:"name"`
	InventoryID    int64      `db:"inventory_id" json:"inventory_id"`
	InventoryName  string     `db:"inventory_name" json:"inventory_name"`
	Enabled        bool       `db:"enabled" json:"enabled"`
	IsRunnerHost   bool       `db:"is_runner_host" json:"is_runner_host"`
	RunnerHealthy  bool       `db:"runner_healthy" json:"runner_healthy"`
	RunnerLastSeen *time.Time `db:"runner_last_seen" json:"runner_last_seen"`
}

// ListRunnerHosts returns the hosts designated as job runners across all
// inventories, with their last reported health. This is the operationally
// useful half of the infrastructure view: where jobs actually execute.
func (rs *InfrastructureResource) ListRunnerHosts(w http.ResponseWriter, r *http.Request) {
	hosts := []RunnerHostView{}
	if err := rs.DB.SelectContext(r.Context(), &hosts, `
		SELECT h.id, h.name, h.inventory_id, i.name AS inventory_name,
		       h.enabled, h.is_runner_host, h.runner_healthy, h.runner_last_seen
		FROM hosts h
		JOIN inventories i ON i.id = h.inventory_id
		WHERE h.is_runner_host = true
		ORDER BY h.runner_last_seen DESC NULLS LAST, h.name`); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.JSON(w, r, hosts)
}

// --- Instances ---

func (rs *InfrastructureResource) ListInstances(w http.ResponseWriter, r *http.Request) {
	query := `SELECT * FROM instances ORDER BY id ASC`
	instances := []models.Instance{}
	if err := rs.DB.SelectContext(r.Context(), &instances, query); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.JSON(w, r, instances)
}

type InstanceRequest struct {
	Hostname string `json:"hostname"`
	Capacity int    `json:"capacity"`
	Enabled  bool   `json:"enabled"`
}

func (req *InstanceRequest) Bind(r *http.Request) error {
	return nil
}

func (rs *InfrastructureResource) CreateInstance(w http.ResponseWriter, r *http.Request) {
	data := &InstanceRequest{Capacity: 100, Enabled: true} // Defaults
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	instance := models.Instance{
		Hostname:   data.Hostname,
		Capacity:   data.Capacity,
		Enabled:    data.Enabled,
		CreatedAt:  time.Now(),
		ModifiedAt: time.Now(),
	}

	query := `INSERT INTO instances (hostname, capacity, enabled, created_at, modified_at) 
	          VALUES (:hostname, :capacity, :enabled, :created_at, :modified_at) RETURNING id`

	rows, err := rs.DB.NamedQueryContext(r.Context(), query, instance)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	defer rows.Close()
	if rows.Next() {
		rows.Scan(&instance.ID)
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, instance)
}

func (rs *InfrastructureResource) UpdateInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)

	data := &InstanceRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	query := `UPDATE instances SET hostname=:hostname, capacity=:capacity, enabled=:enabled, modified_at=:modified_at WHERE id=:id`

	instance := models.Instance{
		ID:         int64(id),
		Hostname:   data.Hostname,
		Capacity:   data.Capacity,
		Enabled:    data.Enabled,
		ModifiedAt: time.Now(),
	}

	if _, err := rs.DB.NamedExecContext(r.Context(), query, instance); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}

	render.JSON(w, r, instance)
}

func (rs *InfrastructureResource) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)

	if _, err := rs.DB.ExecContext(r.Context(), "DELETE FROM instances WHERE id = $1", id); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.Status(r, http.StatusOK)
}

// RegisterInstance POST /api/v1/instances/register - upsert instance by hostname
type RegisterRequest struct {
	Hostname     string  `json:"hostname"`
	Version      *string `json:"version,omitempty"`
	Capacity     int     `json:"capacity"`
	InstanceType string  `json:"instance_type"` // executor, controller, hybrid
	IPAddress    *string `json:"ip_address,omitempty"`
}

func (req *RegisterRequest) Bind(r *http.Request) error {
	return nil
}

func (rs *InfrastructureResource) RegisterInstance(w http.ResponseWriter, r *http.Request) {
	data := &RegisterRequest{Capacity: 100, InstanceType: "executor"}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	if data.Hostname == "" {
		render.Render(w, r, ErrInvalidRequest(nil))
		return
	}

	now := time.Now()

	// Clean up stale instances of the same type (heartbeat > 5 min ago)
	// This prevents accumulation of old container entries when stack is rebuilt
	_, _ = rs.DB.ExecContext(r.Context(), `
		DELETE FROM instances 
		WHERE instance_type = $1 
		AND hostname != $2
		AND (last_heartbeat IS NULL OR last_heartbeat < NOW() - INTERVAL '5 minutes')`,
		data.InstanceType, data.Hostname)

	// Upsert: insert or update existing instance by hostname
	query := `
		INSERT INTO instances (hostname, version, capacity, enabled, instance_type, healthy, last_heartbeat, ip_address, created_at, modified_at)
		VALUES ($1, $2, $3, TRUE, $4, TRUE, $5, $6, $5, $5)
		ON CONFLICT (hostname) DO UPDATE SET
			version = EXCLUDED.version,
			capacity = EXCLUDED.capacity,
			instance_type = EXCLUDED.instance_type,
			healthy = TRUE,
			last_heartbeat = EXCLUDED.last_heartbeat,
			ip_address = EXCLUDED.ip_address,
			modified_at = EXCLUDED.modified_at
		RETURNING *`

	var instance models.Instance
	err := rs.DB.QueryRowxContext(r.Context(), query,
		data.Hostname, data.Version, data.Capacity, data.InstanceType, now, data.IPAddress,
	).StructScan(&instance)

	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, instance)
}

// HeartbeatInstance POST /api/v1/instances/{id}/heartbeat - update heartbeat timestamp
func (rs *InfrastructureResource) HeartbeatInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	now := time.Now()
	query := `UPDATE instances SET healthy = TRUE, last_heartbeat = $2, modified_at = $2 WHERE id = $1 RETURNING *`

	var instance models.Instance
	err = rs.DB.QueryRowxContext(r.Context(), query, id, now).StructScan(&instance)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}

	render.JSON(w, r, instance)
}

// --- Instance Groups ---

func (rs *InfrastructureResource) ListInstanceGroups(w http.ResponseWriter, r *http.Request) {
	query := `SELECT * FROM instance_groups ORDER BY id ASC`
	groups := []models.InstanceGroup{}
	if err := rs.DB.SelectContext(r.Context(), &groups, query); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.JSON(w, r, groups)
}

type InstanceGroupRequest struct {
	Name string `json:"name"`
}

func (req *InstanceGroupRequest) Bind(r *http.Request) error {
	return nil
}

func (rs *InfrastructureResource) CreateInstanceGroup(w http.ResponseWriter, r *http.Request) {
	data := &InstanceGroupRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	group := models.InstanceGroup{
		Name:       data.Name,
		CreatedAt:  time.Now(),
		ModifiedAt: time.Now(),
	}

	query := `INSERT INTO instance_groups (name, created_at, modified_at) 
	          VALUES (:name, :created_at, :modified_at) RETURNING id`

	rows, err := rs.DB.NamedQueryContext(r.Context(), query, group)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	defer rows.Close()
	if rows.Next() {
		rows.Scan(&group.ID)
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, group)
}

func (rs *InfrastructureResource) DeleteInstanceGroup(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)

	if _, err := rs.DB.ExecContext(r.Context(), "DELETE FROM instance_groups WHERE id = $1", id); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.Status(r, http.StatusOK)
}
