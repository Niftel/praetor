-- Track durable completion of secrets-service binding cancellation. Terminal
-- state is committed by the consumer; the scheduler retries cancellation until
-- the remote service confirms it, then writes these immutable completion fields.

ALTER TABLE execution_runs
    ADD COLUMN credential_binding_canceled_at TIMESTAMPTZ,
    ADD COLUMN credential_binding_cancel_reason TEXT;

ALTER TABLE execution_runs
    ADD CONSTRAINT execution_runs_binding_cancellation_pair CHECK (
        (credential_binding_canceled_at IS NULL AND credential_binding_cancel_reason IS NULL)
        OR
        (credential_binding_canceled_at IS NOT NULL AND credential_binding_cancel_reason <> '')
    );

CREATE INDEX idx_execution_runs_pending_binding_cancellation
    ON execution_runs (finished_at, id)
    WHERE credential_binding_created_at IS NOT NULL
      AND credential_binding_canceled_at IS NULL;

CREATE OR REPLACE FUNCTION prevent_execution_run_claim_reassignment() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (OLD.dispatch_id IS NOT NULL AND NEW.dispatch_id IS DISTINCT FROM OLD.dispatch_id)
       OR (OLD.secrets_credential_id IS NOT NULL AND NEW.secrets_credential_id IS DISTINCT FROM OLD.secrets_credential_id)
       OR (OLD.secrets_credential_version IS NOT NULL AND NEW.secrets_credential_version IS DISTINCT FROM OLD.secrets_credential_version)
       OR (OLD.executor_identity IS NOT NULL AND NEW.executor_identity IS DISTINCT FROM OLD.executor_identity)
       OR (OLD.credential_binding_created_at IS NOT NULL AND NEW.credential_binding_created_at IS DISTINCT FROM OLD.credential_binding_created_at)
       OR (OLD.credential_binding_canceled_at IS NOT NULL AND NEW.credential_binding_canceled_at IS DISTINCT FROM OLD.credential_binding_canceled_at)
       OR (OLD.credential_binding_cancel_reason IS NOT NULL AND NEW.credential_binding_cancel_reason IS DISTINCT FROM OLD.credential_binding_cancel_reason) THEN
        RAISE EXCEPTION 'execution run dispatch and credential claim fields are immutable' USING ERRCODE = '23000';
    END IF;
    RETURN NEW;
END;
$$;

COMMENT ON COLUMN execution_runs.credential_binding_canceled_at IS
    'Time the secrets service confirmed cancellation of this run credential binding.';
COMMENT ON COLUMN execution_runs.credential_binding_cancel_reason IS
    'Stable terminal-state reason sent to the secrets service during cancellation.';
