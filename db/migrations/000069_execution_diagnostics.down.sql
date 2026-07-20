DROP INDEX IF EXISTS idx_job_events_run_host_seq;
DROP INDEX IF EXISTS idx_job_events_run_type_seq;
DROP INDEX IF EXISTS idx_job_events_run_outcome_seq;
ALTER TABLE job_events DROP COLUMN IF EXISTS diagnostic_outcome;
DROP INDEX IF EXISTS idx_unified_jobs_source_job_id;
ALTER TABLE unified_jobs DROP COLUMN IF EXISTS source_job_id;
