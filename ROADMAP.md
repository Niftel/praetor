# Praetor Roadmap — Missing Features

Status: planning. This roadmap covers the gap between Praetor today and a credible
AWX/Ansible Automation Platform alternative. It is ordered by **value per effort**
and by **dependency**, not by how AWX ships things.

## Guiding principles

Every item is evaluated against Praetor's actual architecture, so we don't graft
concepts that don't fit (the way Execution Environments and the Instances tab
didn't):

- **Agentless execution.** Jobs run as a host-runner bootstrapped over SSH onto a
  runner host, executing `ansible-playbook` natively. There is no persistent
  execution-node fleet and no per-job container. Features must work in that model.
- **Event-driven core.** A durable JetStream pipeline already carries job events
  (started/stdout/completed) with idempotent projection. New eventing features
  (notifications, webhooks, activity stream) should ride this, not bolt on.
- **The RBAC model already reserves roles** for several of these features
  (`workflow_admin_role`, `notification_admin_role`, `approval_role` in
  `db/migrations/000011_awx_style_rbac.up.sql`) — the roles exist; the features do not.

Effort scale: **S** ≈ a few days · **M** ≈ 1–2 weeks · **L** ≈ 3+ weeks (one engineer).

---

## Phase 0 — Foundation cleanup (S)

Stop the data model implying features that don't exist; unblock later phases.

- **Prune vestigial infra.** `instances` / `instance_groups` are no longer surfaced
  (Infra tab removed); either drop executor self-registration + the tables, or keep
  a single read-only "is the control plane alive" health check. Remove the unused
  `execution_environments` table.
- **Close open TODOs.** Executor failure-event publishing (`cmd/executor/main.go:80`)
  and the unimplemented ingestion HTTP publisher (`services/executor/core/http_publisher.go:59`).
- **Why first:** small, removes confusion, and the failure-event TODO matters for
  notifications (Phase 2).

---

## Phase 1 — Launch-time inputs (M)

The cheapest high-use capability: let a launch supply data instead of being fully
predefined. Surveys are a structured form of prompt-on-launch, so build them together.

### 1a. Prompt-on-launch
- **What:** `ask_variables_on_launch`, `ask_inventory_on_launch`, `ask_credential_on_launch`,
  `ask_limit_on_launch`, etc. — template fields that let the launcher override at run time.
- **Data model:** boolean `ask_*` columns on `job_templates`; overrides already flow
  via `unified_jobs.job_args` (JSONB) — wire them into the manifest the scheduler builds.
- **API/UI:** launch dialog reads the `ask_*` flags and collects overrides; validate
  the launcher may use the chosen inventory/credential (RBAC).
- **Execution:** scheduler merges launch overrides into the manifest (inventory,
  limit, extra_vars, credential). No host-runner change.
- **Effort:** S. **Deps:** none.

### 1b. Surveys
- **What:** a named, ordered set of typed questions (text, password, choice, integer,
  multiselect) that render as a form at launch and become extra_vars.
- **Data model:** `survey_specs` (template_id, JSON spec) — AWX stores the spec as JSON;
  follow that.
- **API/UI:** survey builder on the template; survey form on launch; answers merged
  into extra_vars with `no_log` honored for password questions.
- **Effort:** M. **Deps:** 1a (shares the launch-override plumbing).

---

## Phase 2 — Eventing & integrations (M)

Table-stakes operational features that ride the existing event pipeline.

### 2a. Notifications
- **What:** notify on job start/success/failure via Slack, email (SMTP), and generic
  webhook. `notification_admin_role` is already reserved.
- **Data model:** `notification_templates` (org-scoped, type, encrypted config) and a
  join from `job_templates`/orgs to templates per event (started/success/error).
- **Execution:** a consumer subscribes to terminal `JOB_*` events on JetStream and
  dispatches — no change to the host-runner or scheduler. Reuse `pkg/crypto` for secrets.
- **Effort:** M. **Deps:** Phase 0 failure-event TODO (so failures actually emit).
- **Architecture note:** strong fit — this is exactly what the durable event bus is for.

### 2b. Inbound webhooks
- **What:** GitHub/GitLab push (and generic) webhooks that launch a template, with
  shared-secret (HMAC) verification.
- **Data model:** per-template webhook key + provider; map payload → extra_vars
  (branch, commit, etc.).
- **API:** public `POST /api/v1/webhooks/{provider}/{token}` that verifies signature
  and enqueues a `unified_jobs` row (the existing launch path).
- **Effort:** S–M. **Deps:** none (reuses launch). Pairs naturally with project SCM.

---

## Phase 3 — Inventory & content depth (L)

### 3a. Dynamic inventory sources
- **What:** sync hosts/groups from cloud sources (aws_ec2, azure_rm, gcp_compute,
  openstack, vmware, …) and from SCM-hosted inventory files, on a schedule.
