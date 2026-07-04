---
slug: /
sidebar_position: 1
title: Introduction & Architecture
---

# Praetor: architecture

Praetor is an Ansible automation platform (organizations, RBAC, inventories, credentials, job templates, workflows, schedules). The distinguishing design decision is **where the engine runs**: instead of executing playbooks from a central control node or execution-environment container, Praetor **bootstraps a self-contained runtime onto the target over SSH and runs the play there**. The control plane's job is to *dispatch* and *collect*, not to *execute*.

This page describes the actual infrastructure and the data path a job takes through it.

## Components

The control plane is a set of Go services over two datastores:

| Process | Kind | Responsibility |
|---|---|---|
| **api** | HTTP | REST + AuthN/Z (JWT / [PAT](./api/authentication.md)); serves the SPA's calls |
| **scheduler** | loop | Claims `pending` jobs, builds the manifest, writes the **outbox**; relays outbox→NATS; schedules, triggers, workflows, [retention](./operations/retention.md) |
| **executor** | NATS consumer | Pulls launches off the work queue; SSH-bootstraps the pack + host-runner onto the runner host |
| **host-runner** | on the target | Runs `ansible-playbook` from the pack; writes the WAL; syncs events/logs/heartbeats back |
| **ingestion** | HTTP | Endpoint the host-runner POSTs to; republishes onto the event bus; stores log blobs |
| **consumer** | NATS consumer | Durably projects events into Postgres; fires notifications |
| **reconciler** | loop | SSHes back to a host to harvest its WAL when a push never landed |
| **packbuilder** | loop | Builds [Execution Packs](./concepts/execution-packs.md) from their YAML spec via the Docker daemon; publishes them to Gitea |

**Datastores:**

- **Postgres** — all durable state (jobs, runs, projected events, RBAC, …) *and* the dispatch **outbox** (`execution_outbox`).
- **NATS JetStream** — the message bus (durable, file-backed streams) *and* the log blob store (JetStream Object Store bucket `PRAETOR_LOGS`).

Plus **LDAP** (directory), **Gitea** (artifact registry: built Execution Packs, host-runner releases, and a mirror of Python/pip so pack builds pull nothing from the public internet), and **Traefik** (reverse proxy, `*.localhost` routing, mkcert TLS).

## The messaging fabric (NATS JetStream)

