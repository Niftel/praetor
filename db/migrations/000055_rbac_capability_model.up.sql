-- DAB-style capability RBAC, phase 1 (Gitea #94, epic #93).
--
-- Praetor's existing RBAC (000011 + friends) is AWX's *legacy* implicit-Role model:
-- fixed role_fields per object, with the role_field -> action translation hardwired
-- in Go (services/api/handlers/authz.go). This adds the django-ansible-base (DAB)
-- capability layer *alongside* it, additively — nothing here changes enforcement or
-- touches the legacy roles/role_parents/role_ancestors tables yet.
--
--   dab_permissions            the atomic capability: (codename, content_type, action)
--   role_definitions           a named, admin-definable bundle of capabilities
--   role_definition_permissions M2M linking the two
--
-- The permission catalog itself is SEEDED FROM GO (cmd/migrator: seedRBACPermissions,
-- driven by pkg/rbac.PermissionCatalog) so the canonical (content_type, action) set
-- has a single source of truth in code, mirroring how credential_types are seeded.
-- Managed/custom RoleDefinitions and their assignment/evaluation come in later phases
-- (#95 managed-mirror, #96 assignments + evaluation cache).

-- The capability primitive. codename is "<action>_<content_type>", e.g.
-- 'view_inventory', 'execute_job_template', 'approve_workflow_template'.
CREATE TABLE IF NOT EXISTS dab_permissions (
    id           BIGSERIAL PRIMARY KEY,
    codename     TEXT NOT NULL UNIQUE,
    content_type TEXT NOT NULL,
    action       TEXT NOT NULL,
    name         TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (content_type, action)
);

CREATE INDEX IF NOT EXISTS idx_dab_permissions_content_type ON dab_permissions(content_type);

-- A named set of capabilities. managed=true rows mirror the legacy fixed roles
-- (seeded in #95); managed=false rows are operator-defined custom roles. content_type
-- (nullable) optionally restricts which object type the definition may be assigned to;
-- NULL means "any". name is globally unique, DAB-style — scoping to an org/object is a
-- property of the *assignment* (ObjectRole, #96), not of the definition.
CREATE TABLE IF NOT EXISTS role_definitions (
    id           BIGSERIAL PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    description  TEXT NOT NULL DEFAULT '',
    managed      BOOLEAN NOT NULL DEFAULT false,
    content_type TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The composite: which capabilities a definition confers.
CREATE TABLE IF NOT EXISTS role_definition_permissions (
    role_definition_id BIGINT NOT NULL REFERENCES role_definitions(id) ON DELETE CASCADE,
    permission_id      BIGINT NOT NULL REFERENCES dab_permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_definition_id, permission_id)
);

CREATE INDEX IF NOT EXISTS idx_rdp_permission ON role_definition_permissions(permission_id);
