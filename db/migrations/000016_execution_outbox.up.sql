-- Transactional outbox for job launches. The scheduler inserts an execution
-- request here in the SAME transaction that creates the execution_run and marks
-- the job queued, so a launch is never lost (committed job with no request) nor
-- duplicated (request sent for an uncommitted run). A relay publishes pending
-- rows to the durable request stream and marks them sent.
CREATE TABLE IF NOT EXISTS execution_outbox (
    id               BIGSERIAL PRIMARY KEY,
    execution_run_id UUID NOT NULL REFERENCES execution_runs(id) ON DELETE CASCADE,
    payload          JSONB NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending', -- pending, sent, failed
    attempts         INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at          TIMESTAMPTZ
);

-- Partial index keeps the relay's "find unsent" scan cheap as sent rows pile up.
CREATE INDEX IF NOT EXISTS idx_execution_outbox_pending
    ON execution_outbox (id) WHERE status = 'pending';
