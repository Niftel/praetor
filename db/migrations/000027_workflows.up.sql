-- Workflows (Phase 4): a DAG of nodes (job templates or approval gates) connected
-- by success/failure/always edges. A workflow run (workflow_jobs) is orchestrated
-- by the scheduler's workflow runner; job nodes launch ordinary unified_jobs.

-- Definition
CREATE TABLE IF NOT EXISTS workflow_templates (
    id              BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workflow_nodes (
    id                   BIGSERIAL PRIMARY KEY,
    workflow_template_id BIGINT NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
    node_key             TEXT NOT NULL,                 -- author key, referenced by edges
    node_type            TEXT NOT NULL DEFAULT 'job',   -- job | approval
    job_template_id      BIGINT REFERENCES job_templates(id) ON DELETE SET NULL,
    name                 TEXT NOT NULL DEFAULT '',
    UNIQUE (workflow_template_id, node_key)
);

CREATE TABLE IF NOT EXISTS workflow_node_edges (
    workflow_template_id BIGINT NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
    parent_key           TEXT NOT NULL,
    child_key            TEXT NOT NULL,
    edge_type            TEXT NOT NULL DEFAULT 'success' -- success | failure | always
);

-- Runtime
CREATE TABLE IF NOT EXISTS workflow_jobs (
    id                   BIGSERIAL PRIMARY KEY,
    workflow_template_id BIGINT NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
    status               TEXT NOT NULL DEFAULT 'running',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at          TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS workflow_job_nodes (
    id              BIGSERIAL PRIMARY KEY,
    workflow_job_id BIGINT NOT NULL REFERENCES workflow_jobs(id) ON DELETE CASCADE,
    node_key        TEXT NOT NULL,
    node_type       TEXT NOT NULL,
    job_template_id BIGINT,
    unified_job_id  BIGINT,
    -- pending | running | successful | failed | skipped | awaiting_approval | approved | rejected
    status          TEXT NOT NULL DEFAULT 'pending'
);
