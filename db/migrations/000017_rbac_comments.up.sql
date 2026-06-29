-- Document the canonical RBAC model directly in the schema so its meaning is
-- unambiguous (the earlier permission-list `roles` table from 000001 was already
-- dropped and replaced by the AWX object-role model in 000011; this only adds
-- comments — no structural change).

COMMENT ON TABLE roles IS
  'AWX-style object roles. Each row is a role ATTACHED to an object via '
  '(content_type, object_id, role_field) — e.g. admin_role on organization 5 — '
  'or a system-wide role via singleton_name. This is NOT a permission-list: a '
  'principal''s capabilities derive from which roles they hold plus the role '
  'hierarchy (see role_parents/role_ancestors), not from a permissions column.';

COMMENT ON COLUMN roles.role_field IS
  'The kind of access this role grants on its object: admin_role, read_role, '
  'use_role, execute_role, update_role, member_role, etc.';
COMMENT ON COLUMN roles.singleton_name IS
  'Set for system-wide roles (system_administrator, system_auditor); object '
  'columns are NULL in that case.';

COMMENT ON TABLE role_parents IS
  'Explicit role hierarchy edges (child role_id -> parent_role_id): holding the '
  'parent role implies the child role.';
COMMENT ON TABLE role_ancestors IS
  'Flattened transitive closure of role_parents, maintained for fast permission '
  'lookups (a user in any ancestor role has the descendant role).';
COMMENT ON TABLE role_members IS 'Direct user-to-role grants.';
COMMENT ON TABLE team_roles IS 'Team-to-role grants (every team member inherits the role).';
