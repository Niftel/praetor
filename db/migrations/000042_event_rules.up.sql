-- Event-Driven Automation (EDA-style). Authenticated event SOURCES push events;
-- RULES match on event content and, on a match, launch a job/workflow template
-- with the event injected as context (extra_vars + an optional --limit). This is
-- the "event source -> rulebook condition -> action" loop, adapted to Praetor's
-- push model. See services/api/handlers/events.go.

-- An event source is an authenticated intake channel (EDA "event stream"): its
-- name is the {source} path segment and its token is the shared secret callers
-- (Alertmanager, monitoring, other systems) present.
CREATE TABLE event_sources (
    id              BIGSERIAL PRIMARY KEY,
    organization_id BIGINT      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL UNIQUE,
    token           TEXT        NOT NULL,
    enabled         BOOLEAN     NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A rule: a GRL condition (evaluated by the grule rule engine) matched against an
-- incoming event, and the target to launch when it matches. `condition` is a GRL
-- boolean expression over the `Event` fact's typed accessors, e.g.
--   Event.Str("labels.alertname") == "ApacheDown" && Event.Str("status") == "firing"
-- Paths are dotted and may index arrays (e.g. "alerts.0.status"). source_id NULL
-- means "any source in the org". `limit_field` is a dotted path whose value becomes
-- the launched job's --limit, so remediation targets only the affected host.
-- Exactly one target (job or workflow template) is set.
CREATE TABLE event_rules (
    id                      BIGSERIAL PRIMARY KEY,
    organization_id         BIGINT      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name                    TEXT        NOT NULL,
    enabled                 BOOLEAN     NOT NULL DEFAULT true,
    source_id               BIGINT      REFERENCES event_sources(id) ON DELETE CASCADE,
    condition               TEXT        NOT NULL DEFAULT 'false',
    unified_job_template_id BIGINT      REFERENCES unified_job_templates(id) ON DELETE CASCADE,
    workflow_template_id    BIGINT      REFERENCES workflow_templates(id) ON DELETE CASCADE,
    limit_field             TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_event_rules_source ON event_rules (source_id) WHERE enabled;

-- Receipt log: every accepted event, how many rules it matched, and the jobs it
-- launched — for observability and debugging the rulebook.
CREATE TABLE event_receipts (
    id          BIGSERIAL PRIMARY KEY,
    source_id   BIGINT      REFERENCES event_sources(id) ON DELETE SET NULL,
    payload     JSONB       NOT NULL,
    matched     INTEGER     NOT NULL DEFAULT 0,
    launched    JSONB       NOT NULL DEFAULT '[]',
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
