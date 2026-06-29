-- Add runner host tracking columns to hosts table
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS is_runner_host BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS runner_last_seen TIMESTAMPTZ;
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS runner_healthy BOOLEAN NOT NULL DEFAULT FALSE;

-- Add index for quickly finding runner hosts
CREATE INDEX IF NOT EXISTS idx_hosts_runner ON hosts (inventory_id, is_runner_host) WHERE is_runner_host = TRUE;
