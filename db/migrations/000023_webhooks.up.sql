-- Inbound webhooks (Phase 2b): a template can be triggered by a GitHub/GitLab/
-- generic webhook. webhook_key is the shared secret used to verify each call.
ALTER TABLE job_templates
    ADD COLUMN IF NOT EXISTS webhook_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS webhook_service TEXT NOT NULL DEFAULT '',   -- github | gitlab | generic
    ADD COLUMN IF NOT EXISTS webhook_key TEXT NOT NULL DEFAULT '';
