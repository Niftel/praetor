-- Git-backed Execution Packs: a pack can source its YAML spec from a repo, and a
-- push webhook re-pulls + rebuilds it. When scm_url is set, the packbuilder clones
-- the repo, reads spec_path, updates the stored spec, then builds — so pushing the
-- YAML to the branch rebuilds the pack with the pushed content.
ALTER TABLE execution_packs
    ADD COLUMN IF NOT EXISTS scm_url     TEXT,
    ADD COLUMN IF NOT EXISTS scm_branch  TEXT,
    ADD COLUMN IF NOT EXISTS spec_path   TEXT,
    ADD COLUMN IF NOT EXISTS webhook_key TEXT;
