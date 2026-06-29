# Host-Side Runner Bootstrap via Pre-Job – Specification

## 1. Overview

This document describes how Praetor should execute jobs in a way that allows them to continue running even if the central control plane (API, scheduler, DB, event bus, executor service) becomes unavailable.

The core idea:

- **For each target host in a job**, Praetor first runs a small **pre-job** (agentless, over SSH) that:
  1. Ensures a lightweight **Praetor host runner** is available on the host.
  2. Creates a per-job directory on the host and writes a **job bundle** (manifest, playbook, vars, IDs).
  3. Starts a **background service/process on the host** that owns execution of the playbook and writes a local **WAL + logs** on the host.

- Once the host runner is started, **job execution no longer depends on the central executor staying alive**. The host is the source of truth for that job and can sync events/logs back to Praetor whenever connectivity to the control plane returns.

This moves the resilience boundary from “job survives DB/event bus outage” to “job survives full control-plane / executor outage”, at the cost of running a small, bootstrapped component on the target host (or “near” it).

---

## 2. High-Level Goals

1. **Resilient host-local execution**
   - After bootstrap, each target host runs its part of the job via a local `praetor-host-runner` process.
   - The job continues and completes on the host even if:
     - The central executor dies.
     - The event bus is unavailable.
     - Postgres is unavailable.
     - The API/scheduler/consumer are down.

2. **Host-local WAL (Write-Ahead Log)**
   - Each target host writes an append-only WAL of execution events and stdout, stored on the host’s filesystem.
   - This WAL is the **primary source of truth** during outages.

3. **Eventual synchronization with the control plane**
   - When Praetor’s control plane is available, it can:
     - Discover in-flight or finished host-side jobs.
     - Read or receive their WAL and logs.
     - Reconstruct job history and final state in Postgres (`unified_job`, `execution_run`, `job_event`, `job_output_chunk`, etc.).

4. **Agentless bootstrap**
   - Initial setup uses **agentless SSH/WinRM** from Praetor to:
     - Install or update the host runner binary/script.
     - Create the per-job directory.
     - Start the host-level service.
   - After bootstrap, the host runner behaves like an agent for that job, but it is **bootstrapped via Ansible-style pre-jobs**.

---

## 3. Host-Side Layout and Contract

### 3.1 Per-Job Directory Layout

On each target host, for each `execution_run`, the host runner uses a directory such as:

```text
/var/lib/praetor/jobs/<execution_run_id>/
```

This directory contains at least:

- `manifest.json`  
  Job manifest for this host. Includes:
  - `execution_run_id` (UUID)
  - `unified_job_id` (numeric or string)
  - Playbook reference (path, inline content, or git revision + path)
  - Inventory subset (for this host or host group)
  - Extra vars / environment
  - Any execution parameters (timeouts, forks, etc.)

- `events.jsonl`  
  Append-only **WAL** of execution events. One JSON object per line.

- `stdout.log`  
  Raw stdout/stderr from the run (optional but recommended).

- `status.json`  
  Final status and summary of the run.

The directory MUST be created with permissions that only the host runner and appropriate system users can read/write (e.g. owned by `praetor` user).

### 3.2 `status.json` Schema

Example `status.json` content:

```json
{
  "execution_run_id": "uuid-of-run",
  "unified_job_id": 123,
  "state": "successful",
  "rc": 0,
  "max_seq": 120,
  "completed_at": "2025-12-10T09:30:00Z"
}
```

Fields:

- `execution_run_id` (string, UUID) – unique ID of this run.
- `unified_job_id` (string or integer) – logical job ID.
- `state` (string) – final state of this host-local run:
  - `pending`
  - `running`
  - `successful`
  - `failed`
  - `canceled`
  - `lost`
- `rc` (integer) – process return code of the host runner / Ansible invocation.
- `max_seq` (integer) – highest sequence number written to `events.jsonl`.
- `completed_at` (timestamp) – RFC3339/ISO8601 completion time.

### 3.3 `events.jsonl` Schema (Host WAL)

Each line in `events.jsonl` is a JSON object, e.g.:

```json
{
  "execution_run_id": "uuid-of-run",
  "unified_job_id": 123,
  "seq": 42,
  "ts": "2025-12-10T09:00:01.234Z",
  "kind": "TASK_OK",
  "host": "web-01.example.com",
  "task_name": "Install nginx",
  "play_name": "Configure web servers",
  "message": "changed: [web-01.example.com]",
  "payload": {
    "changed": true,
    "duration": 1.234,
    "raw_event": { "ansible_facts": {}, "...": "..." }
  }
}
```

