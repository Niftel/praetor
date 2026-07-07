-- Restore the attempt_number column (constant 1, matching the original schema).
ALTER TABLE execution_runs ADD COLUMN IF NOT EXISTS attempt_number INT NOT NULL DEFAULT 1;
