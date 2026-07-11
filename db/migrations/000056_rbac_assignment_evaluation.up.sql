-- DAB capability RBAC, phase 3 (Gitea #96, epic #93): assignment + evaluation layer.
--
-- Adds the tables and machinery that make the managed/custom RoleDefinitions (#94/#95)
-- actually checkable. Still additive — enforcement is not switched over until #97; the
-- legacy roles/role_ancestors path remains the source of truth during dual-run.
--
--   object_roles          a RoleDefinition instantiated on an object. (NULL, NULL) scope
--                         is a GLOBAL/system role (System Admin/Auditor) — unified with
--                         object-scoped roles, evaluated at query time from the
--                         definition's permissions (no per-object rows).
--   role_user_assignments / role_team_assignments   bind an actor to an object_role.
--   role_evaluations      denormalized fast-lookup cache: one row per
--                         (object_role, target object, codename). role_evaluations_uuid
--                         is the symmetric variant for UUID-keyed objects (unused today).
--   rbac_content_hierarchy   the parent->child graph propagation walks (general/recursive).
--
-- Maintenance mirrors the existing role_ancestors discipline: a rebuild function plus
-- triggers keep the cache correct as assignments, definitions, and objects change.

-- ── Assignment layer ────────────────────────────────────────────────────────────

-- An ObjectRole: a definition applied to one object. content_type/object_id both NULL
-- means a global (system-wide) role.
CREATE TABLE IF NOT EXISTS object_roles (
    id                 BIGSERIAL PRIMARY KEY,
    role_definition_id BIGINT NOT NULL REFERENCES role_definitions(id) ON DELETE CASCADE,
    content_type       TEXT,
    object_id          BIGINT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((content_type IS NULL) = (object_id IS NULL))
);
-- One object_role per (definition, scoped object); and one global object_role per
-- definition (NULLs are distinct under a plain UNIQUE, so a partial index enforces it).
CREATE UNIQUE INDEX IF NOT EXISTS uq_object_roles_scoped
    ON object_roles (role_definition_id, content_type, object_id)
    WHERE content_type IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_object_roles_global
    ON object_roles (role_definition_id)
    WHERE content_type IS NULL;
CREATE INDEX IF NOT EXISTS idx_object_roles_object ON object_roles (content_type, object_id);

CREATE TABLE IF NOT EXISTS role_user_assignments (
    id                 BIGSERIAL PRIMARY KEY,
    role_definition_id BIGINT NOT NULL REFERENCES role_definitions(id) ON DELETE CASCADE,
    user_id            BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    object_role_id     BIGINT NOT NULL REFERENCES object_roles(id) ON DELETE CASCADE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, object_role_id)
);
CREATE INDEX IF NOT EXISTS idx_rua_object_role ON role_user_assignments (object_role_id);
CREATE INDEX IF NOT EXISTS idx_rua_user ON role_user_assignments (user_id);

CREATE TABLE IF NOT EXISTS role_team_assignments (
    id                 BIGSERIAL PRIMARY KEY,
    role_definition_id BIGINT NOT NULL REFERENCES role_definitions(id) ON DELETE CASCADE,
    team_id            BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    object_role_id     BIGINT NOT NULL REFERENCES object_roles(id) ON DELETE CASCADE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, object_role_id)
);
CREATE INDEX IF NOT EXISTS idx_rta_object_role ON role_team_assignments (object_role_id);
CREATE INDEX IF NOT EXISTS idx_rta_team ON role_team_assignments (team_id);

-- ── Evaluation cache ──────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS role_evaluations (
    object_role_id BIGINT NOT NULL REFERENCES object_roles(id) ON DELETE CASCADE,
    content_type   TEXT   NOT NULL,
    object_id      BIGINT NOT NULL,
    codename       TEXT   NOT NULL,
    PRIMARY KEY (object_role_id, content_type, object_id, codename)
);
CREATE INDEX IF NOT EXISTS idx_role_eval_lookup ON role_evaluations (content_type, object_id, codename);

