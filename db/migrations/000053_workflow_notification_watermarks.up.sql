-- Once-only watermarks so the scheduler fires the workflow 'started' event and
-- approval-outcome events (approved | denied) exactly once, even though it
-- re-advances a running workflow every tick. started_notified is claimed on the
-- workflow run; outcome_notified is claimed per approval node (its outcome is set
-- by the API approve/deny endpoints; the scheduler observes it and notifies).
ALTER TABLE workflow_jobs      ADD COLUMN IF NOT EXISTS started_notified BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE workflow_job_nodes ADD COLUMN IF NOT EXISTS outcome_notified BOOLEAN NOT NULL DEFAULT false;
