---
sidebar_position: 5
title: Host-runner Releases
---

# Host-runner Releases

The **host-runner** daemon ships *inside* the [Execution Pack](../concepts/execution-packs.md), not pushed separately. Deploying a new host-runner version is therefore a two-step release → pack-rebuild.

## Releasing a new version

```bash
# 1. Build + publish the daemon (amd64 + arm64) to Gitea (needs GITEA_TOKEN)
make release-host-runner VERSION=0.4.0

# 2. Bump the pack's spec and rebuild it (the packbuilder pulls the release
#    from Gitea and bakes it into the pack)
#    e.g. set `host_runner: v0.4.0` in the pack spec, then re-queue the build.
```

The packbuilder rebuilds each arch (amd64 cross-builds under qemu) and flips the pack `ready`.

## How it reaches hosts (and why it's safe)

- **Skip-if-present:** the executor skips the whole pack push when the pack already exists on a host. So a new daemon version is **not** auto-churned across the fleet — it lands on a host only at the **next job** after the pack is rebuilt, and **never mid-run** (the daemon is per-run and already in memory).
- **No pre-upgrade drain needed:** the [WAL ack-cursor, final flush, and reconciler](./resilience.md) guarantee delivery, and the WAL **format version** makes a resume by a mismatched binary safe (it refuses a newer format).

To force a specific host to pick up a new pack in testing, remove it there (`rm -rf /opt/praetor/packs/<pack>`) so the next job re-pushes. Note that recreating a target rotates its SSH host key, so clear the executor's `~/.ssh/known_hosts` (trust-on-first-use) and restart the executor.

## Current baseline

Host-runner **v0.4.0** adds job cancellation and WAL format versioning. Packs should bundle `host_runner: v0.4.0` (or newer) for [cancellation](./job-cancellation.md) to work.
