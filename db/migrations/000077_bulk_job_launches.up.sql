-- Request-level idempotency ledger for bounded human-initiated bulk job
-- launches. Results are appended one item at a time in the same transaction as
-- each accepted unified_job, so an interrupted request can resume without
-- duplicating completed work.
CREATE TABLE bulk_job_launch_requests (
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key  TEXT NOT NULL,
    request_hash     TEXT NOT NULL,
    results          JSONB NOT NULL DEFAULT '[]'::JSONB,
    complete         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ,
    PRIMARY KEY (user_id, idempotency_key),
    CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    CHECK (length(request_hash) = 64),
    CHECK (jsonb_typeof(results) = 'array')
);

CREATE INDEX idx_bulk_job_launch_requests_created
    ON bulk_job_launch_requests (created_at DESC);