Fields:

- `execution_run_id` (string) – run ID, consistent with `status.json` and `manifest.json`.
- `unified_job_id` (string or integer) – logical job ID.
- `seq` (integer) – monotonically increasing per run, starting at 1.
- `ts` (string) – event timestamp, RFC3339/ISO8601.
- `kind` (string) – event type, e.g.:
  - `JOB_STARTED`
  - `TASK_OK`
  - `TASK_FAILED`
  - `TASK_SKIPPED`
  - `TASK_UNREACHABLE`
  - `HEARTBEAT`
  - `JOB_COMPLETED`
  - `JOB_FAILED`
  - `JOB_CANCELED`
- `host` (string) – the host the event pertains to (usually “this” host).
- `task_name` (string, optional) – Ansible task/step name.
- `play_name` (string, optional) – Ansible play name.
- `message` (string) – short human-readable summary.
- `payload` (object) – raw event data for debugging and detailed projections.

**Invariants:**

- `seq` MUST be strictly increasing per `execution_run_id` as events are appended.
- The file is **append-only**; no in-place edits.
- `status.json.max_seq` MUST match the highest `seq` written when the job completes.

### 3.4 `praetor-host-runner` Responsibilities

A host-local binary/script `praetor-host-runner` is responsible for:

1. Reading `manifest.json` from the job directory.
2. Executing the job locally on the host (e.g. via Ansible, ansible-pull, or a similar runner).
3. Writing WAL and logs:
   - Appending events to `events.jsonl` as Ansible produces them.
   - Appending stdout/stderr to `stdout.log`.
   - Periodically writing a heartbeat event (`kind = "HEARTBEAT"`) if desired.
4. On completion:
   - Writing `status.json` with final `state`, `rc`, `max_seq`, and `completed_at`.
   - Exiting with the job’s return code.

The host runner **MUST NOT** depend on:

- Direct connectivity to Praetor’s Postgres.
- Direct connectivity to Praetor’s event bus.

Connectivity to Praetor is **best-effort** and only used for syncing events/logs when available.

---

## 4. Pre-Job Bootstrap Flow (Central Executor Side)

### 4.1 Purpose

The pre-job is an **agentless, per-host setup** step that prepares the host to run its portion of the job independently.

### 4.2 Steps

For each target host and `execution_run_id`, the central executor performs:

1. **Connect over SSH/WinRM**
   - Use existing credential and inventory mechanisms.

2. **Ensure host runner installed**
   - Check for `/usr/local/bin/praetor-host-runner` (or configured path).
   - If missing or outdated, upload the new binary/script.
   - Ensure correct ownership and permissions.

3. **Create per-job directory**
   - Create `/var/lib/praetor/jobs/<execution_run_id>/`.
   - Ensure proper permissions (e.g. owned by `praetor` user).

4. **Upload job bundle**
   - Write `manifest.json` to the job directory.
   - Optionally write additional data (bundled playbook, vars, inventory slice).

5. **Start host runner as background service**
   - Example systemd invocation:

     ```bash
     systemd-run --unit=praetor-job-<execution_run_id>        /usr/local/bin/praetor-host-runner        --job-dir=/var/lib/praetor/jobs/<execution_run_id>
     ```

   - Or simple nohup:

     ```bash
     nohup /usr/local/bin/praetor-host-runner        --job-dir=/var/lib/praetor/jobs/<execution_run_id>        >/var/lib/praetor/jobs/<execution_run_id>/runner.log 2>&1 &
     ```

6. **Return control to Praetor**
   - The pre-job does **not** wait for the host runner to finish.
   - The central executor records that the host-side run has been started and continues with other work.

### 4.3 Idempotency Requirements

- Running the pre-job multiple times for the same `(host, execution_run_id)` MUST NOT break the run.
- Re-running may:
  - Confirm the runner binary is already installed.
  - Confirm the job directory exists.
  - Optionally check whether a service for that job is already active and skip restarting if so.

---

## 5. Sync and Reconciliation

### 5.1 Modes of Synchronization

There are two main patterns for synchronizing host WAL/logs back to Praetor:

