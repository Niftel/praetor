DROP INDEX IF EXISTS idx_workflow_job_nodes_approval_deadline;

ALTER TABLE workflow_job_nodes
    DROP CONSTRAINT IF EXISTS workflow_job_nodes_approval_timeout_action,
    DROP CONSTRAINT IF EXISTS workflow_job_nodes_approval_timeout_nonnegative,
    DROP COLUMN IF EXISTS timed_out,
    DROP COLUMN IF EXISTS approval_timeout_action,
    DROP COLUMN IF EXISTS approval_timeout_seconds;

ALTER TABLE workflow_nodes
    DROP CONSTRAINT IF EXISTS workflow_nodes_approval_timeout_action,
    DROP CONSTRAINT IF EXISTS workflow_nodes_approval_timeout_nonnegative,
    DROP COLUMN IF EXISTS approval_timeout_action,
    DROP COLUMN IF EXISTS approval_timeout_seconds;
