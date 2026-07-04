---
sidebar_position: 2
title: Resilience & the WAL
---

# Resilience & the WAL

Because the job runs on the target — not on the control plane — Praetor is built so **the control plane can be unreliable without losing a job**.

## The host-side WAL

Each run's host-runner writes durable state into `/var/lib/praetor/jobs/<run_id>/`:

- **`events.jsonl`** — an append-only write-ahead log of job events (monotonic `seq`),
- **`stdout.log`** — the raw playbook output,
- **`status.json`** — the terminal state + `max_seq`,
- byte **cursors** for the event/log syncers, and a resume `checkpoint.json`.

The syncers ship events/logs to the control plane and advance their cursor **only after receipt is confirmed**, so a failed push or a restart re-delivers from the last acknowledged position — and the consumer's `ON CONFLICT (execution_run_id, seq)` makes projection idempotent. Net effect: **exactly-once** in effect, at-least-once on the wire.

## Liveness & the two reconcilers

The host-runner heartbeats every ~15s (`execution_runs.last_heartbeat_at`). Two mechanisms use it:

1. **Heartbeat-timeout (scheduler):** a run whose heartbeat goes stale is either handed to the pull reconciler (remote) or, for a local run with no SSH path back, declared `lost`.
2. **Pull-based reconciler (service):** for a run stuck in `reconciling`, the reconciler SSHes back to the host, reads `status.json` + `events.jsonl` + `stdout.log`, and **re-feeds them through the same ingestion endpoints a push uses**. It advances `persisted_event_seq`, finalizes the true terminal state, keeps monitoring a still-running host, and only declares `lost` when the host is truly gone or unreachable past a backoff.

So a job that **succeeded on the host but never pushed** (control plane was down, or the host was unreachable at sync time) is **recovered to its real outcome** instead of being falsely failed.

## Boot-time resume

After a host reboot, a systemd unit runs the host-runner with `--resume-root=/var/lib/praetor/jobs`; it re-runs any non-terminal job dir from its on-disk state. Fresh runs and resumed runs are identical — all durable state is in the directory.

## WAL format versioning

The job-dir format has a version (`walFormat`, currently **1**), stamped into `runner-meta.json` and `status.json`.

- **Additive changes** (new fields, new `event_type` values) do **not** bump it — readers ignore unknown fields and the consumer no-ops on unknown events.
- **Structural/breaking** changes bump it. A host-runner understands every format `≤ walFormat` and **refuses to resume a job dir written by a newer format** (a downgrade / mixed-fleet mistake), deferring it to the reconciler rather than misreading it.

You therefore don't need a "flush before upgrade" step: the ack-cursor + final flush + reconciler guarantee delivery, and the version guard makes format changes safe.
