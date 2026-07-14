-- The default 'ansible-runtime' pack was seeded (000032) with no spec, so the
-- registry UI had no bill of materials to show — expanding it revealed nothing.
-- Backfill the canonical spec from build/execpack/specs/default.yml so a pack's
-- real contents (standalone CPython, the Ansible engine, the bundled host-runner
-- daemon, target arch) are queryable and rendered. Only when spec IS NULL, so a
-- user-supplied/rebuilt spec is never clobbered.
UPDATE execution_packs SET spec =
'name: ansible-runtime
python: "3.11.9"
ansible: "12.3.0"
host_runner: v0.7.2
arches:
  - arm64
'
WHERE name = 'ansible-runtime' AND spec IS NULL;
