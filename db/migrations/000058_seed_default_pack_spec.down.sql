-- Revert the backfill only if the spec still matches exactly what we set (a user
-- edit or rebuild changes it, in which case we leave it alone).
UPDATE execution_packs SET spec = NULL
WHERE name = 'ansible-runtime' AND spec =
'name: ansible-runtime
python: "3.11.9"
ansible: "12.3.0"
host_runner: v0.7.2
arches:
  - arm64
';
