-- Durable request ledger for bounded, resumable, per-user bulk host creation.
CREATE TABLE bulk_host_create_requests (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    complete BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, idempotency_key),
    CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    CHECK (length(request_hash) = 64),
    CHECK (jsonb_typeof(results) = 'array')
);

CREATE INDEX idx_bulk_host_create_requests_created
    ON bulk_host_create_requests (created_at DESC);
