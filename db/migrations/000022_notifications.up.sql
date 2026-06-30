-- Notifications (Phase 2a): org-scoped notification targets, attachable to job
-- templates per lifecycle event. Secrets in config are stored encrypted.
CREATE TABLE IF NOT EXISTS notification_templates (
    id                BIGSERIAL PRIMARY KEY,
    organization_id   BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    notification_type TEXT NOT NULL,                       -- webhook | slack
    config            JSONB NOT NULL DEFAULT '{}'::jsonb,  -- e.g. {"url": "<encrypted>"}
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Which notification fires for which job-template lifecycle event.
CREATE TABLE IF NOT EXISTS job_template_notifications (
    job_template_id          BIGINT NOT NULL REFERENCES job_templates(id) ON DELETE CASCADE,
    notification_template_id BIGINT NOT NULL REFERENCES notification_templates(id) ON DELETE CASCADE,
    event                    TEXT NOT NULL,                -- started | success | error
    PRIMARY KEY (job_template_id, notification_template_id, event)
);
