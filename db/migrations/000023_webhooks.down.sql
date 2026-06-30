ALTER TABLE job_templates
    DROP COLUMN IF EXISTS webhook_enabled,
    DROP COLUMN IF EXISTS webhook_service,
    DROP COLUMN IF EXISTS webhook_key;
