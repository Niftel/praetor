-- Durable logical notification deliveries and immutable attempt history.
--
-- Producers enqueue one row per (occurrence, policy) using idempotency_key.
-- The notification consumer is the only process that reads encrypted target
-- configuration; this table stores only bounded operator-visible metadata.
CREATE TABLE notification_deliveries (
    id                       BIGSERIAL PRIMARY KEY,
    idempotency_key          TEXT NOT NULL UNIQUE
                               CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    organization_id          BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    team_id                  BIGINT REFERENCES teams(id) ON DELETE SET NULL,
    notification_policy_id   BIGINT REFERENCES notification_policies(id) ON DELETE SET NULL,
    notification_template_id BIGINT REFERENCES notification_templates(id) ON DELETE SET NULL,
    target_name              TEXT NOT NULL CHECK (length(target_name) BETWEEN 1 AND 255),
    target_type              TEXT NOT NULL CHECK (length(target_type) BETWEEN 1 AND 64),
    resource_type            TEXT NOT NULL
                               CHECK (resource_type IN ('job_template', 'workflow_template', 'inventory_source')),
    resource_id              BIGINT NOT NULL CHECK (resource_id > 0),
    event                    TEXT NOT NULL CHECK (length(event) BETWEEN 1 AND 64),
    occurrence_type          TEXT NOT NULL
                               CHECK (occurrence_type IN ('job_event', 'workflow_job', 'workflow_node')),
    occurrence_id            TEXT NOT NULL CHECK (length(occurrence_id) BETWEEN 1 AND 255),
    subject_id               BIGINT NOT NULL CHECK (subject_id > 0),
    subject_name             TEXT NOT NULL CHECK (length(subject_name) BETWEEN 1 AND 255),
    subject_kind             TEXT NOT NULL CHECK (length(subject_kind) BETWEEN 1 AND 64),
    status                   TEXT NOT NULL DEFAULT 'pending'
                               CHECK (status IN ('pending', 'retrying', 'sending', 'delivered', 'failed')),
    attempt_count            SMALLINT NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 10),
    max_attempts             SMALLINT NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 10),
    next_attempt_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_owner              TEXT CHECK (lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128),
    lease_expires_at         TIMESTAMPTZ,
    first_attempt_at         TIMESTAMPTZ,
    last_attempt_at          TIMESTAMPTZ,
    delivered_at             TIMESTAMPTZ,
    failed_at                TIMESTAMPTZ,
    failure_code             TEXT CHECK (failure_code IS NULL OR length(failure_code) BETWEEN 1 AND 64),
    failure_reason           TEXT CHECK (failure_reason IS NULL OR length(failure_reason) BETWEEN 1 AND 512),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (attempt_count <= max_attempts),
    CHECK (
        (status = 'sending' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR
        (status <> 'sending' AND lease_owner IS NULL AND lease_expires_at IS NULL)
    ),
    CHECK (status <> 'delivered' OR (delivered_at IS NOT NULL AND failed_at IS NULL)),
    CHECK (status <> 'failed' OR (failed_at IS NOT NULL AND delivered_at IS NULL))
);

CREATE INDEX idx_notification_deliveries_claim
    ON notification_deliveries (
        (CASE WHEN status = 'sending' THEN lease_expires_at ELSE next_attempt_at END),
        id
    )
    WHERE status IN ('pending', 'retrying', 'sending');
CREATE INDEX idx_notification_deliveries_org_history
    ON notification_deliveries (organization_id, id DESC);
CREATE INDEX idx_notification_deliveries_team_history
    ON notification_deliveries (team_id, id DESC)
    WHERE team_id IS NOT NULL;

CREATE TABLE notification_delivery_attempts (
    id              BIGSERIAL PRIMARY KEY,
    delivery_id     BIGINT NOT NULL REFERENCES notification_deliveries(id) ON DELETE CASCADE,
    attempt_number  SMALLINT NOT NULL CHECK (attempt_number BETWEEN 1 AND 10),
    outcome         TEXT NOT NULL
                      CHECK (outcome IN ('delivered', 'transient_failure', 'permanent_failure')),
    failure_code    TEXT CHECK (failure_code IS NULL OR length(failure_code) BETWEEN 1 AND 64),
    failure_reason  TEXT CHECK (failure_reason IS NULL OR length(failure_reason) BETWEEN 1 AND 512),
    started_at      TIMESTAMPTZ NOT NULL,
    finished_at     TIMESTAMPTZ NOT NULL,
    CHECK (finished_at >= started_at),
    CHECK (
        (outcome = 'delivered' AND failure_code IS NULL AND failure_reason IS NULL)
        OR
        (outcome <> 'delivered' AND failure_code IS NOT NULL AND failure_reason IS NOT NULL)
    ),
    UNIQUE (delivery_id, attempt_number)
);

CREATE INDEX idx_notification_delivery_attempts_delivery
    ON notification_delivery_attempts (delivery_id, attempt_number);

-- Reject cross-organization references at the storage boundary. History keeps
-- name/type snapshots after a policy or target is deleted, but every enqueue
-- must refer to a currently valid policy and target in the same organization.
CREATE OR REPLACE FUNCTION validate_notification_delivery_scope()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    policy_org BIGINT;
    policy_team BIGINT;
    policy_target BIGINT;
    policy_resource_type TEXT;
    policy_resource_id BIGINT;
    policy_event TEXT;
    target_org BIGINT;
    current_target_name TEXT;
    current_target_type TEXT;
BEGIN
    IF NEW.notification_policy_id IS NULL OR NEW.notification_template_id IS NULL THEN
        RAISE EXCEPTION 'notification delivery requires an active policy and target';
    END IF;

    SELECT organization_id, team_id, notification_template_id, resource_type, resource_id, event
      INTO policy_org, policy_team, policy_target, policy_resource_type, policy_resource_id, policy_event
      FROM notification_policies
     WHERE id = NEW.notification_policy_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'notification delivery policy does not exist';
    END IF;

    SELECT organization_id, name, notification_type
      INTO target_org, current_target_name, current_target_type
      FROM notification_templates
     WHERE id = NEW.notification_template_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'notification delivery target does not exist';
    END IF;

    IF NEW.organization_id <> policy_org OR NEW.organization_id <> target_org
       OR NEW.team_id IS DISTINCT FROM policy_team
       OR NEW.notification_template_id <> policy_target
       OR NEW.resource_type <> policy_resource_type
       OR NEW.resource_id <> policy_resource_id
       OR NEW.event <> policy_event THEN
        RAISE EXCEPTION 'notification delivery scope does not match its policy';
    END IF;

    IF NEW.team_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM teams
         WHERE id = NEW.team_id AND organization_id = NEW.organization_id
    ) THEN
        RAISE EXCEPTION 'notification delivery team belongs to another organization';
    END IF;

    -- Snapshot only the non-secret target identity. Producers cannot smuggle
    -- alternate labels into operator-visible history.
    NEW.target_name := current_target_name;
    NEW.target_type := current_target_type;
    RETURN NEW;
END;
$$;

CREATE TRIGGER notification_deliveries_validate
    BEFORE INSERT ON notification_deliveries
    FOR EACH ROW EXECUTE FUNCTION validate_notification_delivery_scope();
