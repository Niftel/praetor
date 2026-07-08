-- Recreate the legacy LDAP-sync schema dropped by the up migration (columns +
-- indexes from 000012, and the two sync-log tables from 000012–000014).

ALTER TABLE organizations ADD COLUMN IF NOT EXISTS ldap_dn TEXT;
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS ldap_synced_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_organizations_ldap_dn ON organizations(ldap_dn) WHERE ldap_dn IS NOT NULL;

ALTER TABLE teams ADD COLUMN IF NOT EXISTS ldap_dn TEXT;
ALTER TABLE teams ADD COLUMN IF NOT EXISTS ldap_synced_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_teams_ldap_dn ON teams(ldap_dn) WHERE ldap_dn IS NOT NULL;

CREATE TABLE IF NOT EXISTS ldap_sync_log (
    id BIGSERIAL PRIMARY KEY,
    sync_type TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'running',
    items_processed INT DEFAULT 0,
    items_created INT DEFAULT 0,
    items_updated INT DEFAULT 0,
    items_failed INT DEFAULT 0,
    error_message TEXT
);
CREATE INDEX IF NOT EXISTS idx_ldap_sync_log_type_started ON ldap_sync_log(sync_type, started_at DESC);

CREATE TABLE IF NOT EXISTS ldap_sync_items (
    id BIGSERIAL PRIMARY KEY,
    sync_log_id BIGINT NOT NULL REFERENCES ldap_sync_log(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL,
    entity_name TEXT NOT NULL,
    entity_id BIGINT,
    ldap_dn TEXT NOT NULL,
    action TEXT NOT NULL,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ldap_attributes JSONB
);
CREATE INDEX IF NOT EXISTS idx_ldap_sync_items_log ON ldap_sync_items(sync_log_id);
CREATE INDEX IF NOT EXISTS idx_ldap_sync_items_action ON ldap_sync_items(action);
