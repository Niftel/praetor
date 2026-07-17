# Staging acceptance report

Date: 2026-07-17  
Parent: [#156](https://github.com/Niftel/praetor/issues/156)  
Release: persistent `praetor-staging` environment

## Scripted journeys

| Journey | Result | Evidence |
|---|---|---|
| LDAP login, organization/team mapping, inventory/host scope, approvals, notification delivery | Pass | `ldap-operator.json`, `staging-acceptance.json` |
| Delegated API inventory, host, limit, organization, approval-team, extra-variable, idempotency, and principal boundaries | Pass (16 tests) | `delegated-api.json` |

Evidence files are generated locally below
`~/.local/share/praetor/staging/acceptance/evidence/`, contain no credentials or
tokens, and are deliberately not committed.

## Manual UI journeys

| Role and viewport | Journey | Result |
|---|---|---|
| `demo-operator`, desktop | LDAP login; Engineering workflow and inventory visibility; host edit controls; DAG builder node selection; launch and relaunch with backend-team approval; credential fields remain sealed/write-only | Pass |
| `mwebb`, desktop | Pending approval is visible only to an assigned backend-team member; approval completes the run and clears the notification | Pass |
| `demo-auditor`, desktop | Read-only workflow, run, inventory, host, access, and audit visibility | Defect [#162](https://github.com/Niftel/praetor/issues/162): unauthorized mutation controls are rendered |
| `demo-auditor`, 390 x 844 | Workflow and inventory pages remain readable and operable | Defect [#163](https://github.com/Niftel/praetor/issues/163): header/actions clip and resource names become unreadable |

The API authorization tests continued to deny unauthorized mutations; #162 is
a capability-aware UI defect, not evidence that the server-side RBAC boundary
was bypassed. No secret value appeared in the application UI, notification
sink logs, or sanitized evidence artifacts.
