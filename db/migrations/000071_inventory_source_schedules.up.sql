-- Inventory-source schedules execute under the current authority of their creator.
ALTER TABLE schedules
    ADD COLUMN IF NOT EXISTS inventory_source_id BIGINT
        REFERENCES inventory_sources(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS actor_user_id BIGINT
        REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE schedules DROP CONSTRAINT IF EXISTS schedules_exactly_one_target;
ALTER TABLE schedules ADD CONSTRAINT schedules_exactly_one_target CHECK (
    ((unified_job_template_id IS NOT NULL)::INT +
     (workflow_template_id IS NOT NULL)::INT +
     (inventory_source_id IS NOT NULL)::INT) = 1
);

CREATE INDEX IF NOT EXISTS idx_schedules_inventory_source
    ON schedules (inventory_source_id);

ALTER TABLE inventory_sync_history DROP CONSTRAINT IF EXISTS inventory_sync_history_status_check;
ALTER TABLE inventory_sync_history ADD CONSTRAINT inventory_sync_history_status_check
    CHECK (status IN ('pending', 'running', 'successful', 'failed', 'canceled'));

CREATE OR REPLACE FUNCTION cancel_inventory_sync_history() RETURNS trigger AS $$
BEGIN
    IF NEW.status = 'canceled' AND OLD.status IS DISTINCT FROM NEW.status THEN
        UPDATE inventory_sync_history
           SET status='canceled', phase='completed', finished_at=COALESCE(finished_at, now()),
               modified_at=now(), diagnostic_code='canceled',
               diagnostic_message=COALESCE(diagnostic_message, 'Inventory synchronization canceled')
         WHERE unified_job_id=NEW.id AND status IN ('pending', 'running');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_cancel_inventory_sync_history
    AFTER UPDATE OF status ON unified_jobs
    FOR EACH ROW EXECUTE FUNCTION cancel_inventory_sync_history();
