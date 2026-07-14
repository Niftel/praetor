-- Durable workflow approval audit fields. The status trigger records the moment
-- a node enters/leaves the waiting state regardless of which scheduler replica
-- performs the transition. Manual API launches populate launched_by_user_id;
-- machine-originated launches intentionally leave it NULL.

ALTER TABLE workflow_jobs
    ADD COLUMN IF NOT EXISTS launched_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE workflow_job_nodes
    ADD COLUMN IF NOT EXISTS awaiting_since     TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS decided_at         TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS decided_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

UPDATE workflow_job_nodes n
SET awaiting_since = COALESCE(n.awaiting_since, j.created_at)
FROM workflow_jobs j
WHERE j.id = n.workflow_job_id
  AND n.status = 'awaiting_approval'
  AND n.awaiting_since IS NULL;

CREATE OR REPLACE FUNCTION stamp_workflow_approval_transition()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status = 'awaiting_approval' THEN
            NEW.awaiting_since := COALESCE(NEW.awaiting_since, now());
        ELSIF NEW.status IN ('approved', 'rejected') THEN
            NEW.decided_at := COALESCE(NEW.decided_at, now());
        END IF;
    ELSIF OLD.status IS DISTINCT FROM NEW.status THEN
        IF NEW.status = 'awaiting_approval' THEN
            NEW.awaiting_since := COALESCE(NEW.awaiting_since, now());
            NEW.decided_at := NULL;
            NEW.decided_by_user_id := NULL;
        ELSIF NEW.status IN ('approved', 'rejected') THEN
            NEW.decided_at := COALESCE(NEW.decided_at, now());
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_workflow_approval_transition ON workflow_job_nodes;
CREATE TRIGGER trg_workflow_approval_transition
BEFORE INSERT OR UPDATE OF status ON workflow_job_nodes
FOR EACH ROW EXECUTE FUNCTION stamp_workflow_approval_transition();

CREATE INDEX IF NOT EXISTS idx_workflow_job_nodes_pending_approvals
    ON workflow_job_nodes (awaiting_since, id)
    WHERE status = 'awaiting_approval';
