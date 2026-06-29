ALTER TABLE role_parents DROP CONSTRAINT IF EXISTS role_parents_role_id_fkey;
ALTER TABLE role_parents DROP CONSTRAINT IF EXISTS role_parents_parent_role_id_fkey;
ALTER TABLE role_ancestors DROP CONSTRAINT IF EXISTS role_ancestors_role_id_fkey;
ALTER TABLE role_ancestors DROP CONSTRAINT IF EXISTS role_ancestors_ancestor_role_id_fkey;
ALTER TABLE role_members DROP CONSTRAINT IF EXISTS role_members_role_id_fkey;
ALTER TABLE team_roles DROP CONSTRAINT IF EXISTS team_roles_role_id_fkey;
