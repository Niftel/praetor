-- Durable terminal events synthesized by the scheduler. These events must be
-- committed atomically with the state transition so consumers cannot miss
-- lifecycle notifications when NATS is temporarily unavailable.
CREATE TABLE IF NOT EXISTS job_event_outbox (
    id               BIGSERIAL PRIMARY KEY,
    execution_run_id UUID NOT NULL REFERENCES execution_runs(id) ON DELETE CASCADE,
    event_type       TEXT NOT NULL,
    payload          JSONB NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed')),
    attempts         INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at          TIMESTAMPTZ,
    UNIQUE (execution_run_id, event_type)
);

CREATE INDEX IF NOT EXISTS idx_job_event_outbox_pending
    ON job_event_outbox (id) WHERE status = 'pending';
