-- Prompt-on-launch (Phase 1a): let a launch override the template's variables
-- and host limit, gated by per-template ask_* flags. job_limit is the template's
-- default --limit pattern ("limit" is a reserved word, so it is named job_limit).
ALTER TABLE job_templates
    ADD COLUMN IF NOT EXISTS job_limit TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ask_variables_on_launch BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS ask_limit_on_launch BOOLEAN NOT NULL DEFAULT false;