1. **Push-based (host runner pushes)**
   - The host runner (or a sidecar process) periodically:
     - Sends new events from `events.jsonl` to a Praetor endpoint (HTTP/gRPC) or event bus.
     - Sends new log chunks from `stdout.log` to object storage / Praetor log service.

2. **Pull-based (control plane pulls)**
   - A Praetor reconciler periodically:
     - Connects to the host (SSH or API).
     - Reads `status.json` and `events.jsonl` (and optionally `stdout.log` or log chunks).
     - Applies them to Postgres and object storage.

The system can support either or both.

### 5.2 DB Projection and Idempotency

On the control-plane side, synchronization must:

1. Load `status.json` and `events.jsonl` for a given `execution_run_id`.
2. For each event where `seq > persisted_event_seq`:
   - Insert a row into `job_event` with `(execution_run_id, seq)` as a **unique key**.
   - Use `ON CONFLICT (execution_run_id, seq) DO NOTHING` (or equivalent) to ensure idempotency.
3. Optionally create or update `job_output_chunk` rows to point to stored stdout/log chunks.
4. Update `execution_run` and `unified_job`:
   - Set `execution_run.persisted_event_seq = status.max_seq` once all events are ingested.
   - Set `execution_run.state` and `unified_job.status` to `status.state`.
   - Set `finished_at` based on `completed_at` if present.

This allows safe retry of the sync logic at any time.

### 5.3 Failure Cases

- **Control plane is down while host runner is active**
  - Host runner continues running and writing WAL.
  - No events/logs are synced until control plane is reachable.
- **Host is unreachable during sync**
  - Reconciler marks the sync as pending and retries later.
- **Host reboots and loses `/var/lib/praetor/jobs/<execution_run_id>/`**
  - WAL/logs are lost.
  - Praetor should mark `execution_run.state = 'lost'` and `unified_job.status = 'error'` or a specific error state.
- **Partial sync**
  - If sync stops mid-way, `persisted_event_seq` < `status.max_seq`.
  - Re-running sync will continue from where it left off.

---

## 6. Execution and Failure Semantics

### 6.1 Guarantees

With this design:

- Once the host runner is started:
  - The job’s progress and outcome on that host depend only on:
    - The host runner process.
    - The host’s local filesystem.
    - Connectivity from host to its target systems (which may be itself).
  - The central Praetor control plane, its DB, and its event bus are **not** required for job completion.
- As long as:
  - The host runner and `/var/lib/praetor/jobs/<execution_run_id>/` survive,
  - Praetor can always reconstruct what happened via WAL replay.

### 6.2 Non-Guarantees

- If the host itself dies or loses its job directory:
  - The job and its logs cannot be recovered (without additional replication).
- This model **adds a host-side component** and therefore is not “purely agentless” anymore, though it can be:
  - Bootstrapped via agentless SSH pre-jobs.
  - Managed/upgraded in a controlled and minimal way.

---

## 7. Acceptance Criteria

A minimal implementation is considered successful when:

1. **Bootstrap & Execution**
   - I can start a job that targets one or more hosts.
   - For each host, the pre-job:
     - Installs/ensures `praetor-host-runner` is present.
     - Creates `/var/lib/praetor/jobs/<execution_run_id>/`.
     - Writes a valid `manifest.json`.
     - Starts the host runner in the background.

2. **Control-plane outage**
   - While the host runner is executing the job, I can simulate a full control-plane outage:
     - Stop the executor service.
     - Stop the API/scheduler.
     - Stop the event consumer and disconnect from DB/event bus.
   - The host runner continues running the job to completion and writes:
     - A complete `events.jsonl` with monotonically increasing `seq`.
     - A `status.json` with the correct final state and `max_seq`.
     - A `stdout.log` with the job’s terminal output.

3. **Sync after outage**
   - When I bring the control plane back up and run the sync/reconciliation logic:
     - `job_event` is populated from `events.jsonl`.
     - `execution_run.persisted_event_seq` matches `status.max_seq`.
     - `execution_run.state` and `unified_job.status` match `status.state`.
     - Logs are accessible via Praetor (directly or via `job_output_chunk` + object storage).

4. **Idempotent sync**
   - If I run the sync twice in a row:
     - No duplicate events are created.
     - Job state does not regress or get corrupted.

5. **Idempotent pre-job**
   - Running the pre-job multiple times for the same host and `execution_run_id`:
     - Does not break the existing job.
     - Does not start multiple competing host runners for the same job.
     - Is safe to do for recovery/repair scenarios.
