package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/services/api/middleware"
	render "github.com/praetordev/praetor/services/api/render"
)

// TokensResource manages a user's personal access tokens (headless/CI API auth).
type TokensResource struct {
	DB *sqlx.DB
}

func NewTokensResource(db *sqlx.DB) *TokensResource {
	return &TokensResource{DB: db}
}

func (rs *TokensResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.List)
	r.Post("/", rs.Create)
	r.Delete("/{id}", rs.Revoke)
	return r
}

type apiTokenView struct {
	ID         int64      `json:"id" db:"id"`
	Name       string     `json:"name" db:"name"`
	LastUsedAt *time.Time `json:"last_used_at" db:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
}

// List returns the calling user's tokens (metadata only — never the secret).
func (rs *TokensResource) List(w http.ResponseWriter, r *http.Request) {
	uc := currentUser(r)
	tokens := []apiTokenView{}
	if err := rs.DB.SelectContext(r.Context(), &tokens,
		`SELECT id, name, last_used_at, expires_at, created_at
		 FROM api_tokens WHERE user_id = $1 ORDER BY created_at DESC`, uc.UserID); err != nil {
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

	var out apiTokenView
	if err := rs.DB.GetContext(r.Context(), &out,
		`INSERT INTO api_tokens (user_id, name, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, name, last_used_at, expires_at, created_at`,
		uc.UserID, body.Name, middleware.HashToken(plaintext), body.ExpiresAt); err != nil {
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
	var res interface{ RowsAffected() (int64, error) }
	if uc.IsSuperuser {
		res, err = rs.DB.ExecContext(r.Context(), `DELETE FROM api_tokens WHERE id = $1`, id)
	} else {
		res, err = rs.DB.ExecContext(r.Context(), `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`, id, uc.UserID)
	}
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		render.ErrNotFound(nil).Render(w, r)
		return
	}
	render.JSON(w, r, map[string]string{"status": "revoked"})
}
