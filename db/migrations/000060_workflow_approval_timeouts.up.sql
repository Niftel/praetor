-- Optional per-approval timeout policy. Values are snapshotted onto run nodes so
-- editing a workflow never changes an approval already in flight.

ALTER TABLE workflow_nodes
    ADD COLUMN IF NOT EXISTS approval_timeout_seconds INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS approval_timeout_action  TEXT NOT NULL DEFAULT 'rejected';

ALTER TABLE workflow_job_nodes
    ADD COLUMN IF NOT EXISTS approval_timeout_seconds INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS approval_timeout_action  TEXT NOT NULL DEFAULT 'rejected',
    ADD COLUMN IF NOT EXISTS timed_out                BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE workflow_nodes
    ADD CONSTRAINT workflow_nodes_approval_timeout_nonnegative
        CHECK (approval_timeout_seconds >= 0),
    ADD CONSTRAINT workflow_nodes_approval_timeout_action
        CHECK (approval_timeout_action IN ('approved', 'rejected'));

ALTER TABLE workflow_job_nodes
    ADD CONSTRAINT workflow_job_nodes_approval_timeout_nonnegative
        CHECK (approval_timeout_seconds >= 0),
    ADD CONSTRAINT workflow_job_nodes_approval_timeout_action
        CHECK (approval_timeout_action IN ('approved', 'rejected'));

CREATE INDEX IF NOT EXISTS idx_workflow_job_nodes_approval_deadline
    ON workflow_job_nodes (awaiting_since, id)
    WHERE status = 'awaiting_approval' AND approval_timeout_seconds > 0;
