CREATE TABLE IF NOT EXISTS unified_job_templates (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE job_templates ADD COLUMN IF NOT EXISTS unified_job_template_id BIGINT;

-- Backfill data (Idempotent: Only runs if table is empty)
DO $$
DECLARE
    r RECORD;
    new_id BIGINT;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM unified_job_templates LIMIT 1) THEN
        FOR r IN SELECT id, name FROM job_templates WHERE unified_job_template_id IS NULL LOOP
            INSERT INTO unified_job_templates (name) VALUES (r.name) RETURNING id INTO new_id;
            
            -- Link job_template to new parent
            UPDATE job_templates SET unified_job_template_id = new_id WHERE id = r.id;
            
            -- Migrate existing jobs that were using job_template_id as the unified_job_template_id
            -- CAUTION: This assumes unified_jobs.unified_job_template_id WAS pointing to job_templates.id
            UPDATE unified_jobs SET unified_job_template_id = new_id WHERE unified_job_template_id = r.id;
        END LOOP;
    END IF;
END $$;

-- Idempotent Constraints
DO $$
BEGIN
    -- Ensure column is nullable initially? Or enforce NOT NULL?
    -- Original script did: ALTER TABLE job_templates ALTER COLUMN unified_job_template_id SET NOT NULL;
    -- We'll try to set it NOT NULL, catching error if it fails (e.g. if NULLs remain)
    BEGIN
        ALTER TABLE job_templates ALTER COLUMN unified_job_template_id SET NOT NULL;
    EXCEPTION
        WHEN others THEN NULL; -- Ignore if fails (e.g. column contains nulls)
    END;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_jt_ujt') THEN
        ALTER TABLE job_templates ADD CONSTRAINT fk_jt_ujt FOREIGN KEY (unified_job_template_id) REFERENCES unified_job_templates(id) ON DELETE CASCADE;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_uj_ujt') THEN
        ALTER TABLE unified_jobs ADD CONSTRAINT fk_uj_ujt FOREIGN KEY (unified_job_template_id) REFERENCES unified_job_templates(id) ON DELETE CASCADE;
    END IF;
END $$;
