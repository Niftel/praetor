-- ===========================================================================
-- LDAP Sync Items - Add Raw Attributes
-- ===========================================================================
-- Stores the raw LDAP attributes for each synced item so they can be 
-- displayed when clicking on a sync history row.
-- ===========================================================================

-- Add ldap_attributes column to store raw LDAP data as JSON
ALTER TABLE ldap_sync_items ADD COLUMN IF NOT EXISTS ldap_attributes JSONB;
