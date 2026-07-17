-- Approval gates use one platform policy: deny after 24 hours. Keeping the
-- values on nodes preserves the scheduler's snapshot/deadline mechanism while
-- removing unsafe per-workflow policy choices.

UPDATE workflow_nodes
SET approval_timeout_seconds = 86400,
    approval_timeout_action = 'rejected'
WHERE node_type = 'approval';

UPDATE workflow_nodes
SET approval_timeout_seconds = 0,
    approval_timeout_action = 'rejected'
WHERE node_type <> 'approval';

-- Bring approvals that have not completed yet under the same policy. Historical
-- completed runs retain the policy that actually governed them.
UPDATE workflow_job_nodes
SET approval_timeout_seconds = 86400,
    approval_timeout_action = 'rejected'
WHERE node_type = 'approval'
  AND status IN ('pending', 'running', 'awaiting_approval');

ALTER TABLE workflow_nodes
    ADD CONSTRAINT workflow_nodes_fixed_approval_expiry
    CHECK (
        (node_type = 'approval' AND approval_timeout_seconds = 86400 AND approval_timeout_action = 'rejected')
        OR
        (node_type <> 'approval' AND approval_timeout_seconds = 0 AND approval_timeout_action = 'rejected')
    );
