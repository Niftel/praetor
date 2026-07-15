DROP INDEX IF EXISTS idx_workflow_jobs_approval_team;
ALTER TABLE workflow_jobs DROP COLUMN IF EXISTS approval_team_id;
