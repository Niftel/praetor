-- Mark which credential types are system-managed (the built-ins the migrator
-- seeds) vs. user-created. Managed types cannot be edited or deleted via the API;
-- user types can. Existing rows default to false; the migrator re-seeds the
-- built-ins with managed=true.
ALTER TABLE credential_types ADD COLUMN IF NOT EXISTS managed BOOLEAN NOT NULL DEFAULT false;
