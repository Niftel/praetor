ALTER TABLE workflow_job_nodes DROP COLUMN IF EXISTS outcome_notified;
ALTER TABLE workflow_jobs      DROP COLUMN IF EXISTS started_notified;
