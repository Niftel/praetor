-- Short-lived, user-bound previews for destructive bulk host deletion.
-- Only a digest of the opaque confirmation token is persisted.
CREATE TABLE bulk_host_delete_previews (
    token_hash TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    items JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_idempotency_key TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(token_hash) = 64),
    CHECK (jsonb_typeof(items) = 'array'),
    CHECK (consumed_idempotency_key IS NULL OR length(consumed_idempotency_key) BETWEEN 1 AND 128),
    CHECK (expires_at > created_at)
);

CREATE INDEX idx_bulk_host_delete_previews_expiry
    ON bulk_host_delete_previews (expires_at);

-- Durable per-user result ledger. It makes a confirmed request resumable and
-- gives retries the exact original response without deleting twice.
CREATE TABLE bulk_host_delete_requests (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    token_hash TEXT NOT NULL REFERENCES bulk_host_delete_previews(token_hash) ON DELETE RESTRICT,
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    complete BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, idempotency_key),
    CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    CHECK (length(token_hash) = 64),
    CHECK (jsonb_typeof(results) = 'array')
);

CREATE INDEX idx_bulk_host_delete_requests_created
    ON bulk_host_delete_requests (created_at DESC);
