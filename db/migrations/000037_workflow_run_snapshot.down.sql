DROP TABLE IF EXISTS workflow_job_edges;
ALTER TABLE workflow_job_nodes
    DROP COLUMN IF EXISTS name,
    DROP COLUMN IF EXISTS webhook_url,
    DROP COLUMN IF EXISTS webhook_body;
