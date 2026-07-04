---
sidebar_position: 4
title: Workflows
---

# Workflows

A **workflow** chains job templates into a **DAG**. Nodes are job templates (or approval / webhook nodes); edges fire on **success**, **failure**, or **always**, so you can branch on outcomes.

## How a run executes

Launching a workflow snapshots its nodes **and** edges into the run, so editing the template later doesn't change an in-flight run. The scheduler's workflow runner then walks the DAG each tick:

- launches a ready node's job template as an ordinary unified job,
- follows success/failure/always edges as nodes finish,
- **pauses at an approval gate** until someone approves/denies (via the API/UI),
- skips branches whose condition wasn't met, and finalizes when the DAG is done.

The run view shows the graph with each node colored by live status, plus Approve/Deny on gates awaiting approval.

## Concurrency

Like job templates, a workflow has `allow_simultaneous` (**off by default**): a launch is refused while a prior run of the same workflow is still active, so an accidental re-trigger can't start an overlapping run. Set it on the workflow to allow parallel runs.

## Triggers

A workflow can be launched manually, on a schedule, or from an inbound [webhook](../api/webhooks.md) (`/api/v1/webhooks/workflow-templates/{id}/{service}`). Webhook `webhook_out` nodes let a workflow call external systems mid-DAG.
