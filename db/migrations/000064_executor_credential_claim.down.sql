DROP TRIGGER IF EXISTS execution_runs_claim_immutable ON execution_runs;
DROP FUNCTION IF EXISTS prevent_execution_run_claim_reassignment();
DROP INDEX IF EXISTS idx_execution_runs_dispatch_id;

ALTER TABLE execution_runs
    DROP CONSTRAINT IF EXISTS execution_runs_executor_claim_pair,
    DROP CONSTRAINT IF EXISTS execution_runs_secrets_credential_pair,
    DROP COLUMN IF EXISTS credential_binding_created_at,
    DROP COLUMN IF EXISTS executor_identity,
    DROP COLUMN IF EXISTS secrets_credential_version,
    DROP COLUMN IF EXISTS secrets_credential_id,
    DROP COLUMN IF EXISTS dispatch_id;
