CREATE TABLE delegated_launch_grants (
    id                       BIGSERIAL PRIMARY KEY,
    organization_id          BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    service_principal_id     BIGINT NOT NULL REFERENCES service_principals(id) ON DELETE CASCADE,
    workflow_template_id     BIGINT NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
    inventory_id             BIGINT NOT NULL REFERENCES inventories(id) ON DELETE CASCADE,
    allowed_host_ids         BIGINT[] NOT NULL DEFAULT '{}',
    allowed_group_ids        BIGINT[] NOT NULL DEFAULT '{}',
    max_hosts                INTEGER,
    allowed_extra_var_keys   TEXT[] NOT NULL DEFAULT '{}',
    approval_team_id         BIGINT REFERENCES teams(id) ON DELETE RESTRICT,
    not_before               TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at               TIMESTAMPTZ NOT NULL,
    created_by_user_id       BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_by_user_id       BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at               TIMESTAMPTZ,
    CHECK (expires_at > not_before),
    CHECK (max_hosts IS NULL OR max_hosts > 0),
    UNIQUE (service_principal_id, workflow_template_id, inventory_id, not_before)
);

CREATE INDEX idx_delegated_launch_grants_principal
    ON delegated_launch_grants (service_principal_id, id);

CREATE INDEX idx_delegated_launch_grants_active
    ON delegated_launch_grants (service_principal_id, workflow_template_id, inventory_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE OR REPLACE FUNCTION validate_delegated_launch_grant()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    principal_org BIGINT;
    workflow_org BIGINT;
    inventory_org BIGINT;
    team_org BIGINT;
BEGIN
    SELECT organization_id INTO principal_org FROM service_principals WHERE id=NEW.service_principal_id;
    SELECT organization_id INTO workflow_org FROM workflow_templates WHERE id=NEW.workflow_template_id;
    SELECT organization_id INTO inventory_org FROM inventories WHERE id=NEW.inventory_id;

    IF principal_org IS NULL OR workflow_org IS NULL OR inventory_org IS NULL
       OR NEW.organization_id <> principal_org
       OR NEW.organization_id <> workflow_org
       OR NEW.organization_id <> inventory_org THEN
        RAISE EXCEPTION 'delegated launch grant resources must share one organization'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.approval_team_id IS NOT NULL THEN
        SELECT organization_id INTO team_org FROM teams WHERE id=NEW.approval_team_id;
        IF team_org IS NULL OR team_org <> NEW.organization_id THEN
            RAISE EXCEPTION 'approval team must belong to the grant organization'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    IF EXISTS (
        SELECT 1 FROM unnest(NEW.allowed_host_ids) AS host_id
        LEFT JOIN hosts h ON h.id=host_id AND h.inventory_id=NEW.inventory_id
        WHERE h.id IS NULL
    ) THEN
        RAISE EXCEPTION 'allowed hosts must belong to the grant inventory'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1 FROM unnest(NEW.allowed_group_ids) AS group_id
        LEFT JOIN groups g ON g.id=group_id AND g.inventory_id=NEW.inventory_id
        WHERE g.id IS NULL
    ) THEN
        RAISE EXCEPTION 'allowed groups must belong to the grant inventory'
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_validate_delegated_launch_grant
    BEFORE INSERT OR UPDATE ON delegated_launch_grants
    FOR EACH ROW EXECUTE FUNCTION validate_delegated_launch_grant();
