# Delegated API users

Status: design proposal; implementation not yet complete.

## Goal

Allow an external application to offer a restricted Praetor launch experience to
its users. The application may launch workflows owned by another team only when
an administrator has explicitly delegated that workflow and a bounded target
scope to the application.

This is not user impersonation. Praetor must always know:

- which application authenticated;
- which external user requested the action;
- which Praetor administrator created the delegation; and
- which workflow, inventory, hosts, variables, and lifetime the delegation
  permits.

## Why personal access tokens are insufficient

Praetor personal access tokens authenticate as their owning human user and
inherit that user's current RBAC assignments. Using an administrator's token in
an application would create a confused deputy:

- the application receives every permission the human gains later;
- launches are attributed to the human token owner rather than the application
  and external requester;
- there is no server-side boundary for allowed workflows, inventories, hosts,
  or launch variables; and
- revoking one integration can require disrupting the human user's own access.

Personal access tokens remain appropriate for a person automating actions they
could perform directly. They are not the credential type for shared
applications.

## Security model

### Service principal

A service principal is a non-human Praetor identity owned by one organization.
It cannot log into the UI, use LDAP mappings, become a superuser, or receive
human/team role assignments. It has independently rotated client credentials
whose plaintext is shown once and whose digest is stored.

Authentication produces a principal with:

- `kind=service`;
- immutable service-principal ID;
- organization ID;
- credential ID; and
- no user ID or break-glass flag.

The authorization engine must accept typed principals instead of treating a
service principal as a synthetic row in `users`.

### Delegated launch grant

A delegated launch grant is the only source of service-principal launch
authority. Each grant binds:

- one service principal;
- one workflow template;
- one inventory;
- an optional allowlist of host IDs or inventory groups;
- an optional maximum-host count;
- an allowlist of launch-time variable names;
- whether an approval team is fixed by the grant;
- activation and expiry times; and
- the administrator who created, updated, revoked, or renewed it.

The workflow owner delegates execution without transferring workflow edit,
credential, project, inventory administration, or secret-read permissions.

Grants fail closed when expired, revoked, internally inconsistent, or when a
referenced resource has moved to another organization.

### External requester

The application supplies an opaque external requester identifier for audit and
policy correlation, not a Praetor user ID and not a list of roles.

Version one treats that identifier as an application assertion. It is stored in
the launch audit record but grants no additional permission. A later token
exchange design may verify an external identity-provider token, but it must not
weaken the delegated grant.

The application cannot use `X-User`, `X-Roles`, or similar headers to impersonate
a Praetor user.

## Launch contract

Use a dedicated route rather than widening the human launch route:

```http
POST /api/v1/delegated/workflow-templates/{id}/launch
Authorization: Bearer prtr_sp_...
Idempotency-Key: <application-generated UUID>
Content-Type: application/json

{
  "external_requester": "customer-user-1842",
  "inventory_id": 27,
  "host_ids": [301, 302],
  "extra_vars": {
    "change_ticket": "CHG-1042"
  }
}
```

Clients do not send a raw Ansible `limit`. Praetor constructs the effective
limit from server-resolved host IDs after verifying that every host belongs to
the grant's inventory and allowlist. This avoids host-pattern injection and
prevents a client-supplied expression from expanding beyond the delegated
scope.

The server performs these checks in one transaction:

1. authenticate an active service credential;
2. load the active service principal and organization;
3. load one active grant for the requested workflow and inventory;
4. verify the workflow and inventory still belong to the grant organization;
5. verify every requested host belongs to that inventory and the grant scope;
6. enforce the maximum-host count;
7. reject launch variables not present in the grant allowlist;
8. resolve a fixed approval team from the grant when the workflow needs
   approval;
9. enforce workflow concurrency;
10. create the workflow run, attribution record, and audit event atomically; and
11. persist the idempotency result before returning.

Authorization is the intersection of the service credential, the delegated
grant, and current resource state. A permissive result from only one layer is
never sufficient.

## Data model

### `service_principals`

