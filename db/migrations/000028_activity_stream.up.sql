-- Activity stream / audit log (Phase 5): an append-only record of who did what,
-- captured automatically at the API middleware layer for mutating requests.
CREATE TABLE IF NOT EXISTS activity_stream (
    id            BIGSERIAL PRIMARY KEY,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_id       BIGINT,
    username      TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL DEFAULT '',   -- create | update | delete | launch | sync | approve | deny
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id   BIGINT,
    method        TEXT NOT NULL DEFAULT '',
    path          TEXT NOT NULL DEFAULT '',
    status_code   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS activity_stream_created_idx ON activity_stream(created_at DESC);
