ALTER TABLE unified_jobs DROP CONSTRAINT IF EXISTS fk_uj_ujt;

-- We need to revert the IDs in unified_jobs to point back to job_templates.id
-- But since we don't have a 1:1 mapping easily accessible after dropping tables, 
-- we will just attempt to join backwards before dropping.

DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN SELECT id, unified_job_template_id FROM job_templates LOOP
        -- Restore unified_jobs to point to job_templates.id (which is r.id)
        -- where they currently point to unified_job_templates.id (which is r.unified_job_template_id)
        UPDATE unified_jobs SET unified_job_template_id = r.id WHERE unified_job_template_id = r.unified_job_template_id;
    END LOOP;
END $$;

ALTER TABLE job_templates DROP CONSTRAINT IF EXISTS fk_jt_ujt;
ALTER TABLE job_templates DROP COLUMN IF EXISTS unified_job_template_id;

DROP TABLE IF EXISTS unified_job_templates;
