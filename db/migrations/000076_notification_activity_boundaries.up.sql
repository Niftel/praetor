-- Make notification administration and test-delivery boundaries independently
-- auditable without retaining request bodies or secret-bearing target config.
ALTER TABLE activity_stream
    ADD COLUMN organization_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
    ADD COLUMN principal_kind TEXT NOT NULL DEFAULT 'human'
        CHECK (principal_kind IN ('human', 'service')),
    ADD COLUMN outcome TEXT NOT NULL DEFAULT 'success'
        CHECK (outcome IN ('success', 'denied', 'failed')),
    ADD COLUMN failure_code TEXT
        CHECK (failure_code IS NULL OR length(failure_code) BETWEEN 1 AND 64);

UPDATE activity_stream
   SET principal_kind='service'
 WHERE service_principal_id IS NOT NULL;

CREATE INDEX idx_activity_stream_organization
    ON activity_stream (organization_id, created_at DESC)
    WHERE organization_id IS NOT NULL;
