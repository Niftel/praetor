# Staging acceptance report

Date: 2026-07-18
Parent: [#167](https://github.com/Niftel/praetor/issues/167)
Release: platform `0.1.2`, Helm revision 9 in persistent `praetor-staging`

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
| `demo-auditor`, desktop (1280 x 720) | Read-only workflow, run, inventory, host, access, and audit visibility; no workflow or inventory mutation controls | Pass; verifies [#162](https://github.com/Niftel/praetor/issues/162) |
| `demo-auditor`, 390 x 844 | Workflow and inventory pages remain readable and operable; inventory detail tabs remain accessible; no horizontal overflow | Pass; verifies [#163](https://github.com/Niftel/praetor/issues/163) |

The API authorization tests continue to deny unauthorized mutations, and the
`0.1.2` UI now omits the corresponding auditor mutation controls. No secret
value appeared in the application UI, notification sink logs, or sanitized
evidence artifacts.

## Release-candidate decision

**Go.** Platform `0.1.2` is deployed from eight immutable component digests.
The automated product journeys, staging health, encrypted recovery evidence,
and responsive UI acceptance all pass with no open release blocker. The live UI
digest is
`sha256:b0ce7d3d737c09e40bcea6c856d80ae45ccc15f231552049d6dc3a8deafef67d`.

The machine-readable decision is
[`release-candidate-decision.json`](release-candidate-decision.json). It binds
the GO decision to source revision
`5dfb448eb855723778c8ddc9d815413dcf3fe58a`, the exact component digests, and
the sanitized evidence hashes. This completes the release-candidate remediation
tracked by [#167](https://github.com/Niftel/praetor/issues/167); production
promotion remains outside this issue's scope.
