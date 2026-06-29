-- Rollback ldap_attributes column
ALTER TABLE ldap_sync_items DROP COLUMN IF EXISTS ldap_attributes;
