DROP TABLE IF EXISTS event_trigger_fires;
DROP TABLE IF EXISTS event_triggers;
ALTER TABLE schedules DROP COLUMN IF EXISTS workflow_template_id;
-- (unified_job_template_id is left nullable; restoring NOT NULL could fail on data)
