-- ===========================================================================
-- AWX-STYLE ROLE HIERARCHY - Full Implementation
-- ===========================================================================
-- This migration replaces the simplified RBAC with AWX-style role hierarchy:
-- - Polymorphic roles (roles belong to objects via content_type/object_id)
-- - Role parents/ancestors for hierarchy traversal
-- - Implicit role creation via database triggers
-- - System singleton roles (System Administrator, System Auditor)
-- ===========================================================================

-- First, drop existing RBAC tables (they'll be recreated with new schema)
DROP TABLE IF EXISTS role_bindings CASCADE;
DROP TABLE IF EXISTS roles CASCADE;

-- ===========================================================================
-- CORE ROLE TABLES
-- ===========================================================================

-- Roles table with polymorphic ownership
CREATE TABLE IF NOT EXISTS roles (
    id BIGSERIAL PRIMARY KEY,
    
    -- Role identity
    role_field TEXT NOT NULL,              -- e.g., 'admin_role', 'member_role', 'read_role'
    singleton_name TEXT UNIQUE,            -- For system-wide roles: 'system_administrator', 'system_auditor'
    
    -- What object does this role belong to (polymorphic)
    content_type TEXT,                     -- 'organization', 'team', 'project', 'inventory', etc.
    object_id BIGINT,                      -- ID of the owning object
    
    -- Metadata
    name TEXT,                             -- Human-readable computed name
    description TEXT,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    -- Unique per object+role_field OR singleton
    UNIQUE NULLS NOT DISTINCT (content_type, object_id, role_field)
);

CREATE INDEX IF NOT EXISTS idx_roles_content_type_object ON roles(content_type, object_id);
CREATE INDEX IF NOT EXISTS idx_roles_singleton ON roles(singleton_name) WHERE singleton_name IS NOT NULL;

-- Role parent-child relationships (defines hierarchy)
CREATE TABLE IF NOT EXISTS role_parents (
    id BIGSERIAL PRIMARY KEY,
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    parent_role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (role_id, parent_role_id)
);

CREATE INDEX IF NOT EXISTS idx_role_parents_role ON role_parents(role_id);
CREATE INDEX IF NOT EXISTS idx_role_parents_parent ON role_parents(parent_role_id);

-- Computed ancestors for efficient permission lookups
-- This is maintained by triggers/functions for fast queries
CREATE TABLE IF NOT EXISTS role_ancestors (
    id BIGSERIAL PRIMARY KEY,
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    ancestor_role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    UNIQUE (role_id, ancestor_role_id)
);

CREATE INDEX IF NOT EXISTS idx_role_ancestors_role ON role_ancestors(role_id);
CREATE INDEX IF NOT EXISTS idx_role_ancestors_ancestor ON role_ancestors(ancestor_role_id);

-- Role membership (users directly in roles)
CREATE TABLE IF NOT EXISTS role_members (
    id BIGSERIAL PRIMARY KEY,
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (role_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_role_members_role ON role_members(role_id);
CREATE INDEX IF NOT EXISTS idx_role_members_user ON role_members(user_id);

-- Team role assignments (teams can be granted roles on resources)
CREATE TABLE IF NOT EXISTS team_roles (
    id BIGSERIAL PRIMARY KEY,
    team_id BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, role_id)
);

CREATE INDEX IF NOT EXISTS idx_team_roles_team ON team_roles(team_id);
CREATE INDEX IF NOT EXISTS idx_team_roles_role ON team_roles(role_id);

-- ===========================================================================
-- USER ENHANCEMENTS
-- ===========================================================================

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_system_auditor BOOLEAN NOT NULL DEFAULT FALSE;

-- ===========================================================================
-- SEED SYSTEM SINGLETON ROLES
-- ===========================================================================

INSERT INTO roles (role_field, singleton_name, name, description)
VALUES 
    ('system_administrator', 'system_administrator', 'System Administrator', 'Full system access - can do anything'),
    ('system_auditor', 'system_auditor', 'System Auditor', 'Read-only access to entire system')
ON CONFLICT (singleton_name) DO NOTHING;

-- ===========================================================================
-- HELPER FUNCTION: Compute ancestors for a role
-- ===========================================================================

CREATE OR REPLACE FUNCTION compute_role_ancestors(target_role_id BIGINT) RETURNS void AS $$
BEGIN
    -- Delete existing ancestors for this role
    DELETE FROM role_ancestors WHERE role_id = target_role_id;
    
    -- Insert all ancestors using recursive CTE
    INSERT INTO role_ancestors (role_id, ancestor_role_id)
    WITH RECURSIVE ancestors AS (
        -- Base case: direct parents
        SELECT role_id, parent_role_id as ancestor_id
        FROM role_parents
        WHERE role_id = target_role_id
        
        UNION
        
        -- Recursive case: parents of parents
        SELECT a.role_id, rp.parent_role_id
        FROM ancestors a
        JOIN role_parents rp ON a.ancestor_id = rp.role_id
    )
    SELECT DISTINCT target_role_id, ancestor_id FROM ancestors;
END;
$$ LANGUAGE plpgsql;

-- ===========================================================================
-- TRIGGER: Create implicit roles when Organization is created
-- ===========================================================================

CREATE OR REPLACE FUNCTION create_organization_roles() RETURNS TRIGGER AS $$
DECLARE
    admin_role_id BIGINT;
    member_role_id BIGINT;
    auditor_role_id BIGINT;
    execute_role_id BIGINT;
    read_role_id BIGINT;
    project_admin_role_id BIGINT;
    inventory_admin_role_id BIGINT;
    credential_admin_role_id BIGINT;
    workflow_admin_role_id BIGINT;
    notification_admin_role_id BIGINT;
    job_template_admin_role_id BIGINT;
    approval_role_id BIGINT;
    sys_admin_id BIGINT;
    sys_auditor_id BIGINT;
BEGIN
    -- Get system singleton roles
    SELECT id INTO sys_admin_id FROM roles WHERE singleton_name = 'system_administrator';
    SELECT id INTO sys_auditor_id FROM roles WHERE singleton_name = 'system_auditor';
    
    -- Create admin_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('admin_role', 'organization', NEW.id, NEW.name || ' Admin', 'Can manage all aspects of the organization')
    RETURNING id INTO admin_role_id;
    
    -- Create execute_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('execute_role', 'organization', NEW.id, NEW.name || ' Execute', 'May run any executable resources in the organization')
    RETURNING id INTO execute_role_id;
    
    -- Create project_admin_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('project_admin_role', 'organization', NEW.id, NEW.name || ' Project Admin', 'Can manage all projects of the organization')
    RETURNING id INTO project_admin_role_id;
    
    -- Create inventory_admin_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('inventory_admin_role', 'organization', NEW.id, NEW.name || ' Inventory Admin', 'Can manage all inventories of the organization')
    RETURNING id INTO inventory_admin_role_id;
    
    -- Create credential_admin_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('credential_admin_role', 'organization', NEW.id, NEW.name || ' Credential Admin', 'Can manage all credentials of the organization')
    RETURNING id INTO credential_admin_role_id;
    
    -- Create workflow_admin_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('workflow_admin_role', 'organization', NEW.id, NEW.name || ' Workflow Admin', 'Can manage all workflows of the organization')
    RETURNING id INTO workflow_admin_role_id;
    
    -- Create notification_admin_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('notification_admin_role', 'organization', NEW.id, NEW.name || ' Notification Admin', 'Can manage all notifications of the organization')
    RETURNING id INTO notification_admin_role_id;
    
    -- Create job_template_admin_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('job_template_admin_role', 'organization', NEW.id, NEW.name || ' Job Template Admin', 'Can manage all job templates of the organization')
    RETURNING id INTO job_template_admin_role_id;
    
    -- Create auditor_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('auditor_role', 'organization', NEW.id, NEW.name || ' Auditor', 'Can view all aspects of the organization')
    RETURNING id INTO auditor_role_id;
    
    -- Create member_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('member_role', 'organization', NEW.id, NEW.name || ' Member', 'User is a member of the organization')
    RETURNING id INTO member_role_id;
    
    -- Create read_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('read_role', 'organization', NEW.id, NEW.name || ' Read', 'May view settings for the organization')
    RETURNING id INTO read_role_id;
    
    -- Create approval_role
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('approval_role', 'organization', NEW.id, NEW.name || ' Approval', 'Can approve or deny workflow approval nodes')
    RETURNING id INTO approval_role_id;
    
    -- ===========================================================================
    -- Set up role hierarchy (parent -> child means child inherits from parent)
    -- ===========================================================================
    
    -- System Admin is parent of Org Admin
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (admin_role_id, sys_admin_id);
    
    -- Admin is parent of all *_admin roles, execute, approval, and member
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (execute_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (project_admin_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (inventory_admin_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (credential_admin_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (workflow_admin_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (notification_admin_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (job_template_admin_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (member_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (approval_role_id, admin_role_id);
    
    -- System Auditor is parent of Org Auditor
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (auditor_role_id, sys_auditor_id);
    
    -- Read role parents: member, auditor, execute, and all admin roles
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, member_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, auditor_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, execute_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, project_admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, inventory_admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, credential_admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, workflow_admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, notification_admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, job_template_admin_role_id);
    
    -- Compute ancestors for all created roles
    PERFORM compute_role_ancestors(admin_role_id);
    PERFORM compute_role_ancestors(execute_role_id);
    PERFORM compute_role_ancestors(project_admin_role_id);
    PERFORM compute_role_ancestors(inventory_admin_role_id);
    PERFORM compute_role_ancestors(credential_admin_role_id);
    PERFORM compute_role_ancestors(workflow_admin_role_id);
    PERFORM compute_role_ancestors(notification_admin_role_id);
    PERFORM compute_role_ancestors(job_template_admin_role_id);
    PERFORM compute_role_ancestors(auditor_role_id);
    PERFORM compute_role_ancestors(member_role_id);
    PERFORM compute_role_ancestors(read_role_id);
    PERFORM compute_role_ancestors(approval_role_id);
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_create_org_roles ON organizations;
CREATE TRIGGER trg_create_org_roles
    AFTER INSERT ON organizations
    FOR EACH ROW EXECUTE FUNCTION create_organization_roles();

-- ===========================================================================
-- TRIGGER: Create implicit roles when Team is created
-- ===========================================================================

CREATE OR REPLACE FUNCTION create_team_roles() RETURNS TRIGGER AS $$
DECLARE
    admin_role_id BIGINT;
    member_role_id BIGINT;
    read_role_id BIGINT;
    org_admin_id BIGINT;
    org_auditor_id BIGINT;
BEGIN
    -- Get parent org roles
    SELECT id INTO org_admin_id FROM roles 
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'admin_role';
    SELECT id INTO org_auditor_id FROM roles 
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'auditor_role';
    
    -- Create team roles
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('admin_role', 'team', NEW.id, NEW.name || ' Admin', 'Can manage all aspects of the team')
    RETURNING id INTO admin_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('member_role', 'team', NEW.id, NEW.name || ' Member', 'User is a member of the team')
    RETURNING id INTO member_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('read_role', 'team', NEW.id, NEW.name || ' Read', 'May view settings for the team')
    RETURNING id INTO read_role_id;
    
    -- Hierarchy: Org Admin -> Team Admin -> Team Member -> Team Read
    -- Also: Org Auditor -> Team Read
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (admin_role_id, org_admin_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (member_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, member_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, org_auditor_id);
    
    -- Compute ancestors
    PERFORM compute_role_ancestors(admin_role_id);
    PERFORM compute_role_ancestors(member_role_id);
    PERFORM compute_role_ancestors(read_role_id);
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_create_team_roles ON teams;
CREATE TRIGGER trg_create_team_roles
    AFTER INSERT ON teams
    FOR EACH ROW EXECUTE FUNCTION create_team_roles();

-- ===========================================================================
-- TRIGGER: Create implicit roles when Project is created
-- ===========================================================================

CREATE OR REPLACE FUNCTION create_project_roles() RETURNS TRIGGER AS $$
DECLARE
    admin_role_id BIGINT;
    use_role_id BIGINT;
    update_role_id BIGINT;
    read_role_id BIGINT;
    org_project_admin_id BIGINT;
    org_auditor_id BIGINT;
BEGIN
    -- Get parent org roles
    SELECT id INTO org_project_admin_id FROM roles 
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'project_admin_role';
    SELECT id INTO org_auditor_id FROM roles 
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'auditor_role';
    
    -- Create project roles
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('admin_role', 'project', NEW.id, NEW.name || ' Admin', 'Can manage all aspects of the project')
    RETURNING id INTO admin_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('use_role', 'project', NEW.id, NEW.name || ' Use', 'Can use project in a job template')
    RETURNING id INTO use_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('update_role', 'project', NEW.id, NEW.name || ' Update', 'Can update the project from SCM')
    RETURNING id INTO update_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('read_role', 'project', NEW.id, NEW.name || ' Read', 'Can view the project')
    RETURNING id INTO read_role_id;
    
    -- Hierarchy
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (admin_role_id, org_project_admin_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (use_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (update_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, use_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, update_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, org_auditor_id);
    
    PERFORM compute_role_ancestors(admin_role_id);
    PERFORM compute_role_ancestors(use_role_id);
    PERFORM compute_role_ancestors(update_role_id);
    PERFORM compute_role_ancestors(read_role_id);
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_create_project_roles ON projects;
CREATE TRIGGER trg_create_project_roles
    AFTER INSERT ON projects
    FOR EACH ROW EXECUTE FUNCTION create_project_roles();

-- ===========================================================================
-- TRIGGER: Create implicit roles when Inventory is created
-- ===========================================================================

CREATE OR REPLACE FUNCTION create_inventory_roles() RETURNS TRIGGER AS $$
DECLARE
    admin_role_id BIGINT;
    use_role_id BIGINT;
    adhoc_role_id BIGINT;
    update_role_id BIGINT;
    read_role_id BIGINT;
    org_inventory_admin_id BIGINT;
    org_auditor_id BIGINT;
BEGIN
    -- Get parent org roles
    SELECT id INTO org_inventory_admin_id FROM roles 
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'inventory_admin_role';
    SELECT id INTO org_auditor_id FROM roles 
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'auditor_role';
    
    -- Create inventory roles
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('admin_role', 'inventory', NEW.id, NEW.name || ' Admin', 'Can manage all aspects of the inventory')
    RETURNING id INTO admin_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('use_role', 'inventory', NEW.id, NEW.name || ' Use', 'Can use inventory in a job template')
    RETURNING id INTO use_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('adhoc_role', 'inventory', NEW.id, NEW.name || ' Ad Hoc', 'Can run ad hoc commands on this inventory')
    RETURNING id INTO adhoc_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('update_role', 'inventory', NEW.id, NEW.name || ' Update', 'Can update the inventory sources')
    RETURNING id INTO update_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('read_role', 'inventory', NEW.id, NEW.name || ' Read', 'Can view the inventory')
    RETURNING id INTO read_role_id;
    
    -- Hierarchy
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (admin_role_id, org_inventory_admin_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (use_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (adhoc_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (update_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, use_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, adhoc_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, update_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, org_auditor_id);
    
    PERFORM compute_role_ancestors(admin_role_id);
    PERFORM compute_role_ancestors(use_role_id);
    PERFORM compute_role_ancestors(adhoc_role_id);
    PERFORM compute_role_ancestors(update_role_id);
    PERFORM compute_role_ancestors(read_role_id);
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_create_inventory_roles ON inventories;
CREATE TRIGGER trg_create_inventory_roles
    AFTER INSERT ON inventories
    FOR EACH ROW EXECUTE FUNCTION create_inventory_roles();

-- ===========================================================================
-- TRIGGER: Create implicit roles when Job Template is created
-- ===========================================================================

CREATE OR REPLACE FUNCTION create_job_template_roles() RETURNS TRIGGER AS $$
DECLARE
    admin_role_id BIGINT;
    execute_role_id BIGINT;
    read_role_id BIGINT;
    org_jt_admin_id BIGINT;
    org_auditor_id BIGINT;
BEGIN
    -- Get parent org roles
    SELECT id INTO org_jt_admin_id FROM roles 
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'job_template_admin_role';
    SELECT id INTO org_auditor_id FROM roles 
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'auditor_role';
    
    -- Create job template roles
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('admin_role', 'job_template', NEW.id, NEW.name || ' Admin', 'Can manage all aspects of the job template')
    RETURNING id INTO admin_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('execute_role', 'job_template', NEW.id, NEW.name || ' Execute', 'Can execute the job template')
    RETURNING id INTO execute_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('read_role', 'job_template', NEW.id, NEW.name || ' Read', 'Can view the job template')
    RETURNING id INTO read_role_id;
    
    -- Hierarchy
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (admin_role_id, org_jt_admin_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (execute_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, execute_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, org_auditor_id);
    
    PERFORM compute_role_ancestors(admin_role_id);
    PERFORM compute_role_ancestors(execute_role_id);
    PERFORM compute_role_ancestors(read_role_id);
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_create_job_template_roles ON job_templates;
CREATE TRIGGER trg_create_job_template_roles
    AFTER INSERT ON job_templates
    FOR EACH ROW EXECUTE FUNCTION create_job_template_roles();

-- ===========================================================================
-- TRIGGER: Create implicit roles when Credential is created
-- ===========================================================================

CREATE OR REPLACE FUNCTION create_credential_roles() RETURNS TRIGGER AS $$
DECLARE
    admin_role_id BIGINT;
    use_role_id BIGINT;
    read_role_id BIGINT;
    org_cred_admin_id BIGINT;
    org_auditor_id BIGINT;
BEGIN
    -- Get parent org roles
    SELECT id INTO org_cred_admin_id FROM roles 
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'credential_admin_role';
    SELECT id INTO org_auditor_id FROM roles 
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'auditor_role';
    
    -- Create credential roles
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('admin_role', 'credential', NEW.id, NEW.name || ' Admin', 'Can manage all aspects of the credential')
    RETURNING id INTO admin_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('use_role', 'credential', NEW.id, NEW.name || ' Use', 'Can use credential in a job template')
    RETURNING id INTO use_role_id;
    
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('read_role', 'credential', NEW.id, NEW.name || ' Read', 'Can view the credential (not secrets)')
    RETURNING id INTO read_role_id;
    
    -- Hierarchy
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (admin_role_id, org_cred_admin_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (use_role_id, admin_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, use_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, org_auditor_id);
    
    PERFORM compute_role_ancestors(admin_role_id);
    PERFORM compute_role_ancestors(use_role_id);
    PERFORM compute_role_ancestors(read_role_id);
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_create_credential_roles ON credentials;
CREATE TRIGGER trg_create_credential_roles
    AFTER INSERT ON credentials
    FOR EACH ROW EXECUTE FUNCTION create_credential_roles();

-- ===========================================================================
-- BOOTSTRAP: Create roles for existing organizations
-- ===========================================================================
-- Run this to create roles for any organizations that existed before this migration
-- We inline the role creation logic instead of calling the trigger function

DO $$
DECLARE
    org RECORD;
    admin_role_id BIGINT;
    member_role_id BIGINT;
    auditor_role_id BIGINT;
    execute_role_id BIGINT;
    read_role_id BIGINT;
    project_admin_role_id BIGINT;
    inventory_admin_role_id BIGINT;
    credential_admin_role_id BIGINT;
    workflow_admin_role_id BIGINT;
    notification_admin_role_id BIGINT;
    job_template_admin_role_id BIGINT;
    approval_role_id BIGINT;
    sys_admin_id BIGINT;
    sys_auditor_id BIGINT;
BEGIN
    -- Get system singleton roles
    SELECT id INTO sys_admin_id FROM roles WHERE singleton_name = 'system_administrator';
    SELECT id INTO sys_auditor_id FROM roles WHERE singleton_name = 'system_auditor';
    
    FOR org IN SELECT * FROM organizations LOOP
        -- Check if roles already exist for this org
        IF NOT EXISTS (SELECT 1 FROM roles WHERE content_type = 'organization' AND object_id = org.id) THEN
            -- Create admin_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('admin_role', 'organization', org.id, org.name || ' Admin', 'Can manage all aspects of the organization')
            RETURNING id INTO admin_role_id;
            
            -- Create execute_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('execute_role', 'organization', org.id, org.name || ' Execute', 'May run any executable resources in the organization')
            RETURNING id INTO execute_role_id;
            
            -- Create project_admin_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('project_admin_role', 'organization', org.id, org.name || ' Project Admin', 'Can manage all projects of the organization')
            RETURNING id INTO project_admin_role_id;
            
            -- Create inventory_admin_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('inventory_admin_role', 'organization', org.id, org.name || ' Inventory Admin', 'Can manage all inventories of the organization')
            RETURNING id INTO inventory_admin_role_id;
            
            -- Create credential_admin_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('credential_admin_role', 'organization', org.id, org.name || ' Credential Admin', 'Can manage all credentials of the organization')
            RETURNING id INTO credential_admin_role_id;
            
            -- Create workflow_admin_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('workflow_admin_role', 'organization', org.id, org.name || ' Workflow Admin', 'Can manage all workflows of the organization')
            RETURNING id INTO workflow_admin_role_id;
            
            -- Create notification_admin_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('notification_admin_role', 'organization', org.id, org.name || ' Notification Admin', 'Can manage all notifications of the organization')
            RETURNING id INTO notification_admin_role_id;
            
            -- Create job_template_admin_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('job_template_admin_role', 'organization', org.id, org.name || ' Job Template Admin', 'Can manage all job templates of the organization')
            RETURNING id INTO job_template_admin_role_id;
            
            -- Create auditor_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('auditor_role', 'organization', org.id, org.name || ' Auditor', 'Can view all aspects of the organization')
            RETURNING id INTO auditor_role_id;
            
            -- Create member_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('member_role', 'organization', org.id, org.name || ' Member', 'User is a member of the organization')
            RETURNING id INTO member_role_id;
            
            -- Create read_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('read_role', 'organization', org.id, org.name || ' Read', 'May view settings for the organization')
            RETURNING id INTO read_role_id;
            
            -- Create approval_role
            INSERT INTO roles (role_field, content_type, object_id, name, description)
            VALUES ('approval_role', 'organization', org.id, org.name || ' Approval', 'Can approve or deny workflow approval nodes')
            RETURNING id INTO approval_role_id;
            
            -- Set up role hierarchy
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (admin_role_id, sys_admin_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (execute_role_id, admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (project_admin_role_id, admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (inventory_admin_role_id, admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (credential_admin_role_id, admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (workflow_admin_role_id, admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (notification_admin_role_id, admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (job_template_admin_role_id, admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (member_role_id, admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (approval_role_id, admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (auditor_role_id, sys_auditor_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, member_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, auditor_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, execute_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, project_admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, inventory_admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, credential_admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, workflow_admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, notification_admin_role_id) ON CONFLICT DO NOTHING;
            INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, job_template_admin_role_id) ON CONFLICT DO NOTHING;
            
            -- Compute ancestors for all created roles
            PERFORM compute_role_ancestors(admin_role_id);
            PERFORM compute_role_ancestors(execute_role_id);
            PERFORM compute_role_ancestors(project_admin_role_id);
            PERFORM compute_role_ancestors(inventory_admin_role_id);
            PERFORM compute_role_ancestors(credential_admin_role_id);
            PERFORM compute_role_ancestors(workflow_admin_role_id);
            PERFORM compute_role_ancestors(notification_admin_role_id);
            PERFORM compute_role_ancestors(job_template_admin_role_id);
            PERFORM compute_role_ancestors(auditor_role_id);
            PERFORM compute_role_ancestors(member_role_id);
            PERFORM compute_role_ancestors(read_role_id);
            PERFORM compute_role_ancestors(approval_role_id);
        END IF;
    END LOOP;
END $$;

