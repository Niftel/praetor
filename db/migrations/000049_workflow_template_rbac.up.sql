-- Make workflow templates first-class RBAC objects (Gitea #60), mirroring the
-- job_template model (000011 + the org-execute edge from 000048). Each workflow
-- template gets admin/execute/approval/read roles:
--   admin    -> org workflow_admin_role
--   execute  -> wf admin_role, org execute_role   (org-execute may run anything)
--   approval -> org approval_role ONLY            (manage != approve, on purpose)
--   read     -> wf execute_role, wf approval_role, org auditor_role
--
-- adhoc_role is deliberately NOT added — there is no ad-hoc feature to gate.

CREATE OR REPLACE FUNCTION create_workflow_template_roles() RETURNS TRIGGER AS $$
DECLARE
    admin_role_id BIGINT;
    execute_role_id BIGINT;
    approval_role_id BIGINT;
    read_role_id BIGINT;
    org_wf_admin_id BIGINT;
    org_execute_id BIGINT;
    org_approval_id BIGINT;
    org_auditor_id BIGINT;
BEGIN
    SELECT id INTO org_wf_admin_id FROM roles
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'workflow_admin_role';
    SELECT id INTO org_execute_id FROM roles
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'execute_role';
    SELECT id INTO org_approval_id FROM roles
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'approval_role';
    SELECT id INTO org_auditor_id FROM roles
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'auditor_role';

    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('admin_role', 'workflow_template', NEW.id, NEW.name || ' Admin', 'Can manage all aspects of the workflow template')
    RETURNING id INTO admin_role_id;

    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('execute_role', 'workflow_template', NEW.id, NEW.name || ' Execute', 'Can launch the workflow template')
    RETURNING id INTO execute_role_id;

    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('approval_role', 'workflow_template', NEW.id, NEW.name || ' Approve', 'Can approve or deny approval nodes of this workflow')
    RETURNING id INTO approval_role_id;

    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('read_role', 'workflow_template', NEW.id, NEW.name || ' Read', 'Can view the workflow template')
    RETURNING id INTO read_role_id;

    -- Hierarchy. approval_role is NOT a child of the workflow admin_role: managing
    -- a workflow and approving its gates are distinct authorities.
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (admin_role_id, org_wf_admin_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (execute_role_id, admin_role_id);
    IF org_execute_id IS NOT NULL THEN
        INSERT INTO role_parents (role_id, parent_role_id) VALUES (execute_role_id, org_execute_id);
    END IF;
    IF org_approval_id IS NOT NULL THEN
        INSERT INTO role_parents (role_id, parent_role_id) VALUES (approval_role_id, org_approval_id);
    END IF;
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, execute_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, approval_role_id);
    INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_role_id, org_auditor_id);

    PERFORM compute_role_ancestors(admin_role_id);
    PERFORM compute_role_ancestors(execute_role_id);
    PERFORM compute_role_ancestors(approval_role_id);
    PERFORM compute_role_ancestors(read_role_id);

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_create_workflow_template_roles ON workflow_templates;
CREATE TRIGGER trg_create_workflow_template_roles
    AFTER INSERT ON workflow_templates
    FOR EACH ROW EXECUTE FUNCTION create_workflow_template_roles();

-- Extend the shared delete-cleanup (000029) with a workflow_template arm, else a
-- deleted workflow orphans its 4 roles. Restate the existing arms verbatim.
CREATE OR REPLACE FUNCTION delete_object_roles() RETURNS TRIGGER AS $$
DECLARE
    ct TEXT;
BEGIN
    ct := CASE TG_TABLE_NAME
        WHEN 'organizations'      THEN 'organization'
        WHEN 'teams'              THEN 'team'
        WHEN 'projects'           THEN 'project'
        WHEN 'inventories'        THEN 'inventory'
        WHEN 'job_templates'      THEN 'job_template'
        WHEN 'credentials'        THEN 'credential'
        WHEN 'workflow_templates' THEN 'workflow_template'
    END;
    IF ct IS NOT NULL THEN
        DELETE FROM roles WHERE content_type = ct AND object_id = OLD.id;
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_delete_workflow_template_roles ON workflow_templates;
CREATE TRIGGER trg_delete_workflow_template_roles
    AFTER DELETE ON workflow_templates
    FOR EACH ROW EXECUTE FUNCTION delete_object_roles();

-- Backfill existing workflow templates: create all role rows + all parent edges,
-- THEN recompute ancestors (transitive closure reads role_parents).
DO $$
DECLARE
    wt RECORD;
    admin_id BIGINT; exec_id BIGINT; appr_id BIGINT; read_id BIGINT;
    org_wf_admin_id BIGINT; org_exec_id BIGINT; org_appr_id BIGINT; org_aud_id BIGINT;
BEGIN
    FOR wt IN SELECT id, organization_id, name FROM workflow_templates wtpl
              WHERE NOT EXISTS (SELECT 1 FROM roles
                                WHERE content_type = 'workflow_template' AND object_id = wtpl.id) LOOP
        SELECT id INTO org_wf_admin_id FROM roles WHERE content_type='organization' AND object_id=wt.organization_id AND role_field='workflow_admin_role';
        SELECT id INTO org_exec_id     FROM roles WHERE content_type='organization' AND object_id=wt.organization_id AND role_field='execute_role';
        SELECT id INTO org_appr_id     FROM roles WHERE content_type='organization' AND object_id=wt.organization_id AND role_field='approval_role';
        SELECT id INTO org_aud_id      FROM roles WHERE content_type='organization' AND object_id=wt.organization_id AND role_field='auditor_role';

        INSERT INTO roles (role_field, content_type, object_id, name, description)
        VALUES ('admin_role','workflow_template',wt.id, wt.name||' Admin','Can manage all aspects of the workflow template') RETURNING id INTO admin_id;
        INSERT INTO roles (role_field, content_type, object_id, name, description)
        VALUES ('execute_role','workflow_template',wt.id, wt.name||' Execute','Can launch the workflow template') RETURNING id INTO exec_id;
        INSERT INTO roles (role_field, content_type, object_id, name, description)
        VALUES ('approval_role','workflow_template',wt.id, wt.name||' Approve','Can approve or deny approval nodes of this workflow') RETURNING id INTO appr_id;
        INSERT INTO roles (role_field, content_type, object_id, name, description)
        VALUES ('read_role','workflow_template',wt.id, wt.name||' Read','Can view the workflow template') RETURNING id INTO read_id;

        INSERT INTO role_parents (role_id, parent_role_id) VALUES (admin_id, org_wf_admin_id);
        INSERT INTO role_parents (role_id, parent_role_id) VALUES (exec_id, admin_id);
        IF org_exec_id IS NOT NULL THEN INSERT INTO role_parents (role_id, parent_role_id) VALUES (exec_id, org_exec_id); END IF;
        IF org_appr_id IS NOT NULL THEN INSERT INTO role_parents (role_id, parent_role_id) VALUES (appr_id, org_appr_id); END IF;
        INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_id, exec_id);
        INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_id, appr_id);
        INSERT INTO role_parents (role_id, parent_role_id) VALUES (read_id, org_aud_id);

        PERFORM compute_role_ancestors(admin_id);
        PERFORM compute_role_ancestors(exec_id);
        PERFORM compute_role_ancestors(appr_id);
        PERFORM compute_role_ancestors(read_id);
    END LOOP;
END $$;
