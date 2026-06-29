-- ===========================================================================
-- LDAP Sync Support
-- ===========================================================================
-- Adds columns to track LDAP source for users, organizations, and teams,
-- plus a sync log table for monitoring sync operations.
-- ===========================================================================

-- Track LDAP source for users
ALTER TABLE users ADD COLUMN IF NOT EXISTS ldap_dn TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS ldap_synced_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_users_ldap_dn ON users(ldap_dn) WHERE ldap_dn IS NOT NULL;

-- Track LDAP source for organizations
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS ldap_dn TEXT;
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS ldap_synced_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_organizations_ldap_dn ON organizations(ldap_dn) WHERE ldap_dn IS NOT NULL;

-- Track LDAP source for teams
ALTER TABLE teams ADD COLUMN IF NOT EXISTS ldap_dn TEXT;
ALTER TABLE teams ADD COLUMN IF NOT EXISTS ldap_synced_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_teams_ldap_dn ON teams(ldap_dn) WHERE ldap_dn IS NOT NULL;

-- Sync history/status log
CREATE TABLE IF NOT EXISTS ldap_sync_log (
    id BIGSERIAL PRIMARY KEY,
    sync_type TEXT NOT NULL,              -- 'users', 'organizations', 'teams', 'full'
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'running', -- 'running', 'success', 'partial', 'failed'
    items_processed INT DEFAULT 0,
    items_created INT DEFAULT 0,
    items_updated INT DEFAULT 0,
    items_failed INT DEFAULT 0,
    error_message TEXT
);

CREATE INDEX IF NOT EXISTS idx_ldap_sync_log_type_started ON ldap_sync_log(sync_type, started_at DESC);
