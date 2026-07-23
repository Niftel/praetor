-- notification_policies.resource_id is polymorphic, so a normal foreign key
-- cannot clean routes up when their resource is deleted. Keep that lifecycle
-- inside PostgreSQL alongside the ownership validation trigger.
CREATE OR REPLACE FUNCTION delete_notification_policies_for_resource()
RETURNS trigger AS $$
BEGIN
    DELETE FROM notification_policies
     WHERE resource_type = TG_ARGV[0]
       AND resource_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER job_templates_notification_policy_cleanup
    AFTER DELETE ON job_templates
    FOR EACH ROW EXECUTE FUNCTION delete_notification_policies_for_resource('job_template');

CREATE TRIGGER workflow_templates_notification_policy_cleanup
    AFTER DELETE ON workflow_templates
    FOR EACH ROW EXECUTE FUNCTION delete_notification_policies_for_resource('workflow_template');

CREATE TRIGGER inventory_sources_notification_policy_cleanup
    AFTER DELETE ON inventory_sources
    FOR EACH ROW EXECUTE FUNCTION delete_notification_policies_for_resource('inventory_source');
