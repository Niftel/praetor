DROP INDEX IF EXISTS idx_activity_stream_organization;
ALTER TABLE activity_stream
    DROP COLUMN IF EXISTS failure_code,
    DROP COLUMN IF EXISTS outcome,
    DROP COLUMN IF EXISTS principal_kind,
    DROP COLUMN IF EXISTS organization_id;
