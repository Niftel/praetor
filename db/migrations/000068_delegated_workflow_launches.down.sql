DROP TABLE IF EXISTS delegated_launch_idempotency;
DROP INDEX IF EXISTS idx_activity_stream_service_principal;
ALTER TABLE activity_stream
    DROP COLUMN IF EXISTS external_requester,
    DROP COLUMN IF EXISTS delegated_launch_grant_id,
    DROP COLUMN IF EXISTS service_credential_id,
    DROP COLUMN IF EXISTS service_principal_id;
DROP INDEX IF EXISTS idx_workflow_jobs_service_principal;
ALTER TABLE workflow_jobs DROP CONSTRAINT IF EXISTS workflow_jobs_single_authenticated_launcher;
ALTER TABLE workflow_jobs
    DROP COLUMN IF EXISTS delegated_host_ids,
    DROP COLUMN IF EXISTS delegated_inventory_id,
    DROP COLUMN IF EXISTS delegated_external_requester,
    DROP COLUMN IF EXISTS delegated_launch_grant_id,
    DROP COLUMN IF EXISTS launched_by_service_credential_id,
    DROP COLUMN IF EXISTS launched_by_service_principal_id;
