DROP TRIGGER IF EXISTS notification_deliveries_validate ON notification_deliveries;
DROP FUNCTION IF EXISTS validate_notification_delivery_scope();
DROP TABLE IF EXISTS notification_delivery_attempts;
DROP TABLE IF EXISTS notification_deliveries;
