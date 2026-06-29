-- Revert runner host columns
DROP INDEX IF EXISTS idx_hosts_runner;
ALTER TABLE hosts DROP COLUMN IF EXISTS runner_healthy;
ALTER TABLE hosts DROP COLUMN IF EXISTS runner_last_seen;
ALTER TABLE hosts DROP COLUMN IF EXISTS is_runner_host;
