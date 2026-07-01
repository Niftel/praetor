-- Consolidate host-group membership onto a single table.
--
-- Membership was historically split across two tables that different services
-- read and wrote: the API/UI and inventory import wrote host_groups, while
-- dynamic inventory sync wrote host_group_mapping. Because the scheduler read
-- only one of them, hand-built inventories could render with empty groups.
-- host_groups is the canonical table (newer, has created_at, used by the API,
-- import and membership listing), so fold any mapping-only rows into it and drop
-- the duplicate. Application code now reads and writes host_groups exclusively.
INSERT INTO host_groups (host_id, group_id)
SELECT host_id, group_id FROM host_group_mapping
ON CONFLICT (host_id, group_id) DO NOTHING;

DROP TABLE IF EXISTS host_group_mapping;
