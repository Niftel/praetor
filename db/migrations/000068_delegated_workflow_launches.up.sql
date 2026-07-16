ALTER TABLE workflow_jobs
    ADD COLUMN launched_by_service_principal_id BIGINT REFERENCES service_principals(id) ON DELETE SET NULL,
    ADD COLUMN launched_by_service_credential_id BIGINT REFERENCES service_credentials(id) ON DELETE SET NULL,
    ADD COLUMN delegated_launch_grant_id BIGINT REFERENCES delegated_launch_grants(id) ON DELETE SET NULL,
    ADD COLUMN delegated_external_requester TEXT,
    ADD COLUMN delegated_inventory_id BIGINT REFERENCES inventories(id) ON DELETE SET NULL,
    ADD COLUMN delegated_host_ids BIGINT[] NOT NULL DEFAULT '{}';

ALTER TABLE workflow_jobs
    ADD CONSTRAINT workflow_jobs_single_authenticated_launcher
    CHECK (launched_by_user_id IS NULL OR launched_by_service_principal_id IS NULL);

CREATE INDEX idx_workflow_jobs_service_principal
    ON workflow_jobs (launched_by_service_principal_id, created_at DESC)
    WHERE launched_by_service_principal_id IS NOT NULL;

ALTER TABLE activity_stream
    ADD COLUMN service_principal_id BIGINT REFERENCES service_principals(id) ON DELETE SET NULL,
    ADD COLUMN service_credential_id BIGINT REFERENCES service_credentials(id) ON DELETE SET NULL,
    ADD COLUMN delegated_launch_grant_id BIGINT REFERENCES delegated_launch_grants(id) ON DELETE SET NULL,
    ADD COLUMN external_requester TEXT;

CREATE INDEX idx_activity_stream_service_principal
    ON activity_stream (service_principal_id, created_at DESC)
    WHERE service_principal_id IS NOT NULL;

CREATE TABLE delegated_launch_idempotency (
    service_principal_id BIGINT NOT NULL REFERENCES service_principals(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    workflow_job_id BIGINT REFERENCES workflow_jobs(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service_principal_id, idempotency_key),
    CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    CHECK (length(request_hash) = 64)
);
