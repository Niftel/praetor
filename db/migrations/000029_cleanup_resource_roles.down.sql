DROP TRIGGER IF EXISTS trg_delete_org_roles ON organizations;
DROP TRIGGER IF EXISTS trg_delete_team_roles ON teams;
DROP TRIGGER IF EXISTS trg_delete_project_roles ON projects;
DROP TRIGGER IF EXISTS trg_delete_inventory_roles ON inventories;
DROP TRIGGER IF EXISTS trg_delete_job_template_roles ON job_templates;
DROP TRIGGER IF EXISTS trg_delete_credential_roles ON credentials;
DROP FUNCTION IF EXISTS delete_object_roles();
