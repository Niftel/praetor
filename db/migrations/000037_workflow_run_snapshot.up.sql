-- Snapshot a workflow's graph into the run at launch, so editing the template
-- doesn't change in-flight runs. Nodes were already snapshotted (node_key/type/
-- job_template_id); the scheduler still read edges and node name/webhook_* live
-- from the template. Snapshot those too.

-- Per-run edges.
CREATE TABLE IF NOT EXISTS workflow_job_edges (
    workflow_job_id BIGINT NOT NULL REFERENCES workflow_jobs(id) ON DELETE CASCADE,
    parent_key      TEXT NOT NULL,
    child_key       TEXT NOT NULL,
    edge_type       TEXT NOT NULL DEFAULT 'success'
);
CREATE INDEX IF NOT EXISTS idx_workflow_job_edges_job ON workflow_job_edges(workflow_job_id);

-- Per-run node attributes the scheduler reads (name + webhook_out config).
ALTER TABLE workflow_job_nodes
    ADD COLUMN IF NOT EXISTS name         TEXT,
    ADD COLUMN IF NOT EXISTS webhook_url  TEXT,
    ADD COLUMN IF NOT EXISTS webhook_body TEXT;

-- Backfill existing runs so they don't break when reads switch to the snapshot.
UPDATE workflow_job_nodes wjn
SET name = wn.name, webhook_url = wn.webhook_url, webhook_body = wn.webhook_body
FROM workflow_nodes wn
JOIN workflow_jobs wj ON wj.workflow_template_id = wn.workflow_template_id
WHERE wjn.workflow_job_id = wj.id AND wjn.node_key = wn.node_key;

INSERT INTO workflow_job_edges (workflow_job_id, parent_key, child_key, edge_type)
SELECT wj.id, e.parent_key, e.child_key, e.edge_type
FROM workflow_jobs wj
JOIN workflow_node_edges e ON e.workflow_template_id = wj.workflow_template_id;
