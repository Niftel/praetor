-- Provisional-cold reconcile tier.
--
-- "Never give up" means a run parked in 'reconciling' whose host is permanently
-- gone (decommissioned/unreachable) would otherwise sit in the 30s hot sweep
-- forever, diluting throughput for genuinely recoverable runs. This column lets
-- the reconciler DEMOTE such a run to a cheap hourly cadence after many
-- consecutive failed probes, without ever declaring it falsely 'lost' — 'lost'
-- still requires positive proof (the job directory missing on a REACHABLE host,
-- see services/reconciler/core/reconciler.go markLost). Any evidence of life
-- (a successful probe, or a late host-runner push landing at ingestion) promotes
-- the run back to 'hot'.
--
-- A tier COLUMN, deliberately not a new 'state' value: state='reconciling' is
-- load-bearing in queries that must keep matching cold runs too (the scheduler's
-- parking guards and local-run true-loss query, run_is_terminal). A new state
-- would silently exempt cold runs from all of them; a column is invisible to
-- every existing query. The existing partial index
-- idx_execution_runs_reconcile (reconcile_after WHERE state='reconciling') still
-- serves the candidate scan — the reconciling set is tiny.
--
-- DEFAULT 'hot' makes every in-flight 'reconciling' row backward-compatible with
-- zero backfill.
ALTER TABLE execution_runs
    ADD COLUMN IF NOT EXISTS reconcile_tier TEXT NOT NULL DEFAULT 'hot'
        CHECK (reconcile_tier IN ('hot', 'cold'));
