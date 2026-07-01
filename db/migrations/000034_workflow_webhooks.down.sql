ALTER TABLE workflow_job_nodes DROP COLUMN IF EXISTS event_token;
ALTER TABLE workflow_nodes DROP COLUMN IF EXISTS webhook_url, DROP COLUMN IF EXISTS webhook_body;
ALTER TABLE workflow_templates
    DROP COLUMN IF EXISTS webhook_enabled,
    DROP COLUMN IF EXISTS webhook_service,
    DROP COLUMN IF EXISTS webhook_key;