-- Symmetric variant for UUID-keyed objects (none exist yet; kept for DAB parity so a
-- future UUID-PK RBAC object needs no schema reshape).
CREATE TABLE IF NOT EXISTS role_evaluations_uuid (
    object_role_id BIGINT NOT NULL REFERENCES object_roles(id) ON DELETE CASCADE,
    content_type   TEXT NOT NULL,
    object_id      UUID NOT NULL,
    codename       TEXT NOT NULL,
    PRIMARY KEY (object_role_id, content_type, object_id, codename)
);
CREATE INDEX IF NOT EXISTS idx_role_eval_uuid_lookup ON role_evaluations_uuid (content_type, object_id, codename);

-- ── Content hierarchy graph (propagation walks this) ──────────────────────────

CREATE TABLE IF NOT EXISTS rbac_content_hierarchy (
    parent_type TEXT NOT NULL,
    child_type  TEXT NOT NULL,
    child_table TEXT NOT NULL, -- table holding child rows
    fk_column   TEXT NOT NULL, -- column on child_table pointing at the parent's id
    PRIMARY KEY (parent_type, child_type)
);

INSERT INTO rbac_content_hierarchy (parent_type, child_type, child_table, fk_column) VALUES
    ('organization', 'team',              'teams',              'organization_id'),
    ('organization', 'project',           'projects',           'organization_id'),
    ('organization', 'inventory',         'inventories',        'organization_id'),
    ('organization', 'credential',        'credentials',        'organization_id'),
    ('organization', 'job_template',      'job_templates',      'organization_id'),
    ('organization', 'workflow_template', 'workflow_templates', 'organization_id')
ON CONFLICT (parent_type, child_type) DO NOTHING;

-- rbac_descendants walks rbac_content_hierarchy from a root object, breadth-first, and
-- returns every (content_type, id) reachable beneath it. General and multi-level: adding
-- a deeper edge (e.g. inventory->host) to rbac_content_hierarchy extends it with no code
-- change. Uses dynamic SQL because each edge targets a different table/column.
CREATE OR REPLACE FUNCTION rbac_descendants(root_ct TEXT, root_id BIGINT)
RETURNS TABLE(ct TEXT, id BIGINT) AS $$
DECLARE
    frontier_ct TEXT[]   := ARRAY[root_ct];
    frontier_id BIGINT[] := ARRAY[root_id];
    next_ct TEXT[];
    next_id BIGINT[];
    i INT;
    edge RECORD;
    rec RECORD;
BEGIN
    WHILE array_length(frontier_ct, 1) IS NOT NULL LOOP
        next_ct := ARRAY[]::TEXT[];
        next_id := ARRAY[]::BIGINT[];
        FOR i IN 1 .. array_length(frontier_ct, 1) LOOP
            FOR edge IN
                SELECT child_type, child_table, fk_column
                FROM rbac_content_hierarchy WHERE parent_type = frontier_ct[i]
            LOOP
                FOR rec IN EXECUTE
                    format('SELECT id FROM %I WHERE %I = $1', edge.child_table, edge.fk_column)
                    USING frontier_id[i]
                LOOP
                    ct := edge.child_type;
                    id := rec.id;
                    RETURN NEXT;
                    next_ct := next_ct || edge.child_type;
                    next_id := next_id || rec.id;
                END LOOP;
            END LOOP;
        END LOOP;
        frontier_ct := next_ct;
        frontier_id := next_id;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- rebuild_object_role_evaluations recomputes the cache rows for one object_role from its
-- definition's permissions, applying the propagation rules:
--   * global role (NULL scope)         -> no rows (evaluated at query time)
--   * action = add                     -> the assigned (container) object itself
--   * permission type = assigned type  -> the assigned object itself
--   * permission type is a descendant  -> every matching descendant object
CREATE OR REPLACE FUNCTION rebuild_object_role_evaluations(p_or_id BIGINT)
RETURNS VOID AS $$
DECLARE
    or_ct  TEXT;
    or_id  BIGINT;
    or_def BIGINT;
    perm   RECORD;
