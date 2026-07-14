package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
	"github.com/praetordev/praetor/services/api/dto"
	"github.com/praetordev/rbac"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
	"golang.org/x/crypto/bcrypt"
)

// UserStore is the users-domain data access.
type UserStore interface {
	List(ctx context.Context, limit, offset int) ([]models.User, error)
	Count(ctx context.Context) (int64, error)
	Get(ctx context.Context, id int64) (models.User, error)
	Create(ctx context.Context, u models.User) (models.User, error)
	Update(ctx context.Context, u models.User, setPassword bool) (models.User, error)
	Delete(ctx context.Context, id int64) (int64, error)
	Organizations(ctx context.Context, userID int64) ([]models.Organization, error)
	Teams(ctx context.Context, userID int64) ([]models.Team, error)
	ByUsernameWithHash(ctx context.Context, username string) (models.User, error)
}

// UsersResource is the self-contained users domain (extracted from ContentHandler — B6/#85).
type UsersResource struct {
	DB *sqlx.DB
	*Authorizer
	store UserStore
}

func NewUsersResource(db *sqlx.DB, authz *Authorizer) *UsersResource {
	return &UsersResource{DB: db, Authorizer: authz, store: store.NewUserStore(db)}
}

// userInput is the create/update payload: the wire user fields plus a write-only
// password (the User DTO itself never carries a password or its hash).
type userInput struct {
	dto.User
	Password string `json:"password"`
}

// ListUsers GET /api/v1/users
func (h *UsersResource) ListUsers(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)

	users, err := h.store.List(r.Context(), pg.Limit, pg.Offset)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	total, _ := h.store.Count(r.Context())

	render.JSON(w, r, &render.PaginatedResponse{
		Items:  dto.FromUsers(users),
		Total:  total,
		Limit:  pg.Limit,
		Offset: pg.Offset,
	})
}

// CreateUser POST /api/v1/users
func (h *UsersResource) CreateUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireGlobal(w, r, rbac.CapManageUser) {
		return
	}

	var input userInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if input.Username == "" || input.Password == "" {
		render.ErrInvalidRequest(fmt.Errorf("username and password are required")).Render(w, r)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	m := input.User.ToModel()
	m.PasswordHash = string(hash)

	created, err := h.store.Create(r.Context(), m)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, dto.FromUser(created))
}

// GetUser GET /api/v1/users/{id}
func (h *UsersResource) GetUser(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	user, err := h.store.Get(r.Context(), id)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}
	render.JSON(w, r, dto.FromUser(user))
}

// UpdateUser PUT /api/v1/users/{id}
func (h *UsersResource) UpdateUser(w http.ResponseWriter, r *http.Request) {
	// This endpoint can set is_superuser/is_active, so it is superuser-only.
	// Self-service profile editing belongs in a separate, field-restricted route.
	if !h.requireGlobal(w, r, rbac.CapManageUser) {
		return
	}

	id := render.GetIDParam(r)
	var input userInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	input.ID = id
	m := input.User.ToModel()

	// A non-empty password resets it; otherwise the password is left unchanged.
	setPassword := input.Password != ""
	if setPassword {
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		m.PasswordHash = string(hash)
	}

	updated, err := h.store.Update(r.Context(), m, setPassword)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if updated.ID == 0 {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}
	render.JSON(w, r, dto.FromUser(updated))
}

// DeleteUser DELETE /api/v1/users/{id}
func (h *UsersResource) DeleteUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireGlobal(w, r, rbac.CapManageUser) {
		return
	}

	id := render.GetIDParam(r)
	count, err := h.store.Delete(r.Context(), id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if count == 0 {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}
	render.NoContent(w, r)
}

// ListUserOrganizations GET /api/v1/users/{id}/organizations
// Returns organizations the user is a member of
func (h *UsersResource) ListUserOrganizations(w http.ResponseWriter, r *http.Request) {
	userID := render.GetIDParam(r)

	orgs, err := h.store.Organizations(r.Context(), userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, dto.FromOrganizations(orgs))
}

// ListUserTeams GET /api/v1/users/{id}/teams
// Returns teams the user is a member of
func (h *UsersResource) ListUserTeams(w http.ResponseWriter, r *http.Request) {
	userID := render.GetIDParam(r)

	teams, err := h.store.Teams(r.Context(), userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, dto.FromTeams(teams))
}
