ALTER TABLE unified_jobs
    ADD COLUMN IF NOT EXISTS source_job_id BIGINT REFERENCES unified_jobs(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_unified_jobs_source_job_id
    ON unified_jobs (source_job_id, id);

ALTER TABLE job_events
    ADD COLUMN IF NOT EXISTS diagnostic_outcome TEXT
    GENERATED ALWAYS AS (NULLIF(event_data->>'outcome', '')) STORED;

CREATE INDEX IF NOT EXISTS idx_job_events_run_outcome_seq
    ON job_events (execution_run_id, diagnostic_outcome, seq)
    WHERE diagnostic_outcome IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_job_events_run_type_seq
    ON job_events (execution_run_id, event_type, seq);

CREATE INDEX IF NOT EXISTS idx_job_events_run_host_seq
    ON job_events (execution_run_id, seq)
    WHERE host_id IS NOT NULL;
