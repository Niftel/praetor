-- Reverse 000055 (Gitea #94). Additive tables only; the legacy RBAC is untouched.
DROP TABLE IF EXISTS role_definition_permissions;
DROP TABLE IF EXISTS role_definitions;
DROP TABLE IF EXISTS dab_permissions;
