-- Prevent accidental concurrent runs of the same template/workflow (AWX-style).
-- Default false: a launch is refused while a prior run of the same
-- template/workflow is still active. Set true to opt a template into concurrency.
ALTER TABLE job_templates
    ADD COLUMN IF NOT EXISTS allow_simultaneous BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE workflow_templates
    ADD COLUMN IF NOT EXISTS allow_simultaneous BOOLEAN NOT NULL DEFAULT false;
