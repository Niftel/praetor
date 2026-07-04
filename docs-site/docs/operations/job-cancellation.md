---
sidebar_position: 3
title: Job Cancellation
---

# Job Cancellation

Because a job runs as a detached host-runner on the target, the control plane holds no process handle — so cancellation is **cooperative**, over the existing heartbeat channel.

## Cancelling

- **UI:** a **Cancel** button on any active job.
- **API:** `POST /api/v1/jobs/{id}/cancel`.

What happens depends on state:

- **running** → the job is flagged `cancel_requested` (API returns `canceling`). On its next heartbeat the host-runner sees the flag, cancels the play's context (SIGTERM to the whole `ansible-playbook` process group, escalating to SIGKILL), and reports **`JOB_CANCELED`** — which the consumer projects to a terminal `canceled` state.
- **pending / queued** (not executing yet) → canceled outright (API returns `canceled`), and the scheduler skips claiming `cancel_requested` jobs.
- **already finished** → `409 Conflict`.

Cancel latency is bounded by the heartbeat interval (~15s).

:::note Requires host-runner ≥ v0.4.0
The cancel logic lives in the host-runner, so a pack must bundle **v0.4.0 or newer**. See [Host-runner releases](./host-runner-releases.md).
:::
