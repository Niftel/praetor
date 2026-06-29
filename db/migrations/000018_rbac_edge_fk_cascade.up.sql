-- The role hierarchy/membership edge tables (role_parents, role_ancestors, and
-- the role side of role_members/team_roles) lacked foreign keys to roles. So
-- deleting a role — e.g. cascading from an organization or project delete —
-- left ORPHAN edges behind. Because the roles id sequence keeps advancing, a
-- later create_*_roles trigger eventually produces an id that matches an
-- orphaned edge and collides with the unique constraint, which aborts the
-- INSERT and breaks object creation entirely.
--
-- Fix: remove existing orphans, then add ON DELETE CASCADE foreign keys so a
-- role deletion always removes its own edges. Idempotent (safe to re-run).

-- 1. Remove any existing orphan edges.
DELETE FROM role_parents rp
  WHERE NOT EXISTS (SELECT 1 FROM roles r WHERE r.id = rp.role_id)
     OR NOT EXISTS (SELECT 1 FROM roles r WHERE r.id = rp.parent_role_id);

DELETE FROM role_ancestors ra
  WHERE NOT EXISTS (SELECT 1 FROM roles r WHERE r.id = ra.role_id)
     OR NOT EXISTS (SELECT 1 FROM roles r WHERE r.id = ra.ancestor_role_id);

DELETE FROM role_members rm
  WHERE NOT EXISTS (SELECT 1 FROM roles r WHERE r.id = rm.role_id);

DELETE FROM team_roles tr
  WHERE NOT EXISTS (SELECT 1 FROM roles r WHERE r.id = tr.role_id);

-- 2. Add the missing cascade foreign keys (drop-if-exists keeps this re-runnable).
ALTER TABLE role_parents DROP CONSTRAINT IF EXISTS role_parents_role_id_fkey;
ALTER TABLE role_parents DROP CONSTRAINT IF EXISTS role_parents_parent_role_id_fkey;
ALTER TABLE role_parents
  ADD CONSTRAINT role_parents_role_id_fkey        FOREIGN KEY (role_id)        REFERENCES roles(id) ON DELETE CASCADE,
  ADD CONSTRAINT role_parents_parent_role_id_fkey FOREIGN KEY (parent_role_id) REFERENCES roles(id) ON DELETE CASCADE;

ALTER TABLE role_ancestors DROP CONSTRAINT IF EXISTS role_ancestors_role_id_fkey;
ALTER TABLE role_ancestors DROP CONSTRAINT IF EXISTS role_ancestors_ancestor_role_id_fkey;
ALTER TABLE role_ancestors
  ADD CONSTRAINT role_ancestors_role_id_fkey          FOREIGN KEY (role_id)          REFERENCES roles(id) ON DELETE CASCADE,
  ADD CONSTRAINT role_ancestors_ancestor_role_id_fkey FOREIGN KEY (ancestor_role_id) REFERENCES roles(id) ON DELETE CASCADE;

ALTER TABLE role_members DROP CONSTRAINT IF EXISTS role_members_role_id_fkey;
ALTER TABLE role_members
  ADD CONSTRAINT role_members_role_id_fkey FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE;

ALTER TABLE team_roles DROP CONSTRAINT IF EXISTS team_roles_role_id_fkey;
ALTER TABLE team_roles
  ADD CONSTRAINT team_roles_role_id_fkey FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE;
