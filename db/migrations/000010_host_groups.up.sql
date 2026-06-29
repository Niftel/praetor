-- Create host_groups join table for host-group membership
CREATE TABLE IF NOT EXISTS host_groups (
    host_id BIGINT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (host_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_host_groups_host ON host_groups (host_id);
CREATE INDEX IF NOT EXISTS idx_host_groups_group ON host_groups (group_id);
