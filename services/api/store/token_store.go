package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
)

// APIToken is the metadata view of a personal access token (never the secret).
type APIToken struct {
	ID         int64      `json:"id" db:"id"`
	Name       string     `json:"name" db:"name"`
	LastUsedAt *time.Time `json:"last_used_at" db:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
}

// TokenStore is the data-access layer for personal access tokens.
type TokenStore struct {
	db *sqlx.DB
}

func NewTokenStore(db *sqlx.DB) *TokenStore { return &TokenStore{db: db} }

// ListForUser returns a user's tokens (metadata only), newest first.
func (s *TokenStore) ListForUser(ctx context.Context, userID int64) ([]APIToken, error) {
	tokens := []APIToken{}
	err := s.db.SelectContext(ctx, &tokens,
		`SELECT id, name, last_used_at, expires_at, created_at
		 FROM api_tokens WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	return tokens, wrap("TokenStore.ListForUser", err)
}

// Create inserts a token (hash precomputed by the caller) and returns its view.
func (s *TokenStore) Create(ctx context.Context, userID int64, name, tokenHash string, expiresAt *time.Time) (APIToken, error) {
	var out APIToken
	err := s.db.GetContext(ctx, &out,
		`INSERT INTO api_tokens (user_id, name, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, name, last_used_at, expires_at, created_at`,
		userID, name, tokenHash, expiresAt)
	return out, wrap("TokenStore.Create", err)
}

// Revoke deletes a token. When restrictToUser is non-nil the delete is scoped to
// that owner (a superuser passes nil to revoke any). Returns rows affected.
func (s *TokenStore) Revoke(ctx context.Context, id int64, restrictToUser *int64) (int64, error) {
	var res interface{ RowsAffected() (int64, error) }
	var err error
	if restrictToUser == nil {
		res, err = s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = $1`, id)
	} else {
		res, err = s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`, id, *restrictToUser)
	}
	if err != nil {
		return 0, wrap("TokenStore.Revoke", err)
	}
	return res.RowsAffected()
}
