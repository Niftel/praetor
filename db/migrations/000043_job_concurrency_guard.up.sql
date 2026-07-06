-- Enforce allow_simultaneous in the database so the count-then-insert race in the
-- API / webhook / event launch paths cannot admit two concurrent active jobs for a
-- template that disallows it. The handlers still do a count check as a friendly
-- fast path; this index is the actual guarantee.
--
-- concurrency_key is set (to the template id) only for jobs whose template
-- disallows simultaneous runs; simultaneous templates and template-less jobs
-- (inventory-sync, ad-hoc) get NULL and are never constrained. A partial unique
-- index over active statuses then permits at most one active job per constrained
-- template. When a job reaches a terminal status it leaves the partial index,
-- freeing the key for the next run.

ALTER TABLE unified_jobs ADD COLUMN IF NOT EXISTS concurrency_key BIGINT;

CREATE OR REPLACE FUNCTION set_job_concurrency_key() RETURNS trigger AS $$
BEGIN
    NEW.concurrency_key := NULL;
    IF NEW.unified_job_template_id IS NOT NULL THEN
        SELECT CASE WHEN jt.allow_simultaneous THEN NULL ELSE NEW.unified_job_template_id END
        INTO NEW.concurrency_key
        FROM job_templates jt
        WHERE jt.unified_job_template_id = NEW.unified_job_template_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_set_job_concurrency_key ON unified_jobs;
CREATE TRIGGER trg_set_job_concurrency_key
    BEFORE INSERT ON unified_jobs
    FOR EACH ROW EXECUTE FUNCTION set_job_concurrency_key();

-- Backfill currently-active jobs so the index reflects live state.
UPDATE unified_jobs uj
SET concurrency_key = uj.unified_job_template_id
FROM job_templates jt
WHERE uj.unified_job_template_id = jt.unified_job_template_id
  AND NOT jt.allow_simultaneous
  AND uj.status NOT IN ('successful', 'failed', 'canceled', 'error');

CREATE UNIQUE INDEX IF NOT EXISTS uq_unified_jobs_active_concurrency
    ON unified_jobs (concurrency_key)
    WHERE concurrency_key IS NOT NULL
      AND status NOT IN ('successful', 'failed', 'canceled', 'error');
