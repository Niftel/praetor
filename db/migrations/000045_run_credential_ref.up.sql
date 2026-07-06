-- Snapshot the Machine credential id onto the run at dispatch. The scheduler used
-- to decrypt the credential and bake the plaintext SSH key/env into the manifest
-- (persisted in execution_outbox and shipped over NATS). Instead it now records
-- only the credential id here; the executor resolves the injectors at dispatch via
-- an authenticated, run-scoped ingestion endpoint. Keying resolution off this
-- snapshot means a caller can only ever obtain the credential the scheduler
-- actually selected for that run — never an arbitrary one.
ALTER TABLE execution_runs ADD COLUMN IF NOT EXISTS credential_id BIGINT;
