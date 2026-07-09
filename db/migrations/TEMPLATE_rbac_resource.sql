-- TEMPLATE — NOT a migration. The migrator only applies files ending in
-- `.up.sql`, so this file is never run. Copy it to a numbered
-- `NNNNNN_<resource>_rbac.up.sql`, replace the placeholders, and delete this
-- header. It captures the per-object-role pattern (000011 job_template,
-- 000049 workflow_template) so making a resource first-class RBAC is a
-- fill-in-the-blanks change instead of a hand-clone (B8/#87).
--
-- Placeholders:
--   {{RESOURCE}}     content_type value, e.g. execution_pack   (snake_case)
--   {{TABLE}}        backing table,      e.g. execution_packs
--   {{ORG_ADMIN}}    org role that parents this resource's admin_role, e.g.
--                    project_admin_role / job_template_admin_role / credential_admin_role
--
-- Checklist to make {{RESOURCE}} a first-class RBAC object:
--   1. pkg/rbac/types.go      — add ContentType{{Resource}} = "{{RESOURCE}}".
--   2. pkg/rbac/access.go      — add {{RESOURCE}}: "{{TABLE}}" to contentTables.
--   3. this migration          — creates admin/execute/read roles per row + the
--                                org-hierarchy edges, AND extends delete_object_roles.
--   4. handlers                — gate the resource's routes (authorize / readableIDs /
--                                grantCreatorAdmin), like projects/templates/workflows.
-- The delete arm + backfill below are REQUIRED, or deletes orphan roles and
-- pre-existing rows have none.
-- ---------------------------------------------------------------------------

-- Per-row role creation: admin -> org {{ORG_ADMIN}}; execute -> admin + org
-- execute_role; read -> execute + org auditor_role.
CREATE OR REPLACE FUNCTION create_{{RESOURCE}}_roles() RETURNS TRIGGER AS $$
DECLARE
    admin_role_id   BIGINT;
    execute_role_id BIGINT;
    read_role_id    BIGINT;
    org_admin_id    BIGINT;
    org_execute_id  BIGINT;
    org_auditor_id  BIGINT;
BEGIN
    SELECT id INTO org_admin_id   FROM roles WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = '{{ORG_ADMIN}}';
    SELECT id INTO org_execute_id FROM roles WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'execute_role';
    SELECT id INTO org_auditor_id FROM roles WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'auditor_role';

    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('admin_role',   '{{RESOURCE}}', NEW.id, NEW.name || ' Admin',   'Can manage all aspects of the {{RESOURCE}}')
    RETURNING id INTO admin_role_id;
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('execute_role', '{{RESOURCE}}', NEW.id, NEW.name || ' Execute', 'Can use the {{RESOURCE}}')
    RETURNING id INTO execute_role_id;
    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('read_role',    '{{RESOURCE}}', NEW.id, NEW.name || ' Read',    'Can view the {{RESOURCE}}')
    RETURNING id INTO read_role_id;

    INSERT INTO role_parents (role_id, parent_role_id) VALUES (admin_role_id, org_admin_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (execute_role_id, admin_role_id);
    IF org_execute_id IS NOT NULL THEN
        INSERT INTO role_parents (role_id, parent_role_id) VALUES (execute_role_id, org_execute_id);
    END IF;
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, execute_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, org_auditor_id);

    PERFORM compute_role_ancestors(admin_role_id);
    PERFORM compute_role_ancestors(execute_role_id);
    PERFORM compute_role_ancestors(read_role_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_create_{{RESOURCE}}_roles ON {{TABLE}};
CREATE TRIGGER trg_create_{{RESOURCE}}_roles
    AFTER INSERT ON {{TABLE}}
    FOR EACH ROW EXECUTE FUNCTION create_{{RESOURCE}}_roles();

-- REQUIRED: extend the shared delete-cleanup (000029) with a {{RESOURCE}} arm by
-- editing delete_object_roles() to also match content_type = '{{RESOURCE}}'
-- (restate the existing arms verbatim; CREATE OR REPLACE the whole function — see
-- 000049 for the exact shape). Omitting this orphans a deleted row's roles.

-- REQUIRED backfill: existing rows predate the trigger, so create their roles now.
DO $$
DECLARE r RECORD;
BEGIN
    FOR r IN SELECT id, name, organization_id FROM {{TABLE}} LOOP
        -- Re-run the trigger body for each existing row (simplest: temporarily
        -- UPDATE to fire an AFTER trigger, or PERFORM a helper). See 000049's
        -- backfill block for a copy-paste example.
        NULL;
    END LOOP;
END $$;