- `id`
- `organization_id`
- `name`
- `description`
- `enabled`
- `created_by_user_id`
- `created_at`, `updated_at`, `disabled_at`

Names are unique within an organization.

### `service_credentials`

- `id`
- `service_principal_id`
- `name`
- `token_hash`
- `expires_at`
- `last_used_at`
- `created_by_user_id`
- `created_at`, `revoked_at`

Credentials use a distinct `prtr_sp_` prefix and are never accepted as personal
access tokens.

### `delegated_launch_grants`

- `id`
- `service_principal_id`
- `workflow_template_id`
- `inventory_id`
- `allowed_host_ids`
- `allowed_group_ids`
- `max_hosts`
- `allowed_extra_var_keys`
- `approval_team_id`
- `not_before`, `expires_at`, `revoked_at`
- `created_by_user_id`, `updated_by_user_id`
- `created_at`, `updated_at`

Database constraints and transactional validation require all referenced
resources to share the service principal's organization.

### Launch attribution

Workflow runs gain nullable machine-attribution fields:

- `launched_by_service_principal_id`;
- `delegated_launch_grant_id`;
- `external_requester`;
- `idempotency_key`; and
- the resolved inventory and host-ID snapshot.

Exactly one of `launched_by_user_id` and
`launched_by_service_principal_id` is set for an authenticated manual launch.

## Administration and RBAC

Organization administrators may create and disable service principals and
rotate their credentials. Creating a delegated grant additionally requires:

- `execute` on the workflow;
- `use` on the inventory;
- `view` on every allowlisted host through the inventory; and
- authority to assign the fixed approval team.

Updating a grant repeats all checks. Existing grants do not silently expand
when a workflow, inventory, team, or human administrator receives broader
permissions.

Service principals do not appear in LDAP mappings or team membership. A
dedicated administration API and UI show credentials, grants, last use, expiry,
and recent launch outcomes.

## Audit requirements

Audit events must record:

- service-principal and credential IDs;
- delegated-grant ID;
- external requester;
- workflow, inventory, and resolved host IDs;
- idempotency key;
- administrator identity for grant changes;
- allow or deny result with a stable reason code; and
- resulting workflow run ID.

Tokens, credential inputs, secret values, and unrestricted request bodies must
never be logged.

Required stable denial reasons include inactive principal, expired credential,
missing grant, expired grant, cross-organization resource, host outside scope,
host-count exceeded, variable outside allowlist, invalid approval team, replay
conflict, and concurrency conflict.

## Required tests

- a service credential cannot authenticate as a human PAT;
- service principals cannot access ordinary user or administration endpoints;
- execute access without a matching delegated grant is denied;
- a grant cannot reference cross-organization resources;
- raw `limit` input is rejected;
- host IDs outside the inventory or allowlist are rejected;
- deleted, moved, disabled, expired, and revoked resources fail closed;
- unapproved variable names are rejected;
- duplicate idempotency keys return the original result, while conflicting
  payloads are rejected;
- concurrent launches cannot bypass the workflow concurrency guard;
- approval notifications route only to the grant's fixed team;
- every allow and deny is attributable without leaking tokens or secrets; and
- a human PAT launch continues to use the existing RBAC path unchanged.

## Explicit non-goals for version one

- arbitrary impersonation of Praetor users;
- accepting client-provided roles, permissions, organization IDs, or raw host
  patterns;
- giving a service principal general inventory, credential, project, or
  workflow administration;
- allowing a service principal to retrieve automation credentials;
- OAuth/OIDC token exchange or external identity-provider policy evaluation;
- long-lived credentials without an expiry; and
- cross-organization delegation.

## Implementation sequence

1. Add typed authenticated principals without changing current human behavior.
2. Add service-principal and credential administration with expiry, rotation,
   revocation, and audit.
3. Add delegated grant storage and administration with cross-resource
   validation.
4. Add the dedicated launch endpoint, server-resolved host limit, attribution,
   and idempotency.
5. Add end-to-end tests covering service authentication, delegated launch,
   team approval, execution, denial, revocation, and audit.
6. Add the administration UI only after the API security contract is stable.
