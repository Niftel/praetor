# Praetor RBAC

Praetor separates authorization into three explicit responsibilities:

1. Praetor's PostgreSQL schema stores role definitions, assignments, and the
   materialized capability grants derived from them.
2. `pkg/authorization` resolves trusted grants for an authenticated user.
3. `github.com/praetordev/rbac/v4` is the domain-blind policy decision point
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

`services/api/handlers/authz.go` constructs one shared authorizer. Grant writes
continue through the v1 compatibility package while the decision path uses v4:

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

The temporary v1 dependency remains for Praetor-specific vocabulary,
assignment writes, and compatibility interfaces. It is not the PDP. Removing it
requires extracting those remaining domain and persistence APIs into a
Praetor-owned package; that is a later migration and must not be conflated with
the v4 decision cutover.

## Enforcement helpers

The API's shared `Authorizer` exposes:

- `authorize` for an action on one object;
- `authorizeOrgRole` for cross-type create capabilities held on an organization;
- `requireGlobal` for system-scoped resources;
- `readableIDs` and `canViewAll` for collection filtering;
- `grantCreatorAdmin` for assignment after object creation.

The legacy `users.is_superuser` break-glass behavior remains isolated in the
existing compatibility decorator. Normal user and team decisions pass through
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
