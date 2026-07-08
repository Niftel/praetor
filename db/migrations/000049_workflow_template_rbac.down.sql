-- Revert workflow-template RBAC: drop the create/delete triggers + function,
-- restore delete_object_roles() to its 000029 form (no workflow arm), and remove
-- the per-workflow roles (role_parents/ancestors/members/team_roles cascade).

DROP TRIGGER IF EXISTS trg_create_workflow_template_roles ON workflow_templates;
DROP TRIGGER IF EXISTS trg_delete_workflow_template_roles ON workflow_templates;
DROP FUNCTION IF EXISTS create_workflow_template_roles();

CREATE OR REPLACE FUNCTION delete_object_roles() RETURNS TRIGGER AS $$
DECLARE
    ct TEXT;
BEGIN
    ct := CASE TG_TABLE_NAME
        WHEN 'organizations' THEN 'organization'
        WHEN 'teams'         THEN 'team'
        WHEN 'projects'      THEN 'project'
        WHEN 'inventories'   THEN 'inventory'
        WHEN 'job_templates' THEN 'job_template'
        WHEN 'credentials'   THEN 'credential'
    END;
    IF ct IS NOT NULL THEN
        DELETE FROM roles WHERE content_type = ct AND object_id = OLD.id;
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DELETE FROM roles WHERE content_type = 'workflow_template';
