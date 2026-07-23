DROP TRIGGER IF EXISTS notification_policies_legacy_sync ON notification_policies;
DROP FUNCTION IF EXISTS sync_policy_to_legacy_notification_attachment();

DROP TRIGGER IF EXISTS workflow_template_notifications_policy_sync ON workflow_template_notifications;
DROP TRIGGER IF EXISTS job_template_notifications_policy_sync ON job_template_notifications;
DROP FUNCTION IF EXISTS sync_legacy_notification_attachment_to_policy();

DROP TRIGGER IF EXISTS notification_policies_validate ON notification_policies;
DROP FUNCTION IF EXISTS validate_notification_policy();

DROP TABLE IF EXISTS notification_policies;
