-- Resource object roles are created by AFTER INSERT triggers (migration 000011)
-- but were never removed when the resource is deleted, so every deleted org /
-- project / inventory / template / credential / team left its object roles
-- orphaned in the roles table. Add the symmetric AFTER DELETE triggers and
-- garbage-collect the orphans that already accumulated.

-- Generic cleanup: delete the object roles belonging to the row being deleted.
-- role_members / role_ancestors / role_parents / team_roles cascade via their
-- FKs to roles(id).
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

DROP TRIGGER IF EXISTS trg_delete_org_roles ON organizations;
CREATE TRIGGER trg_delete_org_roles
    AFTER DELETE ON organizations
    FOR EACH ROW EXECUTE FUNCTION delete_object_roles();

DROP TRIGGER IF EXISTS trg_delete_team_roles ON teams;
CREATE TRIGGER trg_delete_team_roles
    AFTER DELETE ON teams
    FOR EACH ROW EXECUTE FUNCTION delete_object_roles();

DROP TRIGGER IF EXISTS trg_delete_project_roles ON projects;
CREATE TRIGGER trg_delete_project_roles
    AFTER DELETE ON projects
    FOR EACH ROW EXECUTE FUNCTION delete_object_roles();

DROP TRIGGER IF EXISTS trg_delete_inventory_roles ON inventories;
CREATE TRIGGER trg_delete_inventory_roles
    AFTER DELETE ON inventories
    FOR EACH ROW EXECUTE FUNCTION delete_object_roles();

DROP TRIGGER IF EXISTS trg_delete_job_template_roles ON job_templates;
CREATE TRIGGER trg_delete_job_template_roles
    AFTER DELETE ON job_templates
    FOR EACH ROW EXECUTE FUNCTION delete_object_roles();

DROP TRIGGER IF EXISTS trg_delete_credential_roles ON credentials;
CREATE TRIGGER trg_delete_credential_roles
    AFTER DELETE ON credentials
    FOR EACH ROW EXECUTE FUNCTION delete_object_roles();

-- Garbage-collect roles whose object no longer exists.
DELETE FROM roles r
WHERE r.object_id IS NOT NULL AND (
    (r.content_type = 'organization' AND NOT EXISTS (SELECT 1 FROM organizations o WHERE o.id = r.object_id)) OR
    (r.content_type = 'team'         AND NOT EXISTS (SELECT 1 FROM teams t         WHERE t.id = r.object_id)) OR
    (r.content_type = 'project'      AND NOT EXISTS (SELECT 1 FROM projects p      WHERE p.id = r.object_id)) OR
    (r.content_type = 'inventory'    AND NOT EXISTS (SELECT 1 FROM inventories i   WHERE i.id = r.object_id)) OR
    (r.content_type = 'job_template' AND NOT EXISTS (SELECT 1 FROM job_templates j WHERE j.id = r.object_id)) OR
    (r.content_type = 'credential'   AND NOT EXISTS (SELECT 1 FROM credentials c   WHERE c.id = r.object_id))
);
