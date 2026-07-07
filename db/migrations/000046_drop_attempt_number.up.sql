-- Drop the dead attempt_number column: it was always inserted as the constant 1
-- (nothing ever creates attempt 2 — there is no run-retry feature) and is read by
-- no code, so the schema was implying a capability that does not exist (#18).
ALTER TABLE execution_runs DROP COLUMN IF EXISTS attempt_number;
