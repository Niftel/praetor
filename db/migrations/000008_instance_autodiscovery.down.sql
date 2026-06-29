-- Revert auto-discovery columns
ALTER TABLE instances DROP COLUMN IF EXISTS instance_type;
ALTER TABLE instances DROP COLUMN IF EXISTS healthy;
ALTER TABLE instances DROP COLUMN IF EXISTS last_heartbeat;
ALTER TABLE instances DROP COLUMN IF EXISTS ip_address;
