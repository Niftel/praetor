# Praetor RBAC

Praetor uses an **AWX-style object-role** model. There is exactly one role
concept (an earlier permission-list `roles` table was removed in migration
`000011`; do not reintroduce a `permissions` column).

## Model

A **role** is a grant of a particular kind of access *on a particular object*:

| Concept | Where | Meaning |
|---|---|---|
| Object role | `roles(content_type, object_id, role_field)` | e.g. `admin_role` on `organization 5` |
| Singleton role | `roles(singleton_name)` | system-wide: `system_administrator`, `system_auditor` |
| Hierarchy | `role_parents` → `role_ancestors` | holding a parent role implies the child (the ancestors table is the flattened closure for fast lookups) |
| Membership | `role_members` (user→role), `team_roles` (team→role), `team_members` (user→team) | who holds a role |

`role_field` values: `admin_role`, `read_role`, `use_role`, `execute_role`,
`update_role`, `member_role`, plus the org-level `*_admin_role` variants.

Object roles are created automatically by triggers when an object is created
(`create_organization_roles`, `create_project_roles`, `create_inventory_roles`,
`create_job_template_roles`, `create_credential_roles`, `create_team_roles`),
and parented into the hierarchy (e.g. an org's `admin_role` is an ancestor of
its projects' `admin_role`).

Superuser is the `users.is_superuser` flag (AWX-faithful); `is_system_auditor`
grants read on everything. Both are resolved inside the checker.

## Enforcement

`pkg/rbac.AccessChecker` answers the questions — `CanRead`, `CanAdmin`,
`CanUse`, `CanExecute`, `FilterAccessibleIDs`, plus membership management — by
recursively resolving direct, ancestor, and team grants.

The API enforces via a shared `Authorizer` (`services/api/handlers/authz.go`)
embedded by every resource handler:

- `authorize(w, r, contentType, id, action)` — writes 403/500 and returns
  `false`; callers `return` on `false`.
- `readableIDs(r, contentType)` — scopes list endpoints.
- `grantCreatorAdmin(...)` — the creator of a new object gets its `admin_role`
  (so a non-superuser can manage what they create).

### Verb mapping (what each endpoint requires)

| Action | Check |
|---|---|
| List | scope to `readableIDs` (read) |
| Get | `CanRead` |
| Create | `CanAdmin` on the **parent** (org), then grant creator admin |
| Update / Delete | `CanAdmin` on the object |
| Launch a job template | `CanExecute` on the template |
| Attach project/inventory/credential to a template | `CanUse` on each |
| Hosts / groups | governed by their **parent inventory** (no roles of their own): read to view, admin to mutate |
| Jobs | scoped to jobs whose template the user can read; launch = execute on the template |
| Users (create/update/delete) | superuser only (the update path can set `is_superuser`) |

Role/team membership endpoints (`/roles/{id}/users`, `/teams`) require admin on
the role's parent object; singleton-role grants require superuser.

## Tests

`services/api/handlers/projects_rbac_test.go` exercises the pattern end-to-end
against a migrated DB (triggers firing). Run with `TEST_DATABASE_URL` set.
