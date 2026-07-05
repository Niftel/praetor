---
sidebar_position: 3
title: Inventories & Templates
---

# Inventories & Job Templates

## Inventories

An **inventory** is a set of hosts (with per-host connection vars like `ansible_host`, `ansible_port`, `ansible_user`) and groups. One host is flagged the **runner host** (`is_runner_host`): that's where the Execution Pack is pushed and the play runs *from*. If none is flagged, the first enabled host is used.

### Dynamic inventory sources

An inventory can have **sources** that sync hosts from a cloud/dynamic provider. A "sync now" enqueues a job that runs `ansible-inventory --list` (with any cloud credential injected) and upserts the resulting hosts/groups/memberships. Cloud credentials (AWS/Azure/GCP) are decrypted and injected for the sync.

### Fact caching

With `use_fact_cache` on, the scheduler ships an inventory's stored facts into the run; the host-runner preloads them into Ansible's jsonfile cache and posts freshly-gathered facts back. Subsequent runs skip re-gathering.

## Job templates

A **job template** ties together everything a run needs:

- **playbook** — a `playbook` path within a **source-control [project](#projects-scm)** (inline playbook content is disabled),
- **inventory** + **[credential](./credentials.md)** + **[execution pack](./execution-packs.md)**,
- **extra vars** and a default **limit**,
- prompt-on-launch flags (`ask_variables_on_launch`, `ask_limit_on_launch`) and an optional **survey**,
- `allow_simultaneous` — off by default, so an accidental double-launch is refused while a run is active.

Launching validates any prompts/survey answers, then inserts a `pending` unified job; the scheduler claims it, builds the manifest, and dispatches it to an executor.

### Ways to launch

- **Manually** (UI or `POST /api/v1/jobs`),
- **On a schedule** (rrule-based),
- **From an inbound [webhook](../api/webhooks.md)** (GitHub/GitLab/generic),
- **As a node in a [workflow](./workflows.md)**.

## Projects (SCM)

A **project** references a git repo (`scm_url`, `scm_branch`); syncing validates access and records the revision. Templates reference a project to source their playbook.

**Playbooks come only from source control.** Inline playbook content is disabled: the API rejects `playbook_content` on template create/update and requires a `project_id` + a `playbook` path, and the scheduler never dispatches inline content. This keeps every run's playbook reviewable and versioned in git rather than pasted into a template. At run time the host-runner fetches the project as a `.tar.gz` archive over HTTP (see [Execution Packs](./execution-packs.md#how-a-job-uses-one)).
