# Praetor RBAC

Praetor separates authorization into four explicit responsibilities:

1. Praetor's PostgreSQL schema stores role definitions, assignments, and the
   materialized capability grants derived from them.
2. `pkg/accesscontrol` owns Praetor's original domain vocabulary and assignment store.
3. `pkg/authorization` resolves trusted grants for an authenticated user.
4. `github.com/praetordev/rbac/v4` is the domain-blind policy decision point
   (PDP) that evaluates those grants using an immutable policy snapshot.

HTTP handlers remain policy enforcement points. They express a capability
question and do not inspect role names or assignment tables.

## Persisted grant model

| Concept | Tables | Meaning |
|---|---|---|
| Capability | `dab_permissions` | Atomic codename such as `view_inventory` |
| Role definition | `role_definitions`, `role_definition_permissions` | Named bundle of capabilities |
| Scoped role | `object_roles` | A role definition applied globally or to an object |
| Assignment | `role_user_assignments`, `role_team_assignments` | Users or teams holding an object role |
| Evaluation cache | `role_evaluations` | Flattened scoped capabilities used during resolution |

Praetor owns these tables and their migrations. RBAC v4 deliberately owns no
database schema, content types, actions, role names, or hierarchy. The database
triggers flatten organization-to-resource inheritance before a decision is
made, as required by the engine's integration contract.

## Decision flow

`services/api/handlers/authz.go` constructs one shared authorizer. Praetor owns
the capability vocabulary and grant persistence while the decision path uses v4:

```text
verified user id
  -> PostgreSQL grant resolver
  -> []rbac/v4.Grant
  -> immutable Praetor policy snapshot
  -> allow or deny
  -> HTTP enforcement point
```

Scopes are opaque to the engine. Praetor encodes an object scope as
`<content-type>:<id>` (for example `inventory:42`) and uses the empty string for
global grants. Grants are always resolved from server-controlled tables using
the authenticated user id; request-provided roles or capabilities must never be
passed into the engine.

The embedded policy uses `DenyOverrides`, exact capability matching, exact
scoped matching, and global grants. A missing policy, missing grant, malformed
scope, database failure, or unmatched rule never grants access.

`pkg/accesscontrol` contains newly authored platform vocabulary, built-in role
declarations, assignment persistence, and the handler-facing decision contract.
It contains no policy evaluator. Runtime verdicts are produced exclusively by
RBAC v4 through `pkg/authorization`.

## Enforcement helpers

The API's shared `Authorizer` exposes:

- `authorize` for an action on one object;
- `authorizeOrgRole` for cross-type create capabilities held on an organization;
- `requireGlobal` for system-scoped resources;
- `readableIDs` and `canViewAll` for collection filtering;
- `grantCreatorAdmin` for assignment after object creation.

The `users.is_superuser` break-glass behavior remains isolated in an explicit
Praetor decorator. Normal user and team decisions pass through
RBAC v4. System-auditor access is represented by global capability assignments.

## Verification

Pure adapter tests cover allow, default deny, global visibility, scoped
visibility, and deny-overrides:

```sh
GOWORK=off go test ./pkg/authorization
```

The complete handler and schema integration runs against a throwaway migrated
PostgreSQL instance:

```sh
make test-db
```

## Policy operations

The API binary embeds `pkg/authorization/policy.json` as its safe default. Set
`PRAETOR_RBAC_POLICY` to load a mounted policy file instead. A configured file
must exist and parse successfully at startup; otherwise the API fails to start.

`PRAETOR_RBAC_POLICY_REFRESH_INTERVAL` controls how often the source is checked
(default `30s`). A missing, malformed, or oversized update is reported while
RBAC v4 continues serving the last-known-good immutable snapshot. A later valid
update is parsed once and installed atomically.

Set `PRAETOR_RBAC_POLICY_SHA256` to the lowercase or uppercase hexadecimal
SHA-256 digest of the mounted file to require verify-before-parse on startup and
every refresh. A mismatched update is rejected and the last-known-good snapshot
continues serving. Leaving it empty uses the file source's pass-through verifier;
remote policy sources must not be enabled without an authenticity verifier.

System administrators can inspect and refresh policy provenance through:

- `GET /api/v1/rbac/policy` — source, active version, load state, last refresh
  attempt, last successful refresh, and the latest error;
- `POST /api/v1/rbac/policy/refresh` — request an immediate refresh.

Both routes require authentication and the global `manage_user` capability.

Set `PRAETOR_RBAC_DECISION_AUDIT=true` to emit a structured log event for every
v4 evaluation. Each event records the authenticated user id, requested
capability and scope, allow/deny result, immutable policy snapshot, reason, and
the stable deciding rule id/name/effect. A default deny has no rule id. This is
disabled by default because list visibility checks can evaluate several scopes.
