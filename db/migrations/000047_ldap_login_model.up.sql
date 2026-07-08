-- ===========================================================================
-- LDAP login-time group→role model (drops the legacy OU-discovery sync)
-- ===========================================================================
-- LDAP is now evaluated at login (see pkg/auth/LDAP-REDESIGN.md): users/orgs/
-- teams are mapped from group membership, not mirrored by a background sync. This
-- removes the sync-log tables and the org/team "LDAP-sourced" markers. The users
-- LDAP columns (ldap_dn, ldap_synced_at, ldap_metadata) are KEPT — the mapper
-- still stamps them.
-- ===========================================================================

DROP TABLE IF EXISTS ldap_sync_items;
DROP TABLE IF EXISTS ldap_sync_log;

-- Organizations and teams are operator-named mapping targets, not LDAP objects.
DROP INDEX IF EXISTS idx_organizations_ldap_dn;
ALTER TABLE organizations DROP COLUMN IF EXISTS ldap_dn;
ALTER TABLE organizations DROP COLUMN IF EXISTS ldap_synced_at;

DROP INDEX IF EXISTS idx_teams_ldap_dn;
ALTER TABLE teams DROP COLUMN IF EXISTS ldap_dn;
ALTER TABLE teams DROP COLUMN IF EXISTS ldap_synced_at;
