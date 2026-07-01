-- Recreate the mapping table and repopulate it from the consolidated table so a
-- rollback restores the pre-consolidation shape (both tables present and in sync).
CREATE TABLE IF NOT EXISTS host_group_mapping (
    host_id BIGINT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    PRIMARY KEY (host_id, group_id)
);
CREATE INDEX IF NOT EXISTS idx_host_group_host ON host_group_mapping(host_id);
CREATE INDEX IF NOT EXISTS idx_host_group_group ON host_group_mapping(group_id);

INSERT INTO host_group_mapping (host_id, group_id)
SELECT host_id, group_id FROM host_groups
ON CONFLICT (host_id, group_id) DO NOTHING;
