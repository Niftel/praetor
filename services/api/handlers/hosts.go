package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/services/api/render"
)

// HostsResource handles host operations within inventories
type HostsResource struct {
	DB *sqlx.DB
}

// NewHostsResource creates a new hosts resource handler
func NewHostsResource(db *sqlx.DB) *HostsResource {
	return &HostsResource{DB: db}
}

// Routes creates a REST router for hosts
func (rs *HostsResource) Routes() chi.Router {
	r := chi.NewRouter()
	// Nested under /inventories/{inventoryId}/hosts
	r.Get("/", rs.ListHosts)
	r.Post("/", rs.CreateHost)
	return r
}

// HostRoutes for individual host operations
func (rs *HostsResource) HostRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/{hostId}", rs.GetHost)
	r.Put("/{hostId}", rs.UpdateHost)
	r.Delete("/{hostId}", rs.DeleteHost)
	r.Get("/{hostId}/groups", rs.GetHostGroups)
	r.Post("/{hostId}/set-runner", rs.SetRunnerHost)
	return r
}

// ListHosts GET /api/v1/inventories/{inventoryId}/hosts
func (rs *HostsResource) ListHosts(w http.ResponseWriter, r *http.Request) {
	inventoryIdStr := chi.URLParam(r, "inventoryId")
	inventoryId, err := strconv.ParseInt(inventoryIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var hosts []models.Host
	query := `SELECT * FROM hosts WHERE inventory_id = $1 ORDER BY name`
	err = rs.DB.SelectContext(r.Context(), &hosts, query, inventoryId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if hosts == nil {
		hosts = []models.Host{}
	}

	render.JSON(w, r, hosts)
}

// CreateHost POST /api/v1/inventories/{inventoryId}/hosts
func (rs *HostsResource) CreateHost(w http.ResponseWriter, r *http.Request) {
	inventoryIdStr := chi.URLParam(r, "inventoryId")
	inventoryId, err := strconv.ParseInt(inventoryIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var input models.Host
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if input.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	input.InventoryID = inventoryId

	// Default variables to empty object if nil
	if input.Variables == nil {
		input.Variables = json.RawMessage("{}")
	}

	query := `
		INSERT INTO hosts (inventory_id, name, description, variables, enabled, is_control_node) 
		VALUES ($1, $2, $3, $4, $5, $6) 
		RETURNING *`

	var created models.Host
	err = rs.DB.QueryRowxContext(r.Context(), query,
		input.InventoryID, input.Name, input.Description,
		input.Variables, true, input.IsControlNode,
	).StructScan(&created)

	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.Created(w, r, created)
}

// GetHost GET /api/v1/hosts/{hostId}
func (rs *HostsResource) GetHost(w http.ResponseWriter, r *http.Request) {
	hostIdStr := chi.URLParam(r, "hostId")
	hostId, err := strconv.ParseInt(hostIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var host models.Host
	query := `SELECT * FROM hosts WHERE id = $1`
	err = rs.DB.GetContext(r.Context(), &host, query, hostId)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return
	}

	render.JSON(w, r, host)
}

// UpdateHost PUT /api/v1/hosts/{hostId}
// UpdateHost PUT /api/v1/hosts/{hostId}
func (rs *HostsResource) UpdateHost(w http.ResponseWriter, r *http.Request) {
	hostIdStr := chi.URLParam(r, "hostId")
	hostId, err := strconv.ParseInt(hostIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Fetch existing host
	var host models.Host
	query := `SELECT * FROM hosts WHERE id = $1`
	err = rs.DB.GetContext(r.Context(), &host, query, hostId)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return
	}

	// Decode updates into the existing host struct (Merge)
	if err := json.NewDecoder(r.Body).Decode(&host); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	query = `
		UPDATE hosts 
		SET name = $2, description = $3, variables = $4, enabled = $5, is_control_node = $6, modified_at = now()
		WHERE id = $1 
		RETURNING *`

	var updated models.Host
	err = rs.DB.QueryRowxContext(r.Context(), query,
		hostId, host.Name, host.Description, host.Variables, host.Enabled, host.IsControlNode,
	).StructScan(&updated)

	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, updated)
}

// DeleteHost DELETE /api/v1/hosts/{hostId}
func (rs *HostsResource) DeleteHost(w http.ResponseWriter, r *http.Request) {
	hostIdStr := chi.URLParam(r, "hostId")
	hostId, err := strconv.ParseInt(hostIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	query := `DELETE FROM hosts WHERE id = $1`
	_, err = rs.DB.ExecContext(r.Context(), query, hostId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetHostGroups GET /api/v1/hosts/{hostId}/groups - returns groups this host belongs to
func (rs *HostsResource) GetHostGroups(w http.ResponseWriter, r *http.Request) {
	hostIdStr := chi.URLParam(r, "hostId")
	hostId, err := strconv.ParseInt(hostIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var groups []models.Group
	query := `
		SELECT g.* FROM groups g
		JOIN host_groups hg ON g.id = hg.group_id
		WHERE hg.host_id = $1
		ORDER BY g.name`
	err = rs.DB.SelectContext(r.Context(), &groups, query, hostId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if groups == nil {
		groups = []models.Group{}
	}

	render.JSON(w, r, groups)
}

// SetRunnerHost POST /api/v1/hosts/{hostId}/set-runner - sets this host as the runner for its inventory
func (rs *HostsResource) SetRunnerHost(w http.ResponseWriter, r *http.Request) {
	hostIdStr := chi.URLParam(r, "hostId")
	hostId, err := strconv.ParseInt(hostIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Get the host's inventory_id
	var inventoryId int64
	err = rs.DB.GetContext(r.Context(), &inventoryId, `SELECT inventory_id FROM hosts WHERE id = $1`, hostId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	// Clear previous runner host in this inventory
	_, err = rs.DB.ExecContext(r.Context(), `UPDATE hosts SET is_runner_host = FALSE WHERE inventory_id = $1`, inventoryId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	// Set this host as runner
	_, err = rs.DB.ExecContext(r.Context(), `UPDATE hosts SET is_runner_host = TRUE WHERE id = $1`, hostId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	// Return the updated host
	var host models.Host
	err = rs.DB.GetContext(r.Context(), &host, `SELECT * FROM hosts WHERE id = $1`, hostId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, host)
}

// RunnerHeartbeat POST /api/v1/hosts/{hostId}/runner-heartbeat - called by host-runner agent to report health
func (rs *HostsResource) RunnerHeartbeat(w http.ResponseWriter, r *http.Request) {
	hostIdStr := chi.URLParam(r, "hostId")
	hostId, err := strconv.ParseInt(hostIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Update runner health status
	_, err = rs.DB.ExecContext(r.Context(), `
		UPDATE hosts 
		SET runner_last_seen = NOW(), runner_healthy = TRUE 
		WHERE id = $1 AND is_runner_host = TRUE
	`, hostId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"status":    "ok",
		"host_id":   hostId,
		"timestamp": time.Now(),
	})
}
