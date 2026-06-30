-- Surveys (Phase 1b): a typed, ordered set of launch questions stored as JSON on
-- the template (AWX-compatible spec shape). Answers become extra_vars at launch.
ALTER TABLE job_templates
    ADD COLUMN IF NOT EXISTS survey_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS survey_spec JSONB NOT NULL DEFAULT '{}'::jsonb;
