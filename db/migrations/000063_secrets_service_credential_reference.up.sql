-- Map Praetor's stable RBAC-facing integer credential id to the opaque UUID and
-- optimistic-concurrency version owned by the standalone Secrets Service.
--
-- Existing credentials remain NULL during the controlled migration. New
-- service-backed credentials must set both values together; a partial mapping
-- could make the scheduler bind a run to an unintended credential version.
ALTER TABLE credentials
    ADD COLUMN IF NOT EXISTS secrets_service_id UUID,
    ADD COLUMN IF NOT EXISTS secrets_service_version BIGINT;

ALTER TABLE credentials
    DROP CONSTRAINT IF EXISTS credentials_secrets_service_reference_complete;

ALTER TABLE credentials
    ADD CONSTRAINT credentials_secrets_service_reference_complete CHECK (
        (secrets_service_id IS NULL AND secrets_service_version IS NULL)
        OR
        (secrets_service_id IS NOT NULL AND secrets_service_version > 0)
    );

CREATE UNIQUE INDEX IF NOT EXISTS idx_credentials_secrets_service_id
    ON credentials (secrets_service_id)
    WHERE secrets_service_id IS NOT NULL;

COMMENT ON COLUMN credentials.secrets_service_id IS
    'Opaque credential UUID owned by the Praetor Secrets Service; NULL only for unmigrated legacy credentials.';

COMMENT ON COLUMN credentials.secrets_service_version IS
    'Latest redacted metadata version observed from the Praetor Secrets Service; plaintext and ciphertext never belong in this table.';
