-- Durable inventory-source synchronization history. The launch trigger is the
-- single creation point, so manual, scheduled, and update-on-launch syncs cannot
-- bypass audit history. Preview operations are intentionally non-mutating and
-- therefore do not create sync history.

ALTER TABLE inventory_sources
    ADD COLUMN IF NOT EXISTS reconciliation_policy TEXT NOT NULL DEFAULT 'disable_missing'
        CHECK (reconciliation_policy IN ('disable_missing', 'retain_missing'));

-- Ownership lets reconciliation affect only records managed by this source.
-- Manual hosts and records owned by another dynamic source are never disabled
-- merely because they are absent from this source's result.
ALTER TABLE hosts
    ADD COLUMN IF NOT EXISTS inventory_source_id BIGINT
        REFERENCES inventory_sources(id) ON DELETE SET NULL;
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS inventory_source_id BIGINT
        REFERENCES inventory_sources(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_hosts_inventory_source ON hosts (inventory_source_id);
CREATE INDEX IF NOT EXISTS idx_groups_inventory_source ON groups (inventory_source_id);

CREATE TABLE IF NOT EXISTS inventory_sync_history (
    id                    BIGSERIAL PRIMARY KEY,
    correlation_id        UUID NOT NULL DEFAULT uuid_generate_v4() UNIQUE,
    inventory_id          BIGINT REFERENCES inventories(id) ON DELETE SET NULL,
    inventory_source_id   BIGINT REFERENCES inventory_sources(id) ON DELETE SET NULL,
    unified_job_id        BIGINT UNIQUE REFERENCES unified_jobs(id) ON DELETE SET NULL,
    execution_run_id      UUID REFERENCES execution_runs(id) ON DELETE SET NULL,
    credential_id         BIGINT REFERENCES credentials(id) ON DELETE SET NULL,
    reconciliation_policy TEXT NOT NULL CHECK (reconciliation_policy IN ('disable_missing', 'retain_missing')),
    phase                 TEXT NOT NULL DEFAULT 'queued'
        CHECK (phase IN ('queued', 'acquisition', 'parsing', 'validation', 'reconciliation', 'completed')),
    status                TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'successful', 'failed')),
    hosts_added           INT NOT NULL DEFAULT 0 CHECK (hosts_added >= 0),
    hosts_updated         INT NOT NULL DEFAULT 0 CHECK (hosts_updated >= 0),
    hosts_disabled        INT NOT NULL DEFAULT 0 CHECK (hosts_disabled >= 0),
    hosts_unchanged       INT NOT NULL DEFAULT 0 CHECK (hosts_unchanged >= 0),
    groups_added          INT NOT NULL DEFAULT 0 CHECK (groups_added >= 0),
    groups_updated        INT NOT NULL DEFAULT 0 CHECK (groups_updated >= 0),
    groups_unchanged      INT NOT NULL DEFAULT 0 CHECK (groups_unchanged >= 0),
    diagnostic_code       TEXT,
    diagnostic_message    TEXT,
    diagnostic_details    JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at            TIMESTAMPTZ,
    finished_at           TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_inventory_sync_history_source_created
    ON inventory_sync_history (inventory_source_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_sync_history_inventory_created
    ON inventory_sync_history (inventory_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_sync_history_status_created
    ON inventory_sync_history (status, created_at DESC);

CREATE OR REPLACE FUNCTION create_inventory_sync_history() RETURNS trigger AS $$
DECLARE
    source_id BIGINT;
BEGIN
    IF NOT (NEW.job_args ? 'inventory_source_id')
       OR COALESCE((NEW.job_args->>'inventory_preview')::BOOLEAN, false) THEN
        RETURN NEW;
    END IF;

    source_id := (NEW.job_args->>'inventory_source_id')::BIGINT;
    INSERT INTO inventory_sync_history (
        inventory_id, inventory_source_id, unified_job_id, credential_id,
        reconciliation_policy
    )
    SELECT inventory_id, id, NEW.id, credential_id, reconciliation_policy
      FROM inventory_sources
     WHERE id = source_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_create_inventory_sync_history ON unified_jobs;
CREATE TRIGGER trg_create_inventory_sync_history
    AFTER INSERT ON unified_jobs
    FOR EACH ROW EXECUTE FUNCTION create_inventory_sync_history();

CREATE OR REPLACE FUNCTION start_inventory_sync_history() RETURNS trigger AS $$
BEGIN
    UPDATE inventory_sync_history
       SET execution_run_id = NEW.id,
           phase = 'acquisition',
           status = 'running',
           started_at = COALESCE(started_at, now()),
           modified_at = now()
     WHERE unified_job_id = NEW.unified_job_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_start_inventory_sync_history ON execution_runs;
CREATE TRIGGER trg_start_inventory_sync_history
    AFTER INSERT ON execution_runs
    FOR EACH ROW EXECUTE FUNCTION start_inventory_sync_history();
