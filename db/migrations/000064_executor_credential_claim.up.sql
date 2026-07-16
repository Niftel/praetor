-- Persist the immutable dispatch and secrets-service snapshot required for an
-- executor to claim a run using its certificate-derived workload identity.
-- Existing runs remain nullable; the scheduler supplies every field for new
-- credential-backed dispatches.

ALTER TABLE execution_runs
    ADD COLUMN IF NOT EXISTS dispatch_id UUID,
    ADD COLUMN IF NOT EXISTS secrets_credential_id UUID,
    ADD COLUMN IF NOT EXISTS secrets_credential_version BIGINT,
    ADD COLUMN IF NOT EXISTS executor_identity TEXT,
    ADD COLUMN IF NOT EXISTS credential_binding_created_at TIMESTAMPTZ;

ALTER TABLE execution_runs
    ADD CONSTRAINT execution_runs_secrets_credential_pair CHECK (
        (secrets_credential_id IS NULL AND secrets_credential_version IS NULL)
        OR
        (secrets_credential_id IS NOT NULL AND secrets_credential_version > 0)
    ),
    ADD CONSTRAINT execution_runs_executor_claim_pair CHECK (
        (executor_identity IS NULL AND credential_binding_created_at IS NULL)
        OR
        (executor_identity IS NOT NULL AND executor_identity <> '' AND credential_binding_created_at IS NOT NULL)
    );

CREATE UNIQUE INDEX IF NOT EXISTS idx_execution_runs_dispatch_id
    ON execution_runs (dispatch_id)
    WHERE dispatch_id IS NOT NULL;

CREATE FUNCTION prevent_execution_run_claim_reassignment() RETURNS trigger
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

CREATE TRIGGER execution_runs_claim_immutable
BEFORE UPDATE ON execution_runs
FOR EACH ROW EXECUTE FUNCTION prevent_execution_run_claim_reassignment();

COMMENT ON COLUMN execution_runs.dispatch_id IS
    'Scheduler-generated idempotency and stale-delivery fence for one execution dispatch.';
COMMENT ON COLUMN execution_runs.executor_identity IS
    'Exact certificate-derived executor workload identity authorized to resolve this run credential.';
