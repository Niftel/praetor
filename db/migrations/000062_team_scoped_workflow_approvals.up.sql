-- Snapshot the team responsible for approvals when a workflow is launched.
-- Authorization uses current membership of this team, while the selected team
-- itself remains stable for the lifetime of the run.
ALTER TABLE workflow_jobs
    ADD COLUMN IF NOT EXISTS approval_team_id BIGINT REFERENCES teams(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_workflow_jobs_approval_team
    ON workflow_jobs (approval_team_id)
    WHERE approval_team_id IS NOT NULL;
