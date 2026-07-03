-- Personal access tokens for headless / CI API auth. A token authenticates AS
-- its owning user (inherits that user's RBAC); only the SHA-256 hash is stored,
-- never the plaintext (shown once at creation).
CREATE TABLE IF NOT EXISTS api_tokens (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,            -- NULL = never expires
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