Three subjects across two streams, defined in [`pkg/transport/nats/bus.go`](https://github.com/praetordev/praetor):

| Stream | Subject(s) | Retention | Purpose |
|---|---|---|---|
| `PRAETOR_REQUESTS` | `job.requests` | **WorkQueue** (removed on ack) + dedup window keyed on `execution_run_id` | job launches → executor |
| `PRAETOR_EVENTS` | `job.events`, `job.logs` | Limits (MaxAge) | run events + log-chunk refs → consumer |

Durable consumers: `praetor-executor` (work queue, queue group — load-balances across executors), `praetor-event-consumer`, `praetor-logchunk-consumer`. All file-backed, so nothing is lost across restarts.

## Dispatch: the transactional outbox

A launch must never be lost *or* double-sent, so the scheduler doesn't publish to NATS directly. In one Postgres transaction it claims the `pending` job (`FOR UPDATE SKIP LOCKED`), creates the `execution_run`, and **inserts the `ExecutionRequest` into `execution_outbox`** (`scheduler.go`). Because that's atomic, a crash can't leave a job claimed-but-unqueued.

A separate pass, `relayOutbox`, reads committed outbox rows and publishes them to `job.requests` with `MsgId = execution_run_id` — JetStream's duplicate-suppression means a retried relay can't enqueue the same launch twice. Rows are marked sent/failed with an attempt count.

The executor binds a **durable, manual-ack, work-queue** consumer (`SubscribeToExecutionRequests`). It **acks on receipt** (once the request is handed to a local worker), not on completion — a mid-run crash is recovered by the scheduler's stale-run reconciliation, *not* by redelivery, which is what prevents double-bootstrapping a host.

## Bootstrap: executor → host, over pure SSH

For a run with an inventory, the executor ([`bootstrap_runner.go`](https://github.com/praetordev/praetor)) resolves the runner host's address from its inventory vars + the job's [Machine credential](./concepts/credentials.md), dials SSH (trust-on-first-use host keys, persisted in `known_hosts`), and over that one connection:

1. `mkdir` the job dir (`/var/lib/praetor/jobs/<run_id>`) and plugin dirs.
2. Push the checkpoint callback plugin (task-level resume).
3. **Push the [Execution Pack](./concepts/execution-packs.md)** — arch-probed (`uname -m`), streamed as `<pack>-linux-<arch>.tar.gz` piped into `tar -x`. **Skipped if the pack is already present** on the host.
4. Install the host-runner from the pack to `/usr/local/bin`.
5. Push `manifest.json` (the full `ExecutionRequest`) and the job's SSH key.
6. Install a `praetor-resume` systemd unit (best-effort) for reboot recovery.
7. `setsid` the host-runner, detached, so it outlives the SSH session.

The runner host therefore needs only **sshd, a POSIX shell, and `tar`**. Python is *not* required: the play runs the pack's `ansible-playbook`, and for module execution the runner uses the host's Python if present, else the pack's bundled interpreter ([`runtime.go`](https://github.com/praetordev/praetor) `resolveAnsible`). (Musl/Alpine hosts are unsupported — the pack's CPython is glibc.)

## Execution & the write-ahead log

The host-runner runs the play and writes everything to the job dir first, then syncs — so the control plane is off the critical path:

- **`events.jsonl`** — an append-only, fsync'd WAL of job events (monotonic `seq`).
- **`stdout.log`** — raw playbook output.
- **`status.json`** — terminal state + `max_seq` (also carries the WAL format version).
- **`events.cursor` / `stdout.cursor`** — byte offsets the syncers have confirmed delivered.

Two syncers ship the WAL and the log to **ingestion** over HTTP; a heartbeat loop (~15s) posts liveness and reads back a cancel flag. Each syncer advances its cursor **only after a 2xx**, so a failed push or a restart re-delivers from the last acknowledged byte.

## Ingestion → consumer: the collection pipeline

The host-runner never touches Postgres or NATS directly — it POSTs to **ingestion** (`/api/v1/runs/{id}/events`, `/logs`, `/heartbeat`, `/facts`). Ingestion:

- **events** → `PublishJobEvent` on `job.events`. This **blocks for the JetStream persistence ack**, so the 2xx the host-runner sees means the event is *durably stored* — which is exactly what lets the syncer advance its cursor safely.
- **logs** → the raw chunk is written to the `PRAETOR_LOGS` object store, then a reference is published on `job.logs`.
- **heartbeat** → stamps `last_heartbeat_at`, returns whether the job's `cancel_requested` is set.

The **consumer** binds durable, manual-ack consumers on `job.events`/`job.logs` and projects each into Postgres: `INSERT … ON CONFLICT (execution_run_id, seq) DO NOTHING` (idempotent), updating run/job state on terminal events. On a DB error it **NAKs** the message → JetStream redelivers → a database outage is survived by replay, not by loss. Net delivery: at-least-once on the wire, **exactly-once in effect**.

Remote host-runners reach ingestion at a host-routable callback address (`HOST_RUNNER_CALLBACK_URL`, published as `:8090`), not internal Docker DNS.

## Reliability properties, and where each comes from

- **No lost/duplicated launch** — transactional outbox + JetStream dedup + work-queue ack.
- **No double-bootstrap** — executor acks-on-receipt; crashes recovered by stale-run reconciliation, not redelivery.
- **Job survives a control-plane outage** — it runs entirely on the host against the local WAL; nothing central is required for completion.
- **No lost events** — persistence-ack gates the syncer cursor; consumer NAK-replays past DB outages; projection is idempotent.
- **Recovery of un-pushed runs** — the [reconciler](./operations/resilience.md) SSHes back, reads the WAL, and projects it, advancing `persisted_event_seq` to the host's `max_seq`.
- **Reboot recovery** — the systemd unit resumes non-terminal job dirs; a [WAL format version](./operations/resilience.md) makes a resume by a mismatched binary refuse rather than misread.

## Network surface

Behind Traefik (`80`/`443`, `*.localhost`): the UI (`praetor.localhost`), API (`api.praetor.localhost`), Gitea (`gitea.localhost`), docs (`docs.localhost`). Directly published: NATS client `4222` (+ monitoring `8222`), LDAP `389`/`636`, **ingestion `8090`** (host-runner callbacks), Prometheus `9090`, Grafana `3005`. The api/ui/scheduler/etc. talk to Postgres and NATS on the internal `praetor-net`.

## Where to go next

- **[Getting Started](./getting-started.md)** — bring the stack up and run a job.
- **[Execution Packs](./concepts/execution-packs.md)** — how the pushed runtime is built and delivered.
- **[Resilience & the WAL](./operations/resilience.md)** — the durability model in depth.
- **[Observability](./operations/observability.md)** — the metrics every service exposes.
