-- Webhook wiring for workflows:
--  (1) a remote event can LAUNCH a whole workflow (mirror job_templates' webhook),
--  (2) a 'webhook_out' node CALLS OUT to a URL and continues,
--  (3) a 'webhook_in' node WAITS at 'awaiting_event' until an external caller hits
--      its callback with the per-run event_token, then continues on success/failure.

-- (1) Trigger a workflow from a provider webhook (github | gitlab | generic).
ALTER TABLE workflow_templates
    ADD COLUMN IF NOT EXISTS webhook_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS webhook_service TEXT,
    ADD COLUMN IF NOT EXISTS webhook_key     TEXT;

-- (2/3) Node config. node_type now also allows 'webhook_out' and 'webhook_in'.
-- webhook_url/webhook_body drive an outbound call; webhook_in nodes need no config.
ALTER TABLE workflow_nodes
    ADD COLUMN IF NOT EXISTS webhook_url  TEXT,
    ADD COLUMN IF NOT EXISTS webhook_body TEXT;

-- (3) Per-run secret an external system must present to release a waiting node.
ALTER TABLE workflow_job_nodes
    ADD COLUMN IF NOT EXISTS event_token TEXT;
