ALTER TABLE job_templates
    DROP COLUMN IF EXISTS survey_enabled,
    DROP COLUMN IF EXISTS survey_spec;
