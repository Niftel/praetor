---
slug: /
sidebar_position: 1
title: Introduction
---

# Praetor

Praetor is an Ansible automation platform. On the surface it's AWX/AAP-shaped — organizations, RBAC, inventories, credentials, job templates, workflows, schedules, surveys, webhooks, notifications, an audit stream. What's different is *where and how the automation actually runs*, and that one decision changes the operational model enough to be worth understanding before anything else.

## The problem it's reacting to

Every Ansible-at-scale tool has to answer one question: **where does the engine run, and what does it require of the targets and the control plane?** The common answers each have a cost:

- **Central control node (plain `ansible` / `ansible-pull` shops).** The box running Ansible must carry the *entire* world — the right Ansible version, every collection, every Python dependency — and every play streams over SSH from that one place. It becomes a bottleneck and a version monolith: one dependency set for everyone.
- **AWX / AAP with Execution Environments.** Better isolated (the engine lives in a container image), but you're now building and distributing EE images, running a standing execution fleet, and — critically — **the control plane and its message bus must stay up and reachable for the duration of a job.** If the controller or the network to it drops mid-run, the job is lost.

Both models keep the engine *at the center* and reach outward. Praetor inverts that.

## The Praetor model: push the engine to the edge

> **Praetor delivers a complete, self-contained execution environment onto the target over SSH, and runs the playbook there — locally, on the host itself.**

At launch, the executor opens a plain SSH connection (using a [Machine credential](./concepts/credentials.md) you own), streams an [**Execution Pack**](./concepts/execution-packs.md) onto the host, and starts a small **host-runner** that executes `ansible-playbook` from the pack. The pack is a relocatable bundle — a standalone CPython, Ansible, your collections and pip deps, and the host-runner daemon — laid out under `/opt/praetor`.

The consequences of that flip are the whole point:

- **The target needs almost nothing.** Just `sshd`, a POSIX shell, `tar`, and a system `python3` (for module execution). No pre-installed Ansible, no standing agent, no managed-node onboarding. Delete `/opt/praetor` and the host is exactly as it was.
- **The engine is per-job and versioned, not central.** Different templates can carry different packs (different Ansible/collection sets) with no shared, drifting dependency monolith. Packs are built from a YAML spec and are reproducible.
- **The control plane stops being on the critical path** for a running job (see [Reliability](#reliability-the-job-outlives-the-control-plane) below).

## The nuance: "agentless" is the wrong word

Praetor is *not* purely agentless — it puts a process (the host-runner) on the target. But it's also not an agent platform in the AWX/Salt sense, because **nothing is pre-installed or kept running between jobs.** The runner is bootstrapped fresh, per run, over ordinary SSH, and it's part of the pushed pack rather than something you provision and patch on a fleet.

So the honest framing is a deliberate middle ground: **agentless to deploy, agent-like at runtime.** You get the zero-onboarding of agentless SSH *and* the local-execution resilience of an on-host agent, without maintaining either a central execution fleet or a permanent per-host agent. The trade-off — stated plainly — is that a job does run a Praetor binary on the host for its duration, and that binary (via the pack) is glibc-only, covering mainstream Linux but not musl/Alpine targets.

## Reliability: the job outlives the control plane

Because the play runs on the host, Praetor is built so **the control plane can be unreliable without losing work.** The host-runner writes a durable, append-only **write-ahead log** (`events.jsonl`) plus `stdout.log` and a terminal `status.json` into the job directory, and *then* best-effort syncs them back:

- If the control plane or network is down, the job **keeps running to completion** and records everything locally.
- Event/log syncers advance their cursor only after receipt is confirmed, so a restart re-delivers from the last acknowledged point; projection is idempotent (`ON CONFLICT (run, seq)`). At-least-once on the wire, exactly-once in effect.
- If a push never lands, a **pull-based reconciler** later SSHes back to the host, reads the WAL, and recovers the run to its *true* outcome — instead of falsely marking a job that actually succeeded as failed.
- After a host reboot, a systemd unit resumes any interrupted job from its on-disk state.

This is a genuinely different posture from a controller-centric design, where losing the controller mid-run loses the run. See [Resilience & the WAL](./operations/resilience.md).

## What stays familiar

None of the above costs you the platform surface you expect:

- **Organizations, teams, users, RBAC** (object-scoped roles), backed by an **LDAP** directory, with an **activity/audit stream**.
- **Inventories** (static + dynamic cloud sources, fact caching) and **[job templates](./concepts/inventories-and-templates.md)** with extra-vars, limits, prompts, and **surveys**.
- **[Credentials](./concepts/credentials.md)** on the AAP machine-credential model (encrypted at rest, injected at run time; no shared platform key).
- **[Workflows](./concepts/workflows.md)** — DAGs of templates with success/failure/always edges and approval gates.
- **Triggers** — schedules (rrule), inbound [webhooks](./api/webhooks.md), and a REST API you drive with [personal access tokens](./api/authentication.md).
- **Operations** — Prometheus [metrics](./operations/observability.md) on every service, opt-in [retention pruning](./operations/retention.md), and cooperative [job cancellation](./operations/job-cancellation.md).

## Life of a job

Concretely, one run flows like this:

1. **Launch** (UI, API, schedule, webhook, or a workflow node) inserts a `pending` job.
2. The **scheduler** claims it and builds a *manifest* — playbook, inventory, resolved credential injectors, which pack, extra vars/limit — snapshotting what the run needs.
3. The **executor** SSHes to the inventory's runner host, streams + extracts the Execution Pack (skipped if already present), installs and launches the **host-runner**.
4. The host-runner runs `ansible-playbook` from the pack against the inventory, writing the WAL and streaming events/logs/heartbeats back to **ingestion**.
5. The **consumer** idempotently projects those events into Postgres and fires notifications; the UI shows live status + streamed output.
6. On finish it writes a terminal `status.json`; if anything got lost in transit, the **reconciler** harvests it from the host afterward.

Along the way you can **cancel** (the runner learns via its heartbeat and stops the play), and every service exposes metrics for what's happening.

## Architecture at a glance

Go services around Postgres and NATS:

| Service | Role |
|---|---|
| **api** | REST API + AuthN/Z (JWT + [PATs](./api/authentication.md)) |
| **scheduler** | Claims jobs, builds manifests, dispatches; schedules, triggers, workflows, [retention](./operations/retention.md) |
| **executor** | SSH-bootstraps the pack + host-runner onto the target |
| **host-runner** | Runs on the target: executes the play, writes the WAL, syncs back |
| **ingestion** | Receives events/logs/heartbeats/facts |
| **consumer** | Idempotently projects events; fires notifications |
| **reconciler** | Pulls a host's WAL back when a push never landed |
| **packbuilder** | Builds Execution Packs from their YAML spec |

Supporting infrastructure: **Postgres** (state), **NATS/JetStream** (event bus + log object store), **LDAP** (directory), **Gitea** (artifact registry for the host-runner + a mirror of Python/pip so pack builds need nothing from the public internet), and **Traefik** (reverse proxy, `*.localhost` routing).

## Where to go next

- **[Getting Started](./getting-started.md)** — bring the stack up and run your first job.
- **[Execution Packs](./concepts/execution-packs.md)** — the pushed runtime, in depth.
- **[Resilience & the WAL](./operations/resilience.md)** — how a job survives an unreliable control plane.
- **[Credentials](./concepts/credentials.md)** — the machine-credential model.
- **[API & Authentication](./api/authentication.md)** — drive Praetor from scripts/CI.
