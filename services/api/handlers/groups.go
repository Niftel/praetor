package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/services/api/render"
)

// GroupsResource handles group operations within inventories
type GroupsResource struct {
	DB *sqlx.DB
}

// NewGroupsResource creates a new groups resource handler
func NewGroupsResource(db *sqlx.DB) *GroupsResource {
	return &GroupsResource{DB: db}
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

	var groups []models.Group
	query := `SELECT * FROM groups WHERE inventory_id = $1 ORDER BY name`
	err = rs.DB.SelectContext(r.Context(), &groups, query, inventoryId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if groups == nil {
		groups = []models.Group{}
	}

	render.JSON(w, r, groups)
}

// CreateGroup POST /api/v1/inventories/{inventoryId}/groups
func (rs *GroupsResource) CreateGroup(w http.ResponseWriter, r *http.Request) {
	inventoryIdStr := chi.URLParam(r, "inventoryId")
	inventoryId, err := strconv.ParseInt(inventoryIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var input models.Group
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if input.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	input.InventoryID = inventoryId

	if input.Variables == nil {
		input.Variables = json.RawMessage("{}")
	}

	query := `
		INSERT INTO groups (inventory_id, name, description, variables) 
		VALUES ($1, $2, $3, $4) 
		RETURNING *`

	var created models.Group
	err = rs.DB.QueryRowxContext(r.Context(), query,
		input.InventoryID, input.Name, input.Description, input.Variables,
	).StructScan(&created)

	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.Created(w, r, created)
}

// GetGroup GET /api/v1/groups/{groupId}
func (rs *GroupsResource) GetGroup(w http.ResponseWriter, r *http.Request) {
	groupIdStr := chi.URLParam(r, "groupId")
	groupId, err := strconv.ParseInt(groupIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var group models.Group
	query := `SELECT * FROM groups WHERE id = $1`
	err = rs.DB.GetContext(r.Context(), &group, query, groupId)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return
	}

	render.JSON(w, r, group)
}

// UpdateGroup PUT /api/v1/groups/{groupId}
func (rs *GroupsResource) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	groupIdStr := chi.URLParam(r, "groupId")
	groupId, err := strconv.ParseInt(groupIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var input models.Group
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	query := `
		UPDATE groups 
		SET name = $2, description = $3, variables = $4, modified_at = now()
		WHERE id = $1 
		RETURNING *`

	var updated models.Group
	err = rs.DB.QueryRowxContext(r.Context(), query,
		groupId, input.Name, input.Description, input.Variables,
	).StructScan(&updated)

	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, updated)
}

// DeleteGroup DELETE /api/v1/groups/{groupId}
func (rs *GroupsResource) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	groupIdStr := chi.URLParam(r, "groupId")
	groupId, err := strconv.ParseInt(groupIdStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	query := `DELETE FROM groups WHERE id = $1`
	_, err = rs.DB.ExecContext(r.Context(), query, groupId)
	if err != nil {
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

	var input struct {
		HostID int64 `json:"host_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	query := `INSERT INTO host_groups (host_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err = rs.DB.ExecContext(r.Context(), query, input.HostID, groupId)
	if err != nil {
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

	query := `DELETE FROM host_groups WHERE host_id = $1 AND group_id = $2`
	_, err = rs.DB.ExecContext(r.Context(), query, hostId, groupId)
	if err != nil {
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

	var hosts []models.Host
	query := `
		SELECT h.* FROM hosts h
		JOIN host_groups hg ON h.id = hg.host_id
		WHERE hg.group_id = $1
		ORDER BY h.name`
	err = rs.DB.SelectContext(r.Context(), &hosts, query, groupId)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if hosts == nil {
		hosts = []models.Host{}
	}

	render.JSON(w, r, hosts)
}
