-- Pull-based reconciliation: when the host-runner's WAL never reaches the control
-- plane (host unreachable at sync time, or control plane down for the whole run),
-- a reconciler SSHes to the host and harvests status.json + events.jsonl.
--
-- To do that it needs to know which host a past run targeted (the manifest is
-- ephemeral, not persisted), plus a backoff so unreachable hosts are retried.

ALTER TABLE execution_runs
    -- Snapshot of the runner host chosen at launch, so the reconciler can reach
    -- the same host even if the inventory/host record changes later.
    ADD COLUMN IF NOT EXISTS runner_host_id     BIGINT REFERENCES hosts(id) ON DELETE SET NULL,
    -- Backoff bookkeeping for the reconcile loop.
    ADD COLUMN IF NOT EXISTS reconcile_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reconcile_after    TIMESTAMPTZ;

-- The reconciler polls for runs in the 'reconciling' state that are due.
CREATE INDEX IF NOT EXISTS idx_execution_runs_reconcile
    ON execution_runs (reconcile_after)
    WHERE state = 'reconciling';
