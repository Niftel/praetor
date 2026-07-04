---
sidebar_position: 4
title: Retention & Pruning
---

# Retention & Pruning

Job history — `job_events`, `job_output_chunks`, and their object-store log blobs — grows without bound. An **opt-in** pruner in the scheduler bounds it.

## Enabling

Set `JOB_RETENTION_DAYS` on the scheduler (default **`0` = keep everything, no deletion**):

```yaml
# docker-compose.yml (scheduler service)
environment:
  JOB_RETENTION_DAYS: "90"
```

With a positive value, the pruner deletes **terminal** jobs (`successful`/`failed`/`canceled`/`error`) finished longer ago than the window:

1. Removes their **log blobs** from the object store first.
2. Deletes the `unified_jobs` rows — `execution_runs`, `job_events`, `job_output_chunks` and the outbox **cascade**.

Active jobs are never touched. It runs **at most hourly** and is bounded to **500 jobs per pass**, so a large backlog drains over several passes rather than one huge transaction.

:::caution Deletion is permanent
Pruned job history is gone (no soft-delete). Choose a window you're comfortable losing history beyond. `0` keeps everything.
:::
