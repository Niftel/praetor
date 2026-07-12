package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/render"
	"github.com/praetordev/praetor/services/api/store"
)

// HostStore is the hosts-domain data access the handler depends on.
type HostStore interface {
	InventoryIDForHost(ctx context.Context, hostID int64) (int64, error)
	Facts(ctx context.Context, hostID int64) json.RawMessage
	ListByInventory(ctx context.Context, inventoryID int64) ([]models.Host, error)
	Get(ctx context.Context, id int64) (models.Host, error)
	Create(ctx context.Context, input models.Host) (models.Host, error)
	Update(ctx context.Context, id int64, host models.Host) (models.Host, error)
	Delete(ctx context.Context, id int64) error
	GroupsForHost(ctx context.Context, hostID int64) ([]models.Group, error)
	SetRunner(ctx context.Context, hostID int64) (models.Host, error)
	RunnerHeartbeat(ctx context.Context, hostID int64) error
}

// HostsResource handles host operations within inventories
type HostsResource struct {
	DB *sqlx.DB
	*Authorizer
	store HostStore
}

// NewHostsResource creates a new hosts resource handler
func NewHostsResource(db *sqlx.DB) *HostsResource {
	return &HostsResource{DB: db, Authorizer: NewAuthorizer(db), store: store.NewHostStore(db)}
}

// authorizeHost enforces access on a host via its parent inventory's roles
// (hosts have no object-roles of their own). Returns false (and writes the
// response) when denied or the host does not exist.
func (rs *HostsResource) authorizeHost(w http.ResponseWriter, r *http.Request, hostID int64, action permAction) bool {
	invID, err := rs.store.InventoryIDForHost(r.Context(), hostID)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return false
	}
	return rs.authorize(w, r, rbac.ContentTypeInventory, invID, action)
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
	r.Get("/{hostId}/facts", rs.GetHostFacts)
	return r
}

// GetHostFacts GET /api/v1/hosts/{hostId}/facts — the host's cached ansible_facts.
func (rs *HostsResource) GetHostFacts(w http.ResponseWriter, r *http.Request) {
	hostId, err := strconv.ParseInt(chi.URLParam(r, "hostId"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.authorizeHost(w, r, hostId, actRead) {
		return
	}
	render.JSON(w, r, rs.store.Facts(r.Context(), hostId))
}

// ListHosts GET /api/v1/inventories/{inventoryId}/hosts
func (rs *HostsResource) ListHosts(w http.ResponseWriter, r *http.Request) {
	inventoryIdStr := chi.URLParam(r, "inventoryId")
	inventoryId, err := strconv.ParseInt(inventoryIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeInventory, inventoryId, actRead) {
		return
	}

	hosts, err := rs.store.ListByInventory(r.Context(), inventoryId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
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

	if !rs.authorize(w, r, rbac.ContentTypeInventory, inventoryId, actAdmin) {
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

	created, err := rs.store.Create(r.Context(), input)
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

	if !rs.authorizeHost(w, r, hostId, actRead) {
		return
	}

	host, err := rs.store.Get(r.Context(), hostId)
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

	if !rs.authorizeHost(w, r, hostId, actAdmin) {
		return
	}

	// Fetch existing host
	host, err := rs.store.Get(r.Context(), hostId)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return
	}

	// Decode updates into the existing host struct (Merge)
	if err := json.NewDecoder(r.Body).Decode(&host); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	updated, err := rs.store.Update(r.Context(), hostId, host)
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

	if !rs.authorizeHost(w, r, hostId, actAdmin) {
		return
	}

	if err := rs.store.Delete(r.Context(), hostId); err != nil {
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

	if !rs.authorizeHost(w, r, hostId, actRead) {
		return
	}

	groups, err := rs.store.GroupsForHost(r.Context(), hostId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
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

	if !rs.authorizeHost(w, r, hostId, actAdmin) {
		return
	}

	host, err := rs.store.SetRunner(r.Context(), hostId)
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
	if err := rs.store.RunnerHeartbeat(r.Context(), hostId); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"status":    "ok",
		"host_id":   hostId,
		"timestamp": time.Now(),
	})
}
