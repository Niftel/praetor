-- Remove LDAP sync support

DROP TABLE IF EXISTS ldap_sync_log;

ALTER TABLE teams DROP COLUMN IF EXISTS ldap_synced_at;
ALTER TABLE teams DROP COLUMN IF EXISTS ldap_dn;

ALTER TABLE organizations DROP COLUMN IF EXISTS ldap_synced_at;
ALTER TABLE organizations DROP COLUMN IF EXISTS ldap_dn;

ALTER TABLE users DROP COLUMN IF EXISTS ldap_synced_at;
ALTER TABLE users DROP COLUMN IF EXISTS ldap_dn;
