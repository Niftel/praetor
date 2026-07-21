ALTER TABLE schedules DROP CONSTRAINT IF EXISTS schedules_exactly_one_target;
DELETE FROM schedules WHERE inventory_source_id IS NOT NULL;
ALTER TABLE schedules
    DROP COLUMN IF EXISTS actor_user_id,
    DROP COLUMN IF EXISTS inventory_source_id;
DROP TRIGGER IF EXISTS trg_cancel_inventory_sync_history ON unified_jobs;
DROP FUNCTION IF EXISTS cancel_inventory_sync_history();
ALTER TABLE inventory_sync_history DROP CONSTRAINT IF EXISTS inventory_sync_history_status_check;
UPDATE inventory_sync_history SET status='failed' WHERE status='canceled';
ALTER TABLE inventory_sync_history ADD CONSTRAINT inventory_sync_history_status_check
    CHECK (status IN ('pending', 'running', 'successful', 'failed'));
