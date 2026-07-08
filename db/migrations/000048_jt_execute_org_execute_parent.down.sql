-- Revert: restore create_job_template_roles() to wire only the JT admin_role as
-- the execute_role's parent, and remove the org-execute -> jt-execute edges from
-- existing job templates, recomputing the affected roles' ancestors.

CREATE OR REPLACE FUNCTION create_job_template_roles() RETURNS TRIGGER AS $$
DECLARE
    admin_role_id BIGINT;
    execute_role_id BIGINT;
    read_role_id BIGINT;
    org_jt_admin_id BIGINT;
    org_auditor_id BIGINT;
BEGIN
    SELECT id INTO org_jt_admin_id FROM roles
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'job_template_admin_role';
    SELECT id INTO org_auditor_id FROM roles
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'auditor_role';

    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('admin_role', 'job_template', NEW.id, NEW.name || ' Admin', 'Can manage all aspects of the job template')
    RETURNING id INTO admin_role_id;

    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('execute_role', 'job_template', NEW.id, NEW.name || ' Execute', 'Can execute the job template')
    RETURNING id INTO execute_role_id;

    INSERT INTO roles (role_field, content_type, object_id, name, description)
    VALUES ('read_role', 'job_template', NEW.id, NEW.name || ' Read', 'Can view the job template')
    RETURNING id INTO read_role_id;

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

-- Remove the backfilled edges and recompute affected ancestors.
DO $$
DECLARE
    edge RECORD;
    jt_read_id BIGINT;
    jt_obj BIGINT;
BEGIN
    FOR edge IN
        SELECT rp.role_id AS jt_execute_id, r.object_id AS jt_object_id
        FROM role_parents rp
        JOIN roles r  ON r.id  = rp.role_id
        JOIN roles pr ON pr.id = rp.parent_role_id
        WHERE r.content_type = 'job_template' AND r.role_field = 'execute_role'
          AND pr.content_type = 'organization' AND pr.role_field = 'execute_role'
    LOOP
        DELETE FROM role_parents rp
        USING roles pr
        WHERE rp.role_id = edge.jt_execute_id
          AND rp.parent_role_id = pr.id
          AND pr.content_type = 'organization' AND pr.role_field = 'execute_role';

        PERFORM compute_role_ancestors(edge.jt_execute_id);

        SELECT id INTO jt_read_id FROM roles
        WHERE content_type = 'job_template' AND object_id = edge.jt_object_id AND role_field = 'read_role';
        IF jt_read_id IS NOT NULL THEN
            PERFORM compute_role_ancestors(jt_read_id);
        END IF;
    END LOOP;
END $$;
