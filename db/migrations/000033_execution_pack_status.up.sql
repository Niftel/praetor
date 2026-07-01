-- Build status for Execution Packs. A pack created with a spec is queued
-- ('pending'); the packbuilder service builds it (docker) and marks it 'ready'
-- or 'failed'. Packs registered without a spec (pre-built artifacts) are 'ready'.
ALTER TABLE execution_packs
    ADD COLUMN IF NOT EXISTS status    TEXT NOT NULL DEFAULT 'ready',
    ADD COLUMN IF NOT EXISTS build_log TEXT;
