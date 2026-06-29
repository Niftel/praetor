-- ===========================================================================
-- LDAP Sync Items Detail
-- ===========================================================================
-- Stores individual items that were synced during each LDAP sync operation.
-- This allows viewing details when clicking on a sync history row.
-- ===========================================================================

-- Sync item details (linked to sync_log)
CREATE TABLE IF NOT EXISTS ldap_sync_items (
    id BIGSERIAL PRIMARY KEY,
    sync_log_id BIGINT NOT NULL REFERENCES ldap_sync_log(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL,              -- 'user', 'organization', 'team'
    entity_name TEXT NOT NULL,              -- username, org name, or team name
    entity_id BIGINT,                       -- ID of the entity after sync
    ldap_dn TEXT NOT NULL,                  -- LDAP Distinguished Name
    action TEXT NOT NULL,                   -- 'created', 'updated', 'unchanged', 'failed'
    error_message TEXT,                     -- error details if action='failed'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ldap_sync_items_log ON ldap_sync_items(sync_log_id);
CREATE INDEX IF NOT EXISTS idx_ldap_sync_items_action ON ldap_sync_items(action);
