-- Phase 0 cleanup: drop tables/columns that model features Praetor does not have.
-- There are no Execution Environments (agentless host-runner, no job containers)
-- and no managed instance fleet (the control plane is fixed services; jobs run
-- on runner hosts, not registered instances). Removing them stops the schema
-- from implying capabilities that don't exist.
ALTER TABLE job_templates  DROP COLUMN IF EXISTS execution_environment_id;
ALTER TABLE execution_runs DROP COLUMN IF EXISTS executor_instance_id;

DROP TABLE IF EXISTS execution_environments;
DROP TABLE IF EXISTS instance_groups;
DROP TABLE IF EXISTS instances;
