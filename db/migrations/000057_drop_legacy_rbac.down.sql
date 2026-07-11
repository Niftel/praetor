-- Irreversible: the legacy AWX-style RBAC tables, triggers, and role-creation functions
-- are removed for good (Gitea #99). Rolling back would require replaying 000011 + friends
-- to rebuild the hierarchy and re-derive it from the capability assignments; not supported.
-- Intentional no-op so the down migration exists.
SELECT 1;
