DROP INDEX IF EXISTS idx_execution_runs_pending_binding_cancellation;

ALTER TABLE execution_runs
    DROP CONSTRAINT IF EXISTS execution_runs_binding_cancellation_pair,
    DROP COLUMN IF EXISTS credential_binding_cancel_reason,
    DROP COLUMN IF EXISTS credential_binding_canceled_at;

CREATE OR REPLACE FUNCTION prevent_execution_run_claim_reassignment() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (OLD.dispatch_id IS NOT NULL AND NEW.dispatch_id IS DISTINCT FROM OLD.dispatch_id)
       OR (OLD.secrets_credential_id IS NOT NULL AND NEW.secrets_credential_id IS DISTINCT FROM OLD.secrets_credential_id)
       OR (OLD.secrets_credential_version IS NOT NULL AND NEW.secrets_credential_version IS DISTINCT FROM OLD.secrets_credential_version)
       OR (OLD.executor_identity IS NOT NULL AND NEW.executor_identity IS DISTINCT FROM OLD.executor_identity)
       OR (OLD.credential_binding_created_at IS NOT NULL AND NEW.credential_binding_created_at IS DISTINCT FROM OLD.credential_binding_created_at) THEN
        RAISE EXCEPTION 'execution run dispatch and credential claim fields are immutable' USING ERRCODE = '23000';
    END IF;
    RETURN NEW;
END;
$$;
