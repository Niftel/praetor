-- Retire the legacy AWX-style RBAC (Gitea #99, epic #93). Authorization, grants, and the
-- management APIs now run entirely on the DAB capability model (dab_permissions,
-- role_definitions, object_roles, role_*_assignments, role_evaluations). Nothing reads or
-- writes the legacy hierarchy anymore, so drop it.
--
-- The capability model keeps its own triggers (rbac_on_child_insert, rbac_on_object_delete
-- from 000056) and team_members — untouched here.

-- Drop the legacy trigger functions with CASCADE: this removes the per-object
-- trg_create_*_roles / trg_delete_*_roles triggers that depended on them.
DROP FUNCTION IF EXISTS create_organization_roles() CASCADE;
DROP FUNCTION IF EXISTS create_team_roles() CASCADE;
DROP FUNCTION IF EXISTS create_project_roles() CASCADE;
DROP FUNCTION IF EXISTS create_inventory_roles() CASCADE;
DROP FUNCTION IF EXISTS create_job_template_roles() CASCADE;
DROP FUNCTION IF EXISTS create_credential_roles() CASCADE;
DROP FUNCTION IF EXISTS create_workflow_template_roles() CASCADE;
DROP FUNCTION IF EXISTS delete_object_roles() CASCADE;
DROP FUNCTION IF EXISTS compute_role_ancestors(BIGINT) CASCADE;
DROP FUNCTION IF EXISTS compute_role_ancestors() CASCADE;

-- Drop the legacy tables (CASCADE clears the inter-table FKs to roles).
DROP TABLE IF EXISTS role_ancestors CASCADE;
DROP TABLE IF EXISTS role_parents CASCADE;
DROP TABLE IF EXISTS role_members CASCADE;
DROP TABLE IF EXISTS team_roles CASCADE;
DROP TABLE IF EXISTS roles CASCADE;
