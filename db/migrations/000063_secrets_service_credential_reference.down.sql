DROP INDEX IF EXISTS idx_credentials_secrets_service_id;

ALTER TABLE credentials
    DROP CONSTRAINT IF EXISTS credentials_secrets_service_reference_complete,
    DROP COLUMN IF EXISTS secrets_service_version,
    DROP COLUMN IF EXISTS secrets_service_id;
