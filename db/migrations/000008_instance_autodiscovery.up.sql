-- Add auto-discovery columns to instances table
ALTER TABLE instances ADD COLUMN IF NOT EXISTS instance_type TEXT NOT NULL DEFAULT 'executor'; -- executor, controller, hybrid
ALTER TABLE instances ADD COLUMN IF NOT EXISTS healthy BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS last_heartbeat TIMESTAMPTZ;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS ip_address TEXT;