- **Data model:** `inventory_sources` (inventory_id, source type, credential_id,
  source_vars, update_on_launch, schedule); `inventory_updates` as run records.
- **Execution:** an **inventory sync is itself a host-runner job** — run
  `ansible-inventory -i <plugin config> --list` on a runner host with the cloud
  credential injected, then upsert hosts/groups into the inventory. This reuses the
  whole bootstrap/credential/event machinery instead of inventing a new path.
- **Effort:** L (one source type is M; each additional is S). **Deps:** credential
  injectors for cloud creds (extend `credential_types`).
- **Architecture note:** good fit precisely because we model sync as a normal job.

### 3b. Fact caching
- **What:** persist `ansible_facts` per host after a gather, and make them available to
  later runs (Ansible fact cache).
- **Data model:** `host_facts` (host_id, facts JSONB, modified). Host-runner writes a
  jsonfile fact cache; on completion the syncer ships it; the API serves it back as the
  cache for the next run.
- **Effort:** M. **Deps:** none, but rides the existing artifact-sync pattern (like logs).

### 3c. Native collection caching + lockfile (the "collections like AAP" item)
- **What:** capture the *value* of AAP Execution Environments — reproducibility, speed,
  offline — **without containers**, which don't fit the agentless model.
- **Mechanism:** generate a `collections/requirements.lock` (name + version + sha) per
  project; the host-runner installs into a content-addressed cache on the runner host
  (`/var/lib/praetor/collections-cache/<sha>/`) and reuses/verifies it instead of
  re-downloading every run.
- **Effort:** M. **Deps:** builds directly on the Galaxy requirements work already done.
- **Architecture note:** the Praetor-native answer to EEs. (A real Automation-Hub *host*
  — serving collections — is a separate, larger effort and only worth it for air-gapped
  governance.)

---

## Phase 4 — Workflows (L)

The headline AWX feature and the biggest build. `workflow_admin_role` / `approval_role`
are already reserved.

- **What:** a DAG of nodes (job templates, project syncs, inventory updates, approval
  nodes) connected by **success / failure / always** edges; the workflow itself is a
  unified job template, so it schedules, prompts, and reports like any job.
- **Data model:** `workflow_job_templates`, `workflow_job_template_nodes` (+ edge type),
  and runtime `workflow_jobs` / `workflow_job_nodes`. Slots into the existing
  `unified_job_templates` / `unified_jobs` polymorphism.
- **Execution:** a **workflow runner** in the scheduler walks the DAG, launching child
  `unified_jobs` as nodes become eligible and pausing on **approval nodes** until a user
  approves/denies via the API. No host-runner change — nodes are ordinary jobs.
- **Effort:** L. **Deps:** Phase 1 (node prompts), Phase 2 (workflow notifications),
  ideally Phase 3a (inventory-update nodes).

---

## Phase 5 — Governance & hardening (M, ongoing)

- **Activity stream / audit log** — append-only record of who changed/launched what.
  Capture at the API middleware layer; surface a filterable view. Required for any
  serious multi-tenant deployment.
- **Secrets management** — replace the hardcoded `PRAETOR_SECRET_KEY` / JWT defaults with
  required, rotatable secrets; optional external KMS/Vault backing for credentials.
- **RBAC enforcement on new resources** — every resource above must honor the object-role
  model from day one (the roles are already reserved for the big ones).
- **Operational polish** — auto-install the host-runner resume systemd unit during
  bootstrap; broaden test coverage on new surfaces.

---

## Summary

| Feature | Phase | Value | Effort | Key dependency |
|---|---|---|---|---|
| Vestigial cleanup + TODOs | 0 | Foundation | S | — |
| Prompt-on-launch | 1 | High | S | — |
| Surveys | 1 | High | M | Prompt-on-launch |
| Notifications | 2 | High | M | Failure events (P0) |
| Inbound webhooks | 2 | Med–High | S–M | — |
| Dynamic inventory sources | 3 | High | L | Cloud credential injectors |
| Fact caching | 3 | Med | M | — |
| Native collections cache + lockfile | 3 | Med–High | M | Galaxy reqs (done) |
| Workflows (+ approvals) | 4 | Very High | L | P1, P2, P3a |
| Activity stream / audit | 5 | High | M | — |
| Secrets / RBAC / ops hardening | 5 | High | M | — |

### Recommended sequence
**0 → 1 → 2 → 3 → 4 → 5.** Each phase ships independently usable value, and the order
respects dependencies: launch inputs and notifications make workflows worth building, and
dynamic inventory adds a high-value workflow node. Hardening (Phase 5) runs continuously
alongside the rest, not only at the end.

### What we deliberately are NOT building
- **Execution Environments (containers)** — collections are addressed natively (3c);
  containerized execution would require abandoning the agentless model.
- **A persistent execution-node fleet / Instance Groups** — Praetor bootstraps on demand;
  there is nothing to manage. (This is why the Infrastructure tab was removed.)
