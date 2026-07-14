package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/dto"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
)

// GroupStore is the groups-domain data access the handler depends on.
type GroupStore interface {
	InventoryIDForGroup(ctx context.Context, groupID int64) (int64, error)
	ListByInventory(ctx context.Context, inventoryID int64) ([]models.Group, error)
	Get(ctx context.Context, id int64) (models.Group, error)
	Create(ctx context.Context, input models.Group) (models.Group, error)
	Update(ctx context.Context, id int64, input models.Group) (models.Group, error)
	Delete(ctx context.Context, id int64) error
	AddHost(ctx context.Context, groupID, hostID int64) error
	RemoveHost(ctx context.Context, groupID, hostID int64) error
	HostsInGroup(ctx context.Context, groupID int64) ([]models.Host, error)
}

// GroupsResource handles group operations within inventories
type GroupsResource struct {
	DB *sqlx.DB
	*Authorizer
	store GroupStore
}

// NewGroupsResource creates a new groups resource handler
func NewGroupsResource(db *sqlx.DB, authz *Authorizer) *GroupsResource {
	return &GroupsResource{DB: db, Authorizer: authz, store: store.NewGroupStore(db)}
}

// authorizeGroup enforces access on a group via its parent inventory's roles
// (groups have no object-roles of their own).
func (rs *GroupsResource) authorizeGroup(w http.ResponseWriter, r *http.Request, groupID int64, action permAction) bool {
	invID, err := rs.store.InventoryIDForGroup(r.Context(), groupID)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return false
	}
	return rs.authorize(w, r, rbac.ContentTypeInventory, invID, action)
}

// Routes creates a REST router for groups
func (rs *GroupsResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListGroups)
	r.Post("/", rs.CreateGroup)
	return r
}

// GroupRoutes for individual group operations
func (rs *GroupsResource) GroupRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/{groupId}", rs.GetGroup)
	r.Put("/{groupId}", rs.UpdateGroup)
	r.Delete("/{groupId}", rs.DeleteGroup)
	r.Post("/{groupId}/hosts", rs.AddHostToGroup)
	r.Delete("/{groupId}/hosts/{hostId}", rs.RemoveHostFromGroup)
	r.Get("/{groupId}/hosts", rs.ListGroupHosts)
	return r
}

// ListGroups GET /api/v1/inventories/{inventoryId}/groups
func (rs *GroupsResource) ListGroups(w http.ResponseWriter, r *http.Request) {
	inventoryIdStr := chi.URLParam(r, "inventoryId")
	inventoryId, err := strconv.ParseInt(inventoryIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeInventory, inventoryId, actRead) {
		return
	}

	groups, err := rs.store.ListByInventory(r.Context(), inventoryId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, dto.FromGroups(groups))
}

// CreateGroup POST /api/v1/inventories/{inventoryId}/groups
func (rs *GroupsResource) CreateGroup(w http.ResponseWriter, r *http.Request) {
	inventoryIdStr := chi.URLParam(r, "inventoryId")
	inventoryId, err := strconv.ParseInt(inventoryIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeInventory, inventoryId, actAdmin) {
		return
	}

	var body dto.Group
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	input := body.ToModel()

	if input.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	input.InventoryID = inventoryId

	if input.Variables == nil {
		input.Variables = json.RawMessage("{}")
	}

	created, err := rs.store.Create(r.Context(), input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.Created(w, r, dto.FromGroup(created))
}

// GetGroup GET /api/v1/groups/{groupId}
func (rs *GroupsResource) GetGroup(w http.ResponseWriter, r *http.Request) {
	groupIdStr := chi.URLParam(r, "groupId")
	groupId, err := strconv.ParseInt(groupIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorizeGroup(w, r, groupId, actRead) {
		return
	}

	group, err := rs.store.Get(r.Context(), groupId)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return
	}

	render.JSON(w, r, dto.FromGroup(group))
}

// UpdateGroup PUT /api/v1/groups/{groupId}
func (rs *GroupsResource) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	groupIdStr := chi.URLParam(r, "groupId")
	groupId, err := strconv.ParseInt(groupIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorizeGroup(w, r, groupId, actAdmin) {
		return
	}

	var body dto.Group
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	updated, err := rs.store.Update(r.Context(), groupId, body.ToModel())
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, dto.FromGroup(updated))
}

// DeleteGroup DELETE /api/v1/groups/{groupId}
func (rs *GroupsResource) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	groupIdStr := chi.URLParam(r, "groupId")
	groupId, err := strconv.ParseInt(groupIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorizeGroup(w, r, groupId, actAdmin) {
		return
	}

	if err := rs.store.Delete(r.Context(), groupId); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AddHostToGroup POST /api/v1/groups/{groupId}/hosts
func (rs *GroupsResource) AddHostToGroup(w http.ResponseWriter, r *http.Request) {
	groupIdStr := chi.URLParam(r, "groupId")
	groupId, err := strconv.ParseInt(groupIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorizeGroup(w, r, groupId, actAdmin) {
		return
	}

	var input struct {
		HostID int64 `json:"host_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if err := rs.store.AddHost(r.Context(), groupId, input.HostID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// RemoveHostFromGroup DELETE /api/v1/groups/{groupId}/hosts/{hostId}
func (rs *GroupsResource) RemoveHostFromGroup(w http.ResponseWriter, r *http.Request) {
	groupIdStr := chi.URLParam(r, "groupId")
	groupId, err := strconv.ParseInt(groupIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	hostIdStr := chi.URLParam(r, "hostId")
	hostId, err := strconv.ParseInt(hostIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorizeGroup(w, r, groupId, actAdmin) {
		return
	}

	if err := rs.store.RemoveHost(r.Context(), groupId, hostId); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListGroupHosts GET /api/v1/groups/{groupId}/hosts
func (rs *GroupsResource) ListGroupHosts(w http.ResponseWriter, r *http.Request) {
	groupIdStr := chi.URLParam(r, "groupId")
	groupId, err := strconv.ParseInt(groupIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorizeGroup(w, r, groupId, actRead) {
		return
	}

	hosts, err := rs.store.HostsInGroup(r.Context(), groupId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, dto.FromHosts(hosts))
}
