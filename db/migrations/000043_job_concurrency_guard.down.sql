DROP INDEX IF EXISTS uq_unified_jobs_active_concurrency;
DROP TRIGGER IF EXISTS trg_set_job_concurrency_key ON unified_jobs;
DROP FUNCTION IF EXISTS set_job_concurrency_key();
ALTER TABLE unified_jobs DROP COLUMN IF EXISTS concurrency_key;
