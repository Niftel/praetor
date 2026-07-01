ALTER TABLE execution_packs
    DROP COLUMN IF EXISTS scm_url,
    DROP COLUMN IF EXISTS scm_branch,
    DROP COLUMN IF EXISTS spec_path,
    DROP COLUMN IF EXISTS webhook_key;
