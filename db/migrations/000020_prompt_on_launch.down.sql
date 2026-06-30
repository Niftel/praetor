ALTER TABLE job_templates
    DROP COLUMN IF EXISTS job_limit,
    DROP COLUMN IF EXISTS ask_variables_on_launch,
    DROP COLUMN IF EXISTS ask_limit_on_launch;
