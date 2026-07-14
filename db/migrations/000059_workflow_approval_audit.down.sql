DROP INDEX IF EXISTS idx_workflow_job_nodes_pending_approvals;
DROP TRIGGER IF EXISTS trg_workflow_approval_transition ON workflow_job_nodes;
DROP FUNCTION IF EXISTS stamp_workflow_approval_transition();

ALTER TABLE workflow_job_nodes
    DROP COLUMN IF EXISTS decided_by_user_id,
    DROP COLUMN IF EXISTS decided_at,
    DROP COLUMN IF EXISTS awaiting_since;

ALTER TABLE workflow_jobs DROP COLUMN IF EXISTS launched_by_user_id;
