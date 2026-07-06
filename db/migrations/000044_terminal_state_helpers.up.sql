-- Single source of truth for the "truly terminal" states of a run and a job, so
-- the monotonic-projection guards spread across four writers (consumer projection,
-- scheduler timeout sweep, reconciler finalize, ingestion heartbeat) can't drift
-- apart. These are the states that must never be overwritten. The reconciler-owned
-- provisional states ('lost' for runs, 'error' for jobs) are intentionally NOT
-- terminal — a real recovering terminal event may still replace them — so callers
-- that additionally want to skip those add an explicit `AND <col> <> '<state>'`.

CREATE OR REPLACE FUNCTION run_is_terminal(state text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE AS
$$ SELECT state IN ('successful', 'failed', 'canceled') $$;

CREATE OR REPLACE FUNCTION job_is_terminal(status text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE AS
$$ SELECT status IN ('successful', 'failed', 'canceled') $$;