BEGIN
    DELETE FROM role_evaluations WHERE object_role_id = p_or_id;
    SELECT role_definition_id, content_type, object_id INTO or_def, or_ct, or_id
        FROM object_roles WHERE id = p_or_id;
    IF NOT FOUND OR or_ct IS NULL THEN
        RETURN; -- missing, or a global role (query-time evaluated)
    END IF;

    FOR perm IN
        SELECT p.content_type AS pct, p.action AS action, p.codename AS codename
        FROM role_definition_permissions rdp
        JOIN dab_permissions p ON p.id = rdp.permission_id
        WHERE rdp.role_definition_id = or_def
    LOOP
        IF perm.action = 'add' OR perm.pct = or_ct THEN
            INSERT INTO role_evaluations (object_role_id, content_type, object_id, codename)
            VALUES (p_or_id, or_ct, or_id, perm.codename)
            ON CONFLICT DO NOTHING;
        ELSE
            INSERT INTO role_evaluations (object_role_id, content_type, object_id, codename)
            SELECT p_or_id, d.ct, d.id, perm.codename
            FROM rbac_descendants(or_ct, or_id) d
            WHERE d.ct = perm.pct
            ON CONFLICT DO NOTHING;
        END IF;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- ── Incremental maintenance triggers ──────────────────────────────────────────

-- When a child object is created, add the cache rows contributed by org-scoped
-- object_roles on its organization (so a new inventory is instantly visible to an
-- existing Organization Auditor). One function, keyed off TG_TABLE_NAME; extend the CASE
-- when a new child content type is added.
CREATE OR REPLACE FUNCTION rbac_on_child_insert() RETURNS TRIGGER AS $$
DECLARE
    ct TEXT;
BEGIN
    ct := CASE TG_TABLE_NAME
        WHEN 'teams'              THEN 'team'
        WHEN 'projects'           THEN 'project'
        WHEN 'inventories'        THEN 'inventory'
        WHEN 'credentials'        THEN 'credential'
        WHEN 'job_templates'      THEN 'job_template'
        WHEN 'workflow_templates' THEN 'workflow_template'
    END;
    IF ct IS NULL THEN
        RETURN NEW;
    END IF;
    INSERT INTO role_evaluations (object_role_id, content_type, object_id, codename)
    SELECT orl.id, ct, NEW.id, p.codename
    FROM object_roles orl
    JOIN role_definition_permissions rdp ON rdp.role_definition_id = orl.role_definition_id
    JOIN dab_permissions p ON p.id = rdp.permission_id
    WHERE orl.content_type = 'organization' AND orl.object_id = NEW.organization_id
      AND p.content_type = ct AND p.action <> 'add'
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- When an RBAC object is deleted, drop cache rows targeting it and any object_roles
-- assigned to it (cascading to their assignments + evaluations).
CREATE OR REPLACE FUNCTION rbac_on_object_delete() RETURNS TRIGGER AS $$
DECLARE
    ct TEXT;
BEGIN
    ct := CASE TG_TABLE_NAME
        WHEN 'organizations'      THEN 'organization'
        WHEN 'teams'              THEN 'team'
        WHEN 'projects'           THEN 'project'
        WHEN 'inventories'        THEN 'inventory'
        WHEN 'credentials'        THEN 'credential'
        WHEN 'job_templates'      THEN 'job_template'
        WHEN 'workflow_templates' THEN 'workflow_template'
    END;
    IF ct IS NULL THEN
        RETURN OLD;
    END IF;
    DELETE FROM role_evaluations WHERE content_type = ct AND object_id = OLD.id;
    DELETE FROM object_roles WHERE content_type = ct AND object_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE
    tbl TEXT;
BEGIN
    FOREACH tbl IN ARRAY ARRAY['teams','projects','inventories','credentials','job_templates','workflow_templates'] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS trg_rbac_child_insert ON %I', tbl);
        EXECUTE format('CREATE TRIGGER trg_rbac_child_insert AFTER INSERT ON %I FOR EACH ROW EXECUTE FUNCTION rbac_on_child_insert()', tbl);
    END LOOP;
    FOREACH tbl IN ARRAY ARRAY['organizations','teams','projects','inventories','credentials','job_templates','workflow_templates'] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS trg_rbac_object_delete ON %I', tbl);
        EXECUTE format('CREATE TRIGGER trg_rbac_object_delete AFTER DELETE ON %I FOR EACH ROW EXECUTE FUNCTION rbac_on_object_delete()', tbl);
    END LOOP;
END $$;
