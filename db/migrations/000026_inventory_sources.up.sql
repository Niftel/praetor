-- Dynamic inventory sources (Phase 3a): a source describes how to discover hosts
-- (a static/plugin inventory file, or an executable script). Syncing runs
-- `ansible-inventory --list` against it and upserts the result into the inventory.
CREATE TABLE IF NOT EXISTS inventory_sources (
    id              BIGSERIAL PRIMARY KEY,
    inventory_id    BIGINT NOT NULL REFERENCES inventories(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    source_kind     TEXT NOT NULL DEFAULT 'inventory',  -- inventory | script
    source          TEXT NOT NULL DEFAULT '',           -- file content (plugin yaml/ini) or script
    credential_id   BIGINT REFERENCES credentials(id) ON DELETE SET NULL,
    update_on_launch BOOLEAN NOT NULL DEFAULT false,
    last_synced_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
