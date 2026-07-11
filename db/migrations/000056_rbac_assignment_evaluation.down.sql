-- Reverse 000056 (Gitea #96).
DO $$
DECLARE
    tbl TEXT;
BEGIN
    FOREACH tbl IN ARRAY ARRAY['teams','projects','inventories','credentials','job_templates','workflow_templates'] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS trg_rbac_child_insert ON %I', tbl);
    END LOOP;
    FOREACH tbl IN ARRAY ARRAY['organizations','teams','projects','inventories','credentials','job_templates','workflow_templates'] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS trg_rbac_object_delete ON %I', tbl);
    END LOOP;
END $$;

DROP FUNCTION IF EXISTS rbac_on_child_insert();
DROP FUNCTION IF EXISTS rbac_on_object_delete();
DROP FUNCTION IF EXISTS rebuild_object_role_evaluations(BIGINT);
DROP FUNCTION IF EXISTS rbac_descendants(TEXT, BIGINT);

DROP TABLE IF EXISTS role_evaluations_uuid;
DROP TABLE IF EXISTS role_evaluations;
DROP TABLE IF EXISTS role_team_assignments;
DROP TABLE IF EXISTS role_user_assignments;
DROP TABLE IF EXISTS rbac_content_hierarchy;
DROP TABLE IF EXISTS object_roles;
