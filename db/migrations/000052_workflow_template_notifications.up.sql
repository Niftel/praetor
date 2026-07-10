-- Workflow-level notifications: attach notification templates to a workflow
-- template so a run fires them on terminal state (success | error) and an approval
-- node fires them when it starts waiting (approval). This mirrors the existing
-- job_template_notifications link (000022); the scheduler dispatches these because
-- workflow runs finalize and approval nodes are created there, not on the executor
-- job-event path the consumer projects.
CREATE TABLE IF NOT EXISTS workflow_template_notifications (
    workflow_template_id     BIGINT NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
    notification_template_id BIGINT NOT NULL REFERENCES notification_templates(id) ON DELETE CASCADE,
    event                    TEXT NOT NULL,                 -- success | error | approval
    PRIMARY KEY (workflow_template_id, notification_template_id, event)
);
