-- Rollback ldap_metadata column
DROP INDEX IF EXISTS idx_users_ldap_metadata;
ALTER TABLE users DROP COLUMN IF EXISTS ldap_metadata;
