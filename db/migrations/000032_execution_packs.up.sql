-- Execution Packs: named, self-contained Python+Ansible runtimes Praetor pushes
-- onto hosts. A pack is built from a YAML spec (see `make execpack`); this table
-- registers it so job templates can select which pack to run in. The pack's
-- built artifact (build/runtime/<name>-linux-<arch>.tar.gz) is what the executor
-- pushes.
CREATE TABLE IF NOT EXISTS execution_packs (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    spec        TEXT,          -- the YAML spec, for reference / rebuild
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A template may pin an Execution Pack; NULL means the default pack.
ALTER TABLE job_templates
    ADD COLUMN IF NOT EXISTS execution_pack_id BIGINT REFERENCES execution_packs(id) ON DELETE SET NULL;

INSERT INTO execution_packs (name, description) VALUES
    ('ansible-runtime', 'Default Execution Pack: full Ansible (core + community collections).')
ON CONFLICT (name) DO NOTHING;
