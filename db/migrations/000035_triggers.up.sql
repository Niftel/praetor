-- Triggers: launch a target (a workflow OR a job template) on a schedule, or when
-- an internal Praetor event fires. Inbound webhooks are a third trigger kind, but
-- those already live as columns on workflow_templates/job_templates.

-- (Time) A schedule can now target a workflow instead of a job template.
ALTER TABLE schedules ALTER COLUMN unified_job_template_id DROP NOT NULL;
ALTER TABLE schedules
    ADD COLUMN IF NOT EXISTS workflow_template_id BIGINT REFERENCES workflow_templates(id) ON DELETE CASCADE;

-- (Event) Rules that launch a target when a job reaches a terminal state.
--   event_type:  job_succeeded | job_failed | job_finished
--   source_ujt_id: only fire for jobs of this unified job template (NULL = any)
--   target: exactly one of workflow_template_id / unified_job_template_id
CREATE TABLE IF NOT EXISTS event_triggers (
    id                      BIGSERIAL PRIMARY KEY,
    organization_id         BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name                    TEXT NOT NULL,
    enabled                 BOOLEAN NOT NULL DEFAULT true,
    event_type              TEXT NOT NULL,
    source_ujt_id           BIGINT REFERENCES unified_job_templates(id) ON DELETE CASCADE,
    workflow_template_id    BIGINT REFERENCES workflow_templates(id) ON DELETE CASCADE,
    unified_job_template_id BIGINT REFERENCES unified_job_templates(id) ON DELETE CASCADE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Idempotency: a trigger fires at most once per source job.
CREATE TABLE IF NOT EXISTS event_trigger_fires (
    trigger_id    BIGINT NOT NULL REFERENCES event_triggers(id) ON DELETE CASCADE,
    source_job_id BIGINT NOT NULL,
    fired_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (trigger_id, source_job_id)
);
