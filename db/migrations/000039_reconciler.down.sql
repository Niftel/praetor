DROP INDEX IF EXISTS idx_execution_runs_reconcile;

ALTER TABLE execution_runs
    DROP COLUMN IF EXISTS runner_host_id,
    DROP COLUMN IF EXISTS reconcile_attempts,
    DROP COLUMN IF EXISTS reconcile_after;
