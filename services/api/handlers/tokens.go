package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/middleware"
	render "github.com/praetordev/render"
	"github.com/praetordev/store"
)

// TokenStore is the tokens-domain data access the handler depends on.
type TokenStore interface {
	ListForUser(ctx context.Context, userID int64) ([]store.APIToken, error)
	Create(ctx context.Context, userID int64, name, tokenHash string, expiresAt *time.Time) (store.APIToken, error)
	Revoke(ctx context.Context, id int64, restrictToUser *int64) (int64, error)
}

// TokensResource manages a user's personal access tokens (headless/CI API auth).
type TokensResource struct {
	*Authorizer
	DB    *sqlx.DB
	store TokenStore
}

func NewTokensResource(db *sqlx.DB, authz *Authorizer) *TokensResource {
	return &TokensResource{Authorizer: authz, DB: db, store: store.NewTokenStore(db)}
}

func (rs *TokensResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.List)
	r.Post("/", rs.Create)
	r.Delete("/{id}", rs.Revoke)
	return r
}

// List returns the calling user's tokens (metadata only — never the secret).
func (rs *TokensResource) List(w http.ResponseWriter, r *http.Request) {
	uc := currentUser(r)
	tokens, err := rs.store.ListForUser(r.Context(), uc.UserID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, tokens)
}

// Create mints a new token for the calling user. The plaintext is returned ONCE;
// only its hash is stored.
func (rs *TokensResource) Create(w http.ResponseWriter, r *http.Request) {
	uc := currentUser(r)
	var body struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Name == "" {
		body.Name = "token"
	}

	// 32 bytes of CSPRNG entropy, url-safe, behind a recognizable prefix.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	plaintext := middleware.PATPrefix + base64.RawURLEncoding.EncodeToString(raw)

	out, err := rs.store.Create(r.Context(), uc.UserID, body.Name, middleware.HashToken(plaintext), body.ExpiresAt)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	// The one and only time the plaintext is exposed.
	render.Created(w, r, map[string]interface{}{
		"id": out.ID, "name": out.Name, "expires_at": out.ExpiresAt,
		"created_at": out.CreatedAt, "token": plaintext,
	})
}

// Revoke deletes a token. A user may revoke their own; a superuser may revoke any.
func (rs *TokensResource) Revoke(w http.ResponseWriter, r *http.Request) {
	uc := currentUser(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	// Administering any user's tokens is the global manage_user capability;
	// without it a user may only revoke their own.
	isAdmin, err := rs.holdsGlobal(r, rbac.ManageUsers)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	var restrict *int64
	if !isAdmin {
		restrict = &uc.UserID
	}
	n, err := rs.store.Revoke(r.Context(), id, restrict)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if n == 0 {
		render.ErrNotFound(nil).Render(w, r)
		return
	}
	render.JSON(w, r, map[string]string{"status": "revoked"})
}
