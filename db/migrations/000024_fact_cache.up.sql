-- Fact caching (Phase 3b): persist gathered ansible_facts per host so later runs
-- can reuse them (Ansible jsonfile cache), gated per template by use_fact_cache.
CREATE TABLE IF NOT EXISTS host_facts (
    host_id     BIGINT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
    facts       JSONB NOT NULL DEFAULT '{}'::jsonb,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE job_templates
    ADD COLUMN IF NOT EXISTS use_fact_cache BOOLEAN NOT NULL DEFAULT false;
