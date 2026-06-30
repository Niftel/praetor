-- Best-effort restore of the removed tables/columns as empty shells (the data is
-- not recoverable; these features were unused).
CREATE TABLE IF NOT EXISTS instances (
    id BIGSERIAL PRIMARY KEY,
    hostname TEXT NOT NULL UNIQUE,
    capacity INTEGER DEFAULT 100,
    enabled BOOLEAN DEFAULT true,
    instance_type TEXT NOT NULL DEFAULT 'executor',
    last_heartbeat TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS instance_groups (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS execution_environments (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    image TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE job_templates  ADD COLUMN IF NOT EXISTS execution_environment_id BIGINT REFERENCES execution_environments(id);
ALTER TABLE execution_runs ADD COLUMN IF NOT EXISTS executor_instance_id BIGINT REFERENCES instances(id);
