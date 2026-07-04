---
slug: /
sidebar_position: 1
title: Introduction
---

# Praetor

**Praetor is an Ansible automation platform** — think AWX/AAP (organizations, inventories, credentials, job templates, workflows, RBAC, schedules) — with one defining difference:

> **Praetor pushes a self-contained execution environment to the target and runs the playbook from there. The target needs nothing pre-installed but SSH.**

There is no pre-provisioned execution fleet, no per-host agent to maintain, and **you never `apt install ansible` on a managed host**. The engine travels with the job.

## The core idea: Execution Packs

An **Execution Pack** is a relocatable, self-contained bundle — a standalone CPython + Ansible + your collections + the Praetor **host-runner** daemon — laid out at a fixed prefix (`/opt/praetor/packs/<name>`). At run time the executor:

1. Opens a plain SSH connection to the target (using a **Machine credential** you own).
2. Streams the arch-matched pack onto the host and extracts it.
3. Launches the bundled host-runner, which runs `ansible-playbook` from the pack.

A target host only needs **sshd, a POSIX shell, `tar`, and a system `python3`** (for module execution). Delete `/opt/praetor` and nothing remains. See [Execution Packs](./concepts/execution-packs.md).

## Why it's built this way

- **No managed-node sprawl.** You don't convert hosts into agent-managed nodes; the environment is delivered per run.
- **Reproducible & air-gappable.** Packs are built from a YAML spec and pull Python/wheels from your own Gitea mirror — no PyPI or GitHub at build time.
- **Resilient by design.** The host-runner writes a durable local WAL (`events.jsonl` + `status.json`) and syncs best-effort; if the control plane is down, the job still completes and is [reconciled](./operations/resilience.md) later.

## Architecture at a glance

Praetor is a set of Go services around Postgres and NATS:

| Service | Role |
|---|---|
| **api** | REST API + AuthN/Z (JWT + [personal access tokens](./api/authentication.md)) |
| **scheduler** | Claims pending jobs, builds the job manifest, dispatches; schedules, triggers, workflows, [retention](./operations/retention.md) |
| **executor** | SSH-bootstraps the Execution Pack + host-runner onto the target |
| **host-runner** | Runs on the target: executes the play, writes the WAL, syncs events/logs |
| **ingestion** | Receives events/logs/heartbeats/facts from host-runners |
| **consumer** | Projects events into the DB (idempotent), fires notifications |
| **reconciler** | Pulls a host's WAL back when a push never landed ([resilience](./operations/resilience.md)) |
| **packbuilder** | Builds Execution Packs from their YAML spec |

Supporting infrastructure: **Postgres** (state), **NATS/JetStream** (event bus + log object store), **LDAP** (directory), **Gitea** (artifact registry for the host-runner + mirrored Python/pip), and **Traefik** (reverse proxy, `*.localhost` routing).

## Where to go next

- **[Getting Started](./getting-started.md)** — bring the stack up and run your first job.
- **[Execution Packs](./concepts/execution-packs.md)** — the differentiator, in depth.
- **[Credentials](./concepts/credentials.md)** — the AAP machine-credential model.
- **[Operations](./operations/observability.md)** — metrics, resilience, retention, cancellation.
- **[API & Authentication](./api/authentication.md)** — call Praetor from scripts/CI.
