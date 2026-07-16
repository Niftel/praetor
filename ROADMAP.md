# Praetor Roadmap

Status: active development.

This document records what Praetor has shipped, what is currently committed, and
which ideas are intentionally demand-gated. GitHub issues contain implementation
scope and acceptance criteria; this file must not duplicate completed work as if
it were still missing.

## Product direction

- **Agentless execution.** Praetor bootstraps a self-contained host runner over
  SSH and executes Ansible natively on the selected runner host.
- **Event-driven control plane.** PostgreSQL, NATS JetStream, and independently
  released services provide durable scheduling, execution, ingestion, and
  reconciliation.
- **Fail-closed authorization.** Organization, team, resource-role, LDAP, and
  delegated-launch decisions are enforced server-side.
- **Secret references, not secret copies.** Automation credentials are stored in
  Praetor Secrets and resolved only for a claimed execution.
- **Compatible component sets.** A platform release is the version set declared
  by [`platform-compatibility.yaml`](platform-compatibility.yaml), not one
  repository tag in isolation.

## Shipped capability ledger

The following capabilities are implemented on `main`. Their supporting routes,
schema, workflows, and operational guidance are the evidence used when this
roadmap is reviewed.

| Capability | Current implementation evidence |
| --- | --- |
| Prompt-on-launch and surveys | Migrations `000020` and `000021`; template launch UI and server-side launch validation |
| Notifications and inbound webhooks | Migrations `000022`, `000023`, and `000052`–`000054`; notification and webhook API routes |
| Dynamic inventory, fact caching, and launch scope | Migrations `000024` and `000026`; inventory-source APIs; frozen inventory/fact wire contracts; server-side inventory, host, and client-supplied limit enforcement |
| Native Galaxy collection cache and lockfile | Host-runner content-addressed collection cache and generated `requirements.lock` |
| Workflow DAGs and approvals | Migration `000027` plus workflow snapshots, notification attachments, approval audit, fixed 24-hour rejection, and team-scoped approvals |
| Activity stream and security hardening | Activity-capture middleware, auditor-only activity API/UI, capability-based RBAC, and focused security gates |
| Praetor Secrets integration | Credential records store Secrets Service IDs and versions; executors receive short-lived claims and resolve credentials just in time |
| Delegated API launches | Service principals, bounded launch grants, host/inventory scope enforcement, idempotency, concurrency controls, and attributable audit records |
| Platform release automation | Compatibility manifest, release preflight, protected-environment promotion, component image gates, and GitHub release orchestration |
| Database and wire compatibility | Executable migration matrix and versioned cross-service fixtures with producer/consumer boundary tests |

Detailed operational and security contracts live in:

- [`docs/DELEGATED_API_USERS.md`](docs/DELEGATED_API_USERS.md)
- [`docs/RBAC.md`](docs/RBAC.md)
- [`docs/wire-contracts.md`](docs/wire-contracts.md)
- [`docs/database-compatibility.md`](docs/database-compatibility.md)
- [`docs/releasing.md`](docs/releasing.md)
- [`docs/repository-topology.md`](docs/repository-topology.md)

Praetor Secrets is an independently released service. Its cryptography, storage,
key rotation, and internal roadmap belong in the
[`Niftel/praetor-secrets`](https://github.com/Niftel/praetor-secrets)
repository. This repository owns only the integration boundary.

## Committed next work

### Shared-module standalone health

Track: [#121 — Extend standalone health checks to shared Praetor modules](https://github.com/Niftel/praetor/issues/121)

Every independently released shared module must pass formatting, vet, build,
tests, dependency-isolation checks, and release-metadata validation with
`GOWORK=off`. The result will become part of platform release preflight.

This is the only currently committed roadmap item. New work must first become a
scoped issue with outcome, acceptance criteria, tests, security impact, and
dependencies.

## Demand-gated ideas

These are architectural options, not release blockers or active commitments:

- additional dynamic-inventory and notification providers;
- an Automation Hub-compatible collection service for air-gapped governance;
- container-based execution when a concrete isolation requirement outweighs the
  agentless model;
- a persistent execution-node fleet if on-demand runner bootstrap no longer
  meets measured scale or latency requirements;
- broader delegated-administration UX beyond the shipped bounded API model.

Demand-gated ideas should not enter the committed roadmap until usage evidence
and a scoped GitHub issue exist.

## Explicit non-goals

- Recreating AWX screens or data models that do not fit Praetor's architecture.
- Treating local sibling repositories or `go.work` replacements as a release
  definition.
- Storing plaintext automation credentials in Praetor's database.
- Allowing clients to expand inventory, host, approval-team, or credential scope
  beyond server-side grants.
