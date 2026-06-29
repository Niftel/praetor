-- Rollback AWX-style RBAC migration

-- Drop triggers
DROP TRIGGER IF EXISTS trg_create_credential_roles ON credentials;
DROP TRIGGER IF EXISTS trg_create_job_template_roles ON job_templates;
DROP TRIGGER IF EXISTS trg_create_inventory_roles ON inventories;
DROP TRIGGER IF EXISTS trg_create_project_roles ON projects;
DROP TRIGGER IF EXISTS trg_create_team_roles ON teams;
DROP TRIGGER IF EXISTS trg_create_org_roles ON organizations;

-- Drop functions
DROP FUNCTION IF EXISTS create_credential_roles();
DROP FUNCTION IF EXISTS create_job_template_roles();
DROP FUNCTION IF EXISTS create_inventory_roles();
DROP FUNCTION IF EXISTS create_project_roles();
DROP FUNCTION IF EXISTS create_team_roles();
DROP FUNCTION IF EXISTS create_organization_roles();
DROP FUNCTION IF EXISTS compute_role_ancestors(BIGINT);

-- Drop RBAC tables
DROP TABLE IF EXISTS team_roles CASCADE;
DROP TABLE IF EXISTS role_members CASCADE;
DROP TABLE IF EXISTS role_ancestors CASCADE;
DROP TABLE IF EXISTS role_parents CASCADE;
DROP TABLE IF EXISTS roles CASCADE;

-- Remove user field
ALTER TABLE users DROP COLUMN IF EXISTS is_system_auditor;

-- Recreate original simple tables (from 20240503000000_rbac_tables.up.sql)
CREATE TABLE IF NOT EXISTS roles (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    permissions JSONB NOT NULL DEFAULT '[]'::jsonb
);

CREATE TABLE IF NOT EXISTS role_bindings (
    id BIGSERIAL PRIMARY KEY,
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    team_id BIGINT REFERENCES teams(id) ON DELETE CASCADE,
    organization_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_subject CHECK (
        (user_id IS NOT NULL AND team_id IS NULL) OR 
        (user_id IS NULL AND team_id IS NOT NULL)
    ),
    UNIQUE (role_id, user_id, team_id, organization_id)
);

-- Re-seed default roles
INSERT INTO roles (name, description, permissions) VALUES
('System Administrator', 'Full access to the entire system', '["*"]'::jsonb),
('System Auditor', 'Read-only access to the entire system', '["read:system"]'::jsonb),
('Organization Administrator', 'Full access to specific organization', '["*"]'::jsonb),
('Organization Member', 'Standard access to organization resources', '["read:org","execute:job"]'::jsonb),
('Organization Auditor', 'Read-only access to organization resources', '["read:org"]'::jsonb)
ON CONFLICT (name) DO NOTHING;
