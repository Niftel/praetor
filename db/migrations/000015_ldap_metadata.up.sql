-- ===========================================================================
-- LDAP Metadata - Custom Attributes Storage
-- ===========================================================================
-- Stores custom LDAP attributes for users as JSONB
-- ===========================================================================

-- Add ldap_metadata column to store custom LDAP attributes
ALTER TABLE users ADD COLUMN IF NOT EXISTS ldap_metadata JSONB;

-- Create index for querying LDAP metadata
CREATE INDEX IF NOT EXISTS idx_users_ldap_metadata ON users USING gin (ldap_metadata) WHERE ldap_metadata IS NOT NULL;
