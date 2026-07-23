-- Common notification routing policy model. Notification targets remain
-- organization-owned secrets; policies decide which target receives which
-- automation event. Approval events are always scoped to an explicit team.
CREATE TABLE notification_policies (
    id                       BIGSERIAL PRIMARY KEY,
    organization_id          BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    team_id                  BIGINT REFERENCES teams(id) ON DELETE CASCADE,
    notification_template_id BIGINT NOT NULL REFERENCES notification_templates(id) ON DELETE CASCADE,
    resource_type            TEXT NOT NULL CHECK (resource_type IN ('job_template', 'workflow_template', 'inventory_source')),
    resource_id              BIGINT NOT NULL,
    event                    TEXT NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_notification_policies_scope
    ON notification_policies (
        organization_id,
        COALESCE(team_id, 0),
        notification_template_id,
        resource_type,
        resource_id,
        event
    );

CREATE INDEX idx_notification_policies_delivery
    ON notification_policies (resource_type, resource_id, event, team_id);

-- Polymorphic resource ownership cannot be expressed as a normal foreign key.
-- Validate it centrally so neither an API bug nor a direct database client can
-- connect a target, team, and automation resource from different organizations.
CREATE OR REPLACE FUNCTION validate_notification_policy()
RETURNS trigger AS $$
DECLARE
    resource_org BIGINT;
    target_org BIGINT;
    policy_team_org BIGINT;
BEGIN
    IF NEW.resource_type = 'job_template' THEN
        SELECT organization_id INTO resource_org FROM job_templates WHERE id = NEW.resource_id;
        IF NEW.event NOT IN ('started', 'success', 'error') THEN
            RAISE EXCEPTION 'unsupported job template notification event: %', NEW.event;
        END IF;
        IF NEW.team_id IS NOT NULL THEN
            RAISE EXCEPTION 'job template notification policies must be organization scoped';
        END IF;
    ELSIF NEW.resource_type = 'workflow_template' THEN
        SELECT organization_id INTO resource_org FROM workflow_templates WHERE id = NEW.resource_id;
        IF NEW.event NOT IN ('started', 'success', 'error', 'approval', 'approved', 'denied', 'timeout') THEN
            RAISE EXCEPTION 'unsupported workflow template notification event: %', NEW.event;
        END IF;
        IF NEW.event IN ('approval', 'approved', 'denied', 'timeout') AND NEW.team_id IS NULL THEN
            RAISE EXCEPTION 'workflow approval notification policies require a team';
        END IF;
        IF NEW.event IN ('started', 'success', 'error') AND NEW.team_id IS NOT NULL THEN
            RAISE EXCEPTION 'workflow lifecycle notification policies must be organization scoped';
        END IF;
    ELSIF NEW.resource_type = 'inventory_source' THEN
        SELECT i.organization_id INTO resource_org
          FROM inventory_sources src
          JOIN inventories i ON i.id = src.inventory_id
         WHERE src.id = NEW.resource_id;
        IF NEW.event NOT IN ('started', 'success', 'error') THEN
            RAISE EXCEPTION 'unsupported inventory source notification event: %', NEW.event;
        END IF;
        IF NEW.team_id IS NOT NULL THEN
            RAISE EXCEPTION 'inventory source notification policies must be organization scoped';
        END IF;
    END IF;

    IF resource_org IS NULL THEN
        RAISE EXCEPTION 'notification policy resource does not exist';
    END IF;
    IF resource_org <> NEW.organization_id THEN
        RAISE EXCEPTION 'notification policy resource belongs to another organization';
    END IF;

    SELECT organization_id INTO target_org
      FROM notification_templates
     WHERE id = NEW.notification_template_id;
    IF target_org IS NULL THEN
        RAISE EXCEPTION 'notification target does not exist';
    END IF;
    IF target_org <> NEW.organization_id THEN
        RAISE EXCEPTION 'notification target belongs to another organization';
    END IF;

    IF NEW.team_id IS NOT NULL THEN
        SELECT organization_id INTO policy_team_org FROM teams WHERE id = NEW.team_id;
        IF policy_team_org IS NULL THEN
            RAISE EXCEPTION 'notification policy team does not exist';
        END IF;
        IF policy_team_org <> NEW.organization_id THEN
            RAISE EXCEPTION 'notification policy team belongs to another organization';
        END IF;
    END IF;

    NEW.modified_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER notification_policies_validate
    BEFORE INSERT OR UPDATE ON notification_policies
    FOR EACH ROW EXECUTE FUNCTION validate_notification_policy();

-- Migrate ordinary lifecycle attachments as organization policies.
INSERT INTO notification_policies (
    organization_id, notification_template_id, resource_type, resource_id, event
)
SELECT jt.organization_id, jtn.notification_template_id, 'job_template', jtn.job_template_id, jtn.event
  FROM job_template_notifications jtn
  JOIN job_templates jt ON jt.id = jtn.job_template_id
ON CONFLICT DO NOTHING;

INSERT INTO notification_policies (
    organization_id, notification_template_id, resource_type, resource_id, event
)
SELECT wt.organization_id, wtn.notification_template_id, 'workflow_template', wtn.workflow_template_id, wtn.event
  FROM workflow_template_notifications wtn
  JOIN workflow_templates wt ON wt.id = wtn.workflow_template_id
 WHERE wtn.event NOT IN ('approval', 'approved', 'denied', 'timeout')
ON CONFLICT DO NOTHING;

-- Legacy approval attachments had no recipient scope. Preserve their behavior
-- without creating an organization-wide approval route by materializing one
-- policy for each existing team. Delivery still selects only the team assigned
-- to the workflow run.
INSERT INTO notification_policies (
    organization_id, team_id, notification_template_id, resource_type, resource_id, event
)
SELECT wt.organization_id, t.id, wtn.notification_template_id,
       'workflow_template', wtn.workflow_template_id, wtn.event
  FROM workflow_template_notifications wtn
  JOIN workflow_templates wt ON wt.id = wtn.workflow_template_id
  JOIN teams t ON t.organization_id = wt.organization_id
 WHERE wtn.event IN ('approval', 'approved', 'denied', 'timeout')
ON CONFLICT DO NOTHING;

-- Keep the existing job/workflow attachment endpoints operational during the
-- rolling migration. Team-scoped approval policies deliberately do not write
-- back to the legacy table because that table cannot represent their scope.
CREATE OR REPLACE FUNCTION sync_legacy_notification_attachment_to_policy()
RETURNS trigger AS $$
DECLARE
    resource_org BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'job_template_notifications' THEN
        IF TG_OP = 'DELETE' THEN
            DELETE FROM notification_policies
             WHERE resource_type = 'job_template'
               AND resource_id = OLD.job_template_id
               AND notification_template_id = OLD.notification_template_id
               AND event = OLD.event
               AND team_id IS NULL;
            RETURN OLD;
        END IF;
        SELECT organization_id INTO resource_org FROM job_templates WHERE id = NEW.job_template_id;
        INSERT INTO notification_policies (
            organization_id, notification_template_id, resource_type, resource_id, event
        ) VALUES (
            resource_org, NEW.notification_template_id, 'job_template', NEW.job_template_id, NEW.event
        ) ON CONFLICT DO NOTHING;
        RETURN NEW;
    END IF;

    IF TG_OP = 'DELETE' THEN
        IF OLD.event IN ('approval', 'approved', 'denied', 'timeout') THEN
            DELETE FROM notification_policies
             WHERE resource_type = 'workflow_template'
               AND resource_id = OLD.workflow_template_id
               AND notification_template_id = OLD.notification_template_id
               AND event = OLD.event;
        ELSE
            DELETE FROM notification_policies
             WHERE resource_type = 'workflow_template'
               AND resource_id = OLD.workflow_template_id
               AND notification_template_id = OLD.notification_template_id
               AND event = OLD.event
               AND team_id IS NULL;
        END IF;
        RETURN OLD;
    END IF;

    SELECT organization_id INTO resource_org FROM workflow_templates WHERE id = NEW.workflow_template_id;
    IF NEW.event IN ('approval', 'approved', 'denied', 'timeout') THEN
        INSERT INTO notification_policies (
            organization_id, team_id, notification_template_id, resource_type, resource_id, event
        )
        SELECT resource_org, t.id, NEW.notification_template_id,
               'workflow_template', NEW.workflow_template_id, NEW.event
          FROM teams t
         WHERE t.organization_id = resource_org
        ON CONFLICT DO NOTHING;
    ELSE
        INSERT INTO notification_policies (
            organization_id, notification_template_id, resource_type, resource_id, event
        ) VALUES (
            resource_org, NEW.notification_template_id, 'workflow_template', NEW.workflow_template_id, NEW.event
        ) ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER job_template_notifications_policy_sync
    AFTER INSERT OR DELETE ON job_template_notifications
    FOR EACH ROW EXECUTE FUNCTION sync_legacy_notification_attachment_to_policy();

CREATE TRIGGER workflow_template_notifications_policy_sync
    AFTER INSERT OR DELETE ON workflow_template_notifications
    FOR EACH ROW EXECUTE FUNCTION sync_legacy_notification_attachment_to_policy();

CREATE OR REPLACE FUNCTION sync_policy_to_legacy_notification_attachment()
RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.team_id IS NOT NULL THEN
            RETURN OLD;
        END IF;
        IF OLD.resource_type = 'job_template' THEN
            DELETE FROM job_template_notifications
             WHERE job_template_id = OLD.resource_id
               AND notification_template_id = OLD.notification_template_id
               AND event = OLD.event;
        ELSIF OLD.resource_type = 'workflow_template' THEN
            DELETE FROM workflow_template_notifications
             WHERE workflow_template_id = OLD.resource_id
               AND notification_template_id = OLD.notification_template_id
               AND event = OLD.event;
        END IF;
        RETURN OLD;
    END IF;

    IF NEW.team_id IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.resource_type = 'job_template' THEN
        INSERT INTO job_template_notifications (job_template_id, notification_template_id, event)
        VALUES (NEW.resource_id, NEW.notification_template_id, NEW.event)
        ON CONFLICT DO NOTHING;
    ELSIF NEW.resource_type = 'workflow_template' THEN
        INSERT INTO workflow_template_notifications (workflow_template_id, notification_template_id, event)
        VALUES (NEW.resource_id, NEW.notification_template_id, NEW.event)
        ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER notification_policies_legacy_sync
    AFTER INSERT OR DELETE ON notification_policies
    FOR EACH ROW EXECUTE FUNCTION sync_policy_to_legacy_notification_attachment();
