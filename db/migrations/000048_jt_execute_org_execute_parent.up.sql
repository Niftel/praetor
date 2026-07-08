-- Wire the missing hierarchy edge: a job template's execute_role should have the
-- organization's execute_role as a parent (in addition to the JT admin_role).
--
-- AWX defines JobTemplate.execute_role with implicit parents
-- [admin_role, organization.execute_role], so a holder of the org execute_role
-- may run any job template in the org. Praetor's original trigger only wired the
-- JT admin_role, leaving the org execute_role inert (nothing was ever a child of
-- it). This adds the edge for new JTs and backfills existing ones.
--
-- Because a JT's read_role is a child of its execute_role, the org execute_role
-- also flows into read visibility (org-execute holders can see the JTs they may
-- run) — so we recompute ancestors for both the execute_role and the read_role
-- of every existing job template.

CREATE OR REPLACE FUNCTION create_job_template_roles() RETURNS TRIGGER AS $$
DECLARE
    admin_role_id BIGINT;
    execute_role_id BIGINT;
    read_role_id BIGINT;
    org_jt_admin_id BIGINT;
    org_execute_id BIGINT;
    org_auditor_id BIGINT;
BEGIN
    -- Get parent org roles
    SELECT id INTO org_jt_admin_id FROM roles
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'job_template_admin_role';
    SELECT id INTO org_execute_id FROM roles
    WHERE content_type = 'organization' AND object_id = NEW.organization_id AND role_field = 'execute_role';
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
    -- Org execute_role may run any job template in the org.
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

-- Backfill existing job templates: add the org-execute -> jt-execute edge and
-- recompute the affected roles' ancestors.
DO $$
DECLARE
    jt RECORD;
    jt_execute_id BIGINT;
    jt_read_id BIGINT;
    org_execute_id BIGINT;
BEGIN
    FOR jt IN SELECT id, organization_id FROM job_templates LOOP
        SELECT id INTO jt_execute_id FROM roles
        WHERE content_type = 'job_template' AND object_id = jt.id AND role_field = 'execute_role';
        SELECT id INTO jt_read_id FROM roles
        WHERE content_type = 'job_template' AND object_id = jt.id AND role_field = 'read_role';
        SELECT id INTO org_execute_id FROM roles
        WHERE content_type = 'organization' AND object_id = jt.organization_id AND role_field = 'execute_role';

        IF jt_execute_id IS NOT NULL AND org_execute_id IS NOT NULL THEN
            INSERT INTO role_parents (role_id, parent_role_id)
            VALUES (jt_execute_id, org_execute_id)
            ON CONFLICT (role_id, parent_role_id) DO NOTHING;

            PERFORM compute_role_ancestors(jt_execute_id);
            IF jt_read_id IS NOT NULL THEN
                PERFORM compute_role_ancestors(jt_read_id);
            END IF;
        END IF;
    END LOOP;
END $$;
