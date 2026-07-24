
# Praetor REST API Reference (v1) — Full

## Table of Contents

1. [Overview](#overview)  
2. [API Standards and Best Practices](#api-standards-and-best-practices)  
3. [Authentication](#authentication)  
4. [Versioning](#versioning)  
5. [API Root and Discovery](#api-root-and-discovery)  
6. [Pagination](#pagination)  
7. [Filtering, Searching, and Sorting](#filtering-searching-and-sorting)  
8. [Response Formats](#response-formats)  
9. [Permissions and RBAC](#permissions-and-rbac)  
10. [Core Execution Resources](#core-execution-resources)  
    - [Job (Logical)](#101-job-logical)  
    - [Job Run (Physical Attempt)](#102-job-run-physical-attempt)  
    - [Job Events](#103-job-events)  
    - [Job Host Summaries](#104-job-host-summaries)  
    - [Logs](#105-logs)  
    - [Internal Execution Manifest](#106-internal-execution-manifest)  
11. [Control-Plane Resources](#control-plane-resources)  
    - [System Configuration](#111-system-configuration)  
    - [Users, Me, and Auth](#112-users-me-and-auth)  
    - [Organizations](#113-organizations)  
    - [Teams](#114-teams)  
    - [Roles](#115-roles)  
    - [Projects and Project Syncs](#116-projects-and-project-syncs)  
    - [Execution Environments](#117-execution-environments)  
    - [Credentials and Credential Types](#118-credentials-and-credential-types)  
    - [Inventories, Hosts, Groups](#119-inventories-hosts-groups)  
    - [Inventory Sources and Updates](#1110-inventory-sources-and-updates)  
    - [Job Templates](#1111-job-templates)  
    - [Ad Hoc Commands](#1112-ad-hoc-commands)  
    - [Workflow Job Templates and Workflow Jobs](#1113-workflow-job-templates-and-workflow-jobs)  
    - [Workflow Approvals](#1114-workflow-approvals)  
    - [Schedules](#1115-schedules)  
    - [Notifications and Notification Templates](#1116-notifications-and-notification-templates)  
    - [Labels](#1117-labels)  
    - [Activity Stream](#1118-activity-stream)  
    - [Unified Resources and Bulk Operations](#1119-unified-resources-and-bulk-operations)  
12. [Infrastructure and Kubernetes Abstractions](#infrastructure-and-kubernetes-abstractions)  
    - [Execution Pools (Instance Groups)](#121-execution-pools-instance-groups)  
    - [Execution Instances](#122-execution-instances)  
    - [Control Plane Instances](#123-control-plane-instances)  
13. [Special Features and Actions](#special-features-and-actions)  
14. [Error Handling](#error-handling)  
15. [URL Patterns and Naming](#url-patterns-and-naming)  
16. [References](#references)  

---

## Overview

The **Praetor REST API** is a comprehensive RESTful interface that provides programmatic access to Praetor’s:

- **Control plane** (organizations, inventories, job templates, workflows, RBAC, etc.).  
- **Execution plane** (jobs, job runs, events, logs).  
- **Infrastructure abstractions** for Kubernetes-backed execution capacity.

The design is heavily inspired by the AWX `/api/v2` API surface and aims for **UI-level parity** with AWX:

> Anything a user can do via the AWX UI should be achievable via the Praetor UI backed by this `/api/v1` API.

Internally, Praetor differs from AWX in key ways:

- Jobs are split into **logical Jobs** and **physical Job Runs**.  
- Logs are stored in **object storage** (S3/MinIO/etc.), indexed by Job Run + sequence.  
- Execution is **Kubernetes-native** and resilient to **DB and event bus outages**.  

**Base URL**: `/api/v1/`  
**Current Version**: `v1` (only supported major version)  
**Content-Type**: `application/json` (unless otherwise stated)

---

## API Standards and Best Practices

### Core Principles

1. **Paginate Everything**  
   All collection endpoints MUST be paginated (page-based or limit/offset).

2. **Performance Target**  
   Typical response time SHOULD be ≤ 250ms under normal load for standard UI operations.

3. **Assume Large Data Sets**  
   The system MUST handle organizations with thousands of hosts and jobs with millions of events.

4. **RBAC Filtering**  
   Collections MUST be automatically filtered by caller permissions.

5. **API Discoverability**  
   The API MUST be navigable from `/api/` and `/api/v1/` without out-of-band knowledge.

6. **RESTful Verbs**  
   Use `GET`, `POST`, `PATCH`, `DELETE` appropriately. `PUT` MAY be supported but is not required.

7. **Constant-Time Queries**  
   Query complexity SHOULD NOT grow with collection size; avoid full-table scans where possible.

8. **Bounded Query Count**  
   The number of SQL (or equivalent) queries per request SHOULD be bounded and predictable.

### Additional Requirements

- Frequently used filters MUST be indexed in the database.  
- Serializers MUST avoid N+1 queries (use `select_related` / `prefetch_related` or equivalent).  
- Every route MUST have unit tests (happy-path and basic failure modes).  

---

## Authentication

Praetor supports token-based and session-based authentication.

### Supported Methods

1. **Session Authentication**
   - Recommended for browser-based UIs.
   - Login via `/auth/login` (implementation-specific) using username/password.
   - Uses CSRF protection where applicable.

2. **HTTP Basic Auth (Optional)**
   - `Authorization: Basic base64(username:password)`
   - Controlled by configuration; typically disabled on public-facing deployments.
   - Requests SHOULD be logged for auditing.

3. **Bearer Token (Recommended)**
   - `Authorization: Bearer <token>`
   - Token MAY be a JWT or opaque token.
   - SHOULD carry identity, tenant/organization, and role/permissions when possible.

### Login and Logout

The REST spec does not require a particular schema for login/logout; a typical pattern:

- `POST /auth/login`
  - `{ "username": "alice", "password": "secret" }`
  - Returns a session cookie (for browsers) and/or an access token.

- `POST /auth/logout`
  - Invalidates the session / token.

### Current User

- `GET /api/v1/me`
  - Returns information about the currently authenticated user, organizations, roles, permissions, and feature flags relevant to the UI.

---

## Versioning

Praetor uses **URL path versioning**.

- API root: `/api/`
- Version root: `/api/v1/`

Future incompatible versions MUST use a new path prefix (e.g. `/api/v2/`).

Examples:

- `GET /api/` – version discovery.  
- `GET /api/v1/jobs` – v1 jobs collection.  

---

## API Root and Discovery

### `GET /api/`

Returns information on available versions:

```json
{
  "description": "Praetor REST API",
  "current_version": "/api/v1/",
  "available_versions": {
    "v1": "/api/v1/"
  }
}
```

### `GET /api/v1/`

Returns top-level endpoints for discovery; example shape:

```json
{
  "ping": "/api/v1/ping",
  "config": "/api/v1/config",
  "me": "/api/v1/me",
  "organizations": "/api/v1/organizations",
  "users": "/api/v1/users",
  "teams": "/api/v1/teams",
  "roles": "/api/v1/roles",
  "projects": "/api/v1/projects",
  "inventories": "/api/v1/inventories",
  "hosts": "/api/v1/hosts",
  "groups": "/api/v1/groups",
  "job_templates": "/api/v1/job-templates",
  "jobs": "/api/v1/jobs",
  "runs": "/api/v1/runs",
  "job_events": "/api/v1/job-events",
  "job_host_summaries": "/api/v1/job-host-summaries",
  "execution_environments": "/api/v1/execution-environments",
  "credentials": "/api/v1/credentials",
  "workflow_job_templates": "/api/v1/workflow-job-templates",
  "workflow_jobs": "/api/v1/workflow-jobs",
  "schedules": "/api/v1/schedules",
  "notification_templates": "/api/v1/notification-templates",
  "notifications": "/api/v1/notifications",
  "labels": "/api/v1/labels",
  "activity_stream": "/api/v1/activity-stream",
  "infra": {
    "execution_pools": "/api/v1/infra/execution-pools",
    "execution_instances": "/api/v1/infra/instances",
    "control_plane_instances": "/api/v1/infra/control-plane-instances"
  }
}
```

Exact keys MAY vary; the root is for interactive discovery only.

---

## Pagination

### Query Parameters

Praetor uses **limit/offset** pagination as canonical for v1:

- `limit` – maximum number of items (default 50, max 500).  
- `offset` – zero-based index into the result set (default 0).  

### Response Envelope

Standard paginated response:

```json
{
  "items": [
    // resources
  ],
  "total": 123,
  "limit": 50,
  "offset": 0
}
```

Implementations MAY also support an AWX-compatible `count/next/previous/results` format for compatibility, but `items/total/limit/offset` is canonical.

---

## Filtering, Searching, and Sorting

Praetor adopts AWX-style conventions.

### Sorting

Use `order_by`:

```text
?order_by=name
?order_by=-created_at
?order_by=status,-created_at
```

Multiple fields: comma-separated. A leading `-` indicates descending order.

### Searching

Use `search`:

```text
?search=web
```

- Searches across resource-defined `search_fields` (case-insensitive).  
- Multiple terms:
  - OR: `?search=foo&search=bar`  
  - AND: `?search=foo,bar`  

### Field Filters

Use `field__lookup=value` style:

```text
?name__icontains=prod
?created_at__gte=2025-01-01T00:00:00Z
?created_at__lt=2026-01-01T00:00:00Z
```

Common lookups:

- `exact`, `iexact`
- `contains`, `icontains`
- `startswith`, `istartswith`
- `endswith`, `iendswith`
- `gt`, `gte`, `lt`, `lte`
- `isnull` (boolean)
- `in` (comma-separated list)

Related-field filters use double-underscore notation:

```text
?organization__name__icontains=default
```

---

## Response Formats

### Standard Fields

Most resources include:

- `id` – primary key (integer or UUID).  
- `url` – canonical resource URL (optional).  
- `created_at` – ISO 8601 timestamp.  
- `modified_at` – ISO 8601 timestamp.  
- `name` – human-readable name (if applicable).  
- `description` – optional free-text description.  

Praetor often exposes compact relationship info via `summary` or `summary_fields` to avoid extra HTTP calls.

### Content Types

- `application/json` – default for most endpoints.  
- `text/plain` – used by some log/stream endpoints (optional).  

Clients SHOULD send `Accept: application/json` unless plain-text logs are desired.

---

## Permissions and RBAC

Praetor uses role-based access control.

### Roles and Scopes

Typical levels:

- **System roles** – `system_admin`, `system_auditor`, etc.  
- **Organization roles** – `org_admin`, `org_member`, etc.  
- **Resource roles** – e.g., `project_admin`, `inventory_admin`, `template_executor`.  

The mapping between roles and permissions is deployment-specific, but **the API behavior is standard**:

- **401 Unauthorized** if not authenticated.  
- **403 Forbidden** if authenticated but not authorized.  
- Collections are automatically filtered based on the caller’s permissions.  

Praetor SHOULD expose enough RBAC metadata for the UI to show/disable actions appropriately (via `/api/v1/me`, `/roles`, and per-resource permission flags if desired).

---

## Core Execution Resources

Praetor’s execution model is built around:

- **Job** – logical job (user-visible, what you see in the Jobs list).  
- **Job Run** – physical attempt (what executor Pods bind to).  
- **Job Events** – structured timeline of execution.  
- **Job Host Summaries** – per-host aggregated results.  
- **Logs** – streamed and chunked stdout/stderr.  
- **Execution Manifest** – internal manifest for executors.  

### 10.1 Job (Logical)

**Base URL**: `/api/v1/jobs`

Represents a high-level unit of work (playbook run, project sync, workflow, etc.).

#### Representation

```json
{
  "id": 123,
  "name": "Deploy web tier",
  "type": "job",             
  "job_template_id": 17,
  "organization_id": 1,
  "status": "running",        
  "created_at": "2025-12-10T09:00:00Z",
  "started_at": "2025-12-10T09:01:10Z",
  "finished_at": null,
  "timeout_seconds": 7200,
  "cancel_requested": false,
  "inventory_id": 42,
  "project_id": 7,
  "execution_environment_id": 5,
  "labels": ["deploy", "web"],
  "current_run_id": "c0c3bdc8-08c8-49aa-aea3-51f2d036e9f1",
  "summary": {
    "hosts_total": 120,
    "hosts_ok": 115,
    "hosts_failed": 3,
    "hosts_unreachable": 2,
    "hosts_skipped": 0,
    "hosts_changed": 110
  }
}
```

`type` is one of:
- `job`  
- `project_update`  
- `inventory_update`  
- `workflow_job`  
- `system_job`  
- `ad_hoc_command`  

#### Endpoints

- `GET /api/v1/jobs` – list jobs.  
- `POST /api/v1/jobs` – create a new job (job launch).  
- `GET /api/v1/jobs/{job_id}` – retrieve job details.  
- `POST /api/v1/jobs/{job_id}/cancel` – request cancellation.  
- `POST /api/v1/jobs/{job_id}/relaunch` – relaunch a job.  
- `GET /api/v1/jobs/{job_id}/runs` – list runs for a job.  
- `GET /api/v1/jobs/{job_id}/events` – list events for a job.  
- `GET /api/v1/jobs/{job_id}/hosts` – host summaries for a job.  
- `GET /api/v1/jobs/{job_id}/logs` – aggregated logs.  

#### Create Job (Launch)

**POST** `/api/v1/jobs`

```json
{
  "job_template_id": 17,
  "execution_environment_id": 5,
  "inventory_id": 42,
  "extra_vars": {
    "version": "1.2.3",
    "env": "prod"
  },
  "limit": "web*&!web-drain",
  "timeout_seconds": 7200,
  "labels": ["deploy", "web"]
}
```

Response `201 Created`:

```json
{
  "id": 123,
  "status": "pending",
  "job_template_id": 17,
  "inventory_id": 42,
  "created_at": "2025-12-10T09:00:00Z"
}
```

#### Cancel Job

**POST** `/api/v1/jobs/{job_id}/cancel`

```json
{
  "reason": "User requested cancel from UI"
}
```

Returns `202 Accepted` and updated job with `cancel_requested: true` (or `409 Conflict` if terminal).

#### Relaunch Job

**POST** `/api/v1/jobs/{job_id}/relaunch`

```json
{
  "reuse": "template_and_vars",
  "override_extra_vars": {
    "env": "staging"
  }
}
```

Returns `201 Created` with a new Job.

---

### 10.2 Job Run (Physical Attempt)

**Base URL**: `/api/v1/runs`

Represents a single execution attempt of a Job. Executors bind to a Run via `run_id`.

#### Representation

```json
{
  "id": "c0c3bdc8-08c8-49aa-aea3-51f2d036e9f1",
  "job_id": 123,
  "attempt_number": 1,
  "state": "running",    
  "created_at": "2025-12-10T09:00:10Z",
  "started_at": "2025-12-10T09:01:10Z",
  "finished_at": null,
  "last_heartbeat_at": "2025-12-10T09:05:05Z",
  "last_event_seq": 842,
  "exit_code": null,
  "error_reason": null
}
```

`state` is one of: `pending`, `starting`, `running`, `successful`, `failed`, `canceled`, `lost`.

#### Endpoints

- `GET /api/v1/runs` – list runs (filter by job_id, state, etc.).  
- `GET /api/v1/runs/{run_id}` – get run details.  
- `GET /api/v1/jobs/{job_id}/runs` – list runs for a given job.  

---

### 10.3 Job Events

**Base URL**: `/api/v1/job-events`  
**Job Subresource**: `/api/v1/jobs/{job_id}/events`

Represents play/task/host-level events produced by a Job Run.

#### Representation

```json
{
  "id": 1001,
  "job_id": 123,
  "run_id": "c0c3bdc8-08c8-49aa-aea3-51f2d036e9f1",
  "seq": 42,
  "timestamp": "2025-12-10T09:03:11.123Z",
  "event_type": "TASK_OK",
  "host": "web01.example.com",
  "task_name": "Install nginx",
  "play_name": "Configure web servers",
  "stdout_snippet": "changed: [web01.example.com]",
  "data": {
    "rc": 0,
    "changed": true
  }
}
```

#### Endpoints

- `GET /api/v1/job-events`
  - Filters: `job_id`, `run_id`, `from_seq`, `to_seq`, `event_type`, `host`, plus pagination.  

- `GET /api/v1/jobs/{job_id}/events`
  - Same query parameters, but implicitly filtered by `job_id`.  

Praetor MAY also support a streaming channel (e.g. WebSocket or SSE) for live UI updates based on events.

---

### 10.4 Job Host Summaries

**Base URL**: `/api/v1/job-host-summaries`  
**Job Subresource**: `/api/v1/jobs/{job_id}/hosts`

Aggregated counts for each host in a Job.

#### Representation

```json
{
  "job_id": 123,
  "host": "web01.example.com",
  "host_id": 555,
  "ok": 10,
  "changed": 2,
  "failed": 0,
  "skipped": 0,
  "unreachable": 0,
  "last_event_at": "2025-12-10T09:05:00Z"
}
```

#### Endpoints

- `GET /api/v1/job-host-summaries`
  - Filters: `job_id`, `host`, `status` (logical status like `failed`, `unreachable`, `ok`).  

- `GET /api/v1/jobs/{job_id}/hosts`
  - Same but constrained to a single job.  

These power the “Hosts” tab in the Job detail page.

---

### 10.5 Logs

Praetor stores logs in object storage (e.g. S3) and provides aggregated views and chunk indexes.

#### Aggregated Job Logs

**GET** `/api/v1/jobs/{job_id}/logs`

Query parameters:

- `host` – optional host filter.  
- `follow` – if `true`, server MAY stream new log lines.  

JSON response example:

```json
{
  "lines": [
    "PLAY [Configure web servers] *********************************************************",
    "TASK [Install nginx] *****************************************************************",
    "changed: [web01.example.com]"
  ]
}
```

Server MAY also support `text/plain` streaming for `follow=true`.

#### Log Chunk Index

**GET** `/api/v1/runs/{run_id}/log-chunks`

```json
{
  "items": [
    {
      "run_id": "c0c3bdc8-08c8-49aa-aea3-51f2d036e9f1",
      "seq": 0,
      "storage_key": "jobs/c0c3bdc8-08c8-49aa-aea3-51f2d036e9f1/chunk-0000.log",
      "byte_length": 1024,
      "created_at": "2025-12-10T09:04:00Z"
    }
  ],
  "total": 20,
  "limit": 50,
  "offset": 0
}
```

Praetor does not prescribe how object storage is accessed; the `storage_key` is an implementation detail for the controller/executor.

---

### 10.6 Internal Execution Manifest

**Base URL**: `/internal/api/v1/runs/{run_id}/manifest`

This endpoint is **not public**; it is used by executor Pods inside the cluster to obtain a full manifest for a Job Run.

#### Representation

```json
{
  "run_id": "c0c3bdc8-08c8-49aa-aea3-51f2d036e9f1",
  "job_id": 123,
  "attempt_number": 1,
  "execution_environment": {
    "id": 5,
    "image": "registry.example.com/ee:latest"
  },
  "inventory": {
    "mode": "inline",
    "content": "[web]\nweb01.example.com\nweb02.example.com\n",
    "path": null
  },
  "playbook": {
    "type": "scm",
    "project_id": 7,
    "playbook_path": "playbooks/deploy.yml"
  },
  "extra_vars": {
    "version": "1.2.3",
    "env": "prod"
  },
  "credentials": [
    { "id": 100, "type": "machine", "name": "SSH Key" },
    { "id": 101, "type": "vault", "name": "Vault Password" }
  ],
  "timeout_seconds": 7200,
  "limit": "web*&!web-drain",
  "verbosity": 1
}
```

Executors SHOULD:

- Fetch manifest at start-up.  
- Use event bus and object storage for ongoing I/O.  
- Not require direct DB access.  

---

## Control-Plane Resources

This section describes resources that correspond to AWX UI areas and workflows: organizations, users, teams, projects, inventories, credentials, job templates, jobs, workflows, schedules, notifications, etc.

Praetor aims for **feature parity** with AWX from a UI perspective.

### 11.1 System Configuration

**Endpoints** (examples):

- `GET /api/v1/ping` – basic instance information.  
- `GET /api/v1/config` – system configuration and license information.  
- `GET /api/v1/metrics` – Prometheus-style metrics (optional).  
- `GET /api/v1/settings` – list of setting categories.  
- `GET /api/v1/settings/{category}` – settings in a category.  
- `PATCH /api/v1/settings/{category}` – update settings in a category.

These power the “Settings/Config” parts of the UI.

---

### 11.2 Users, Me, and Auth

#### Users

**Base URL**: `/api/v1/users`

- `GET /api/v1/users` – list users.  
- `POST /api/v1/users` – create user.  
- `GET /api/v1/users/{id}` – retrieve user.  
- `PATCH /api/v1/users/{id}` – update user.  
- `DELETE /api/v1/users/{id}` – delete user.  

User representation:

```json
{
  "id": 42,
  "username": "alice",
  "email": "alice@example.com",
  "first_name": "Alice",
  "last_name": "Admin",
  "is_active": true,
  "created_at": "2025-01-01T12:00:00Z",
  "modified_at": "2025-12-10T10:00:00Z"
}
```

Subresources (for UI tabs):

- `/api/v1/users/{id}/organizations` – orgs the user belongs to.  
- `/api/v1/users/{id}/teams` – teams.  
- `/api/v1/users/{id}/roles` – roles.  

#### Me

- `GET /api/v1/me`
  - Provides the current user along with organizations, roles, and effective permissions.

---

### 11.3 Organizations

**Base URL**: `/api/v1/organizations`

- `GET /api/v1/organizations`  
- `POST /api/v1/organizations`  
- `GET /api/v1/organizations/{id}`  
- `PATCH /api/v1/organizations/{id}`  
- `DELETE /api/v1/organizations/{id}`  

Representation:

```json
{
  "id": 1,
  "name": "Default",
  "description": "Default organization",
  "created_at": "2025-01-01T00:00:00Z",
  "modified_at": "2025-12-10T09:00:00Z"
}
```

Subresources (UI tabs):

- `/api/v1/organizations/{id}/users`  
- `/api/v1/organizations/{id}/teams`  
- `/api/v1/organizations/{id}/projects`  
- `/api/v1/organizations/{id}/inventories`  
- `/api/v1/organizations/{id}/job-templates`  
- `/api/v1/organizations/{id}/workflow-job-templates`  
- `/api/v1/organizations/{id}/execution-environments`  
- `/api/v1/organizations/{id}/credentials`  
- `/api/v1/organizations/{id}/notification-templates`  

These subresources are filtered views into the respective collections.

---

### 11.4 Teams

**Base URL**: `/api/v1/teams`

- `GET /api/v1/teams`  
- `POST /api/v1/teams`  
- `GET /api/v1/teams/{id}`  
- `PATCH /api/v1/teams/{id}`  
- `DELETE /api/v1/teams/{id}`  

Representation:

```json
{
  "id": 5,
  "name": "DevOps",
  "description": "DevOps team",
  "organization_id": 1,
  "created_at": "2025-02-01T10:00:00Z",
  "modified_at": "2025-12-10T10:30:00Z"
}
```

Subresources:

- `/api/v1/teams/{id}/users`  
- `/api/v1/teams/{id}/roles`  

---

### 11.5 Roles

**Base URL**: `/api/v1/roles`

Roles define permissions that can be assigned to users and teams.

- `GET /api/v1/roles` – list roles.  
- `GET /api/v1/roles/{id}` – role details.  
- `POST /api/v1/roles/{id}/assign-users` – assign users to role.  
- `POST /api/v1/roles/{id}/assign-teams` – assign teams to role.  

Representation (example):

```json
{
  "id": 10,
  "name": "project_admin",
  "scope": "project",
  "description": "Full control over a project",
  "created_at": "2025-03-01T00:00:00Z"
}
```

---

### 11.6 Projects and Project Syncs

**Base URL**: `/api/v1/projects`

- `GET /api/v1/projects`  
- `POST /api/v1/projects`  
- `GET /api/v1/projects/{id}`  
- `PATCH /api/v1/projects/{id}`  
- `DELETE /api/v1/projects/{id}`  

Representation:

```json
{
  "id": 7,
  "name": "web-app-config",
  "description": "Web app Ansible repo",
  "organization_id": 1,
  "scm_url": "git@github.com:example/web-app-config.git",
  "scm_branch": "main",
  "created_at": "2025-05-01T10:00:00Z",
  "modified_at": "2025-12-10T09:05:00Z"
}
```

Actions:

- `POST /api/v1/projects/{id}/sync`
  - Initiates a project update (SCM sync).  
  - Internally creates a Job with `type: "project_update"`.  

Subresources:

- `/api/v1/projects/{id}/playbooks` – list playbooks in project.  
- `/api/v1/projects/{id}/jobs` – project-related jobs (project updates + jobs using project).  

---

### 11.7 Execution Environments

(Already introduced under core spec, repeated for completeness)

**Base URL**: `/api/v1/execution-environments`

- `GET /api/v1/execution-environments`  
- `POST /api/v1/execution-environments`  
- `GET /api/v1/execution-environments/{id}`  
- `PATCH /api/v1/execution-environments/{id}`  
- `DELETE /api/v1/execution-environments/{id}`  

Representation:

```json
{
  "id": 5,
  "name": "Default EE",
  "description": "Default execution environment",
  "organization_id": 1,
  "image": "quay.io/ansible/awx-ee:latest",
  "credential_id": 10,
  "pull": "missing",
  "managed": true,
  "created_at": "2025-12-01T10:00:00Z",
  "modified_at": "2025-12-08T11:22:00Z"
}
```

---

### 11.8 Credentials and Credential Types

#### Credentials

**Base URL**: `/api/v1/credentials`

- `GET /api/v1/credentials`  
- `POST /api/v1/credentials`  
- `GET /api/v1/credentials/{id}`  
- `PATCH /api/v1/credentials/{id}`  
- `DELETE /api/v1/credentials/{id}`  

Representation (simplified):

```json
{
  "id": 100,
  "name": "Prod SSH Key",
  "description": "SSH key for prod hosts",
  "organization_id": 1,
  "credential_type_id": 1,
  "inputs": {
    "username": "ansible",
    "ssh_key_data": "********"
  },
  "created_at": "2025-06-01T10:00:00Z",
  "modified_at": "2025-12-10T09:10:00Z"
}
```

#### Credential Types

**Base URL**: `/api/v1/credential-types`

- `GET /api/v1/credential-types`  
- `GET /api/v1/credential-types/{id}`  

Representation includes field definitions and options (similar to AWX).

#### Credential Input Sources (optional)

**Base URL**: `/api/v1/credential-input-sources`

Used to define external secret sources (vaults, etc.).

- `GET /api/v1/credential-input-sources`  
- `POST /api/v1/credential-input-sources`  
- `GET /api/v1/credential-input-sources/{id}`  
- `PATCH /api/v1/credential-input-sources/{id}`  
- `DELETE /api/v1/credential-input-sources/{id}`  

---

### 11.9 Inventories, Hosts, Groups

#### Inventories

**Base URL**: `/api/v1/inventories`

- `GET /api/v1/inventories`  
- `POST /api/v1/inventories`  
- `GET /api/v1/inventories/{id}`  
- `PATCH /api/v1/inventories/{id}`  
- `DELETE /api/v1/inventories/{id}`  

Subresources:

- `/api/v1/inventories/{id}/hosts`  
- `/api/v1/inventories/{id}/groups`  
- `/api/v1/inventories/{id}/tree`  
- `/api/v1/inventories/{id}/variable-data` (GET/PUT vars).  

#### Hosts

**Base URL**: `/api/v1/hosts`

- `GET /api/v1/hosts`  
- `POST /api/v1/hosts`  
- `GET /api/v1/hosts/{id}`  
- `PATCH /api/v1/hosts/{id}`  
- `DELETE /api/v1/hosts/{id}`  

Subresources:

- `/api/v1/hosts/{id}/groups`  
- `/api/v1/hosts/{id}/variable-data`  
- `/api/v1/hosts/{id}/facts`  
- `/api/v1/hosts/{id}/job-host-summaries`  

#### Groups

**Base URL**: `/api/v1/groups`

- `GET /api/v1/groups`  
- `POST /api/v1/groups`  
- `GET /api/v1/groups/{id}`  
- `PATCH /api/v1/groups/{id}`  
- `DELETE /api/v1/groups/{id}`  

Subresources:

- `/api/v1/groups/{id}/hosts`  
- `/api/v1/groups/{id}/children`  
- `/api/v1/groups/{id}/variable-data`  
- `/api/v1/groups/{id}/job-host-summaries`  

---

### 11.10 Inventory Sources and Updates

Praetor may treat these similarly to AWX:

- `GET /api/v1/inventory-sources`  
- `POST /api/v1/inventory-sources`  
- `GET /api/v1/inventory-sources/{id}`  
- `PATCH /api/v1/inventory-sources/{id}`  
- `DELETE /api/v1/inventory-sources/{id}`  
- `POST /api/v1/inventory-sources/{id}/sync` – start inventory update.  

Inventory Updates can be modeled as Jobs with `type: "inventory_update"` and `inventory_source_id` linking.

---

### 11.11 Job Templates

**Base URL**: `/api/v1/job-templates`

Represents a reusable definition of a job (equivalent to AWX Job Template).

- `GET /api/v1/job-templates`  
- `POST /api/v1/job-templates`  
- `GET /api/v1/job-templates/{id}`  
- `PATCH /api/v1/job-templates/{id}`  
- `DELETE /api/v1/job-templates/{id}`  

Subresources / actions:

- `POST /api/v1/job-templates/{id}/launch` – launch a job (equivalent to `POST /jobs` with `job_template_id`).  
- `GET /api/v1/job-templates/{id}/jobs` – list jobs created from this template.  
- `/api/v1/job-templates/{id}/credentials`  
- `/api/v1/job-templates/{id}/schedules`  
- `/api/v1/job-templates/{id}/survey-spec` (optional)  

Representation (simplified):

```json
{
  "id": 17,
  "name": "Deploy web tier",
  "description": "Deploys the web stack",
  "organization_id": 1,
  "project_id": 7,
  "playbook": "deploy.yml",
  "inventory_id": 42,
  "execution_environment_id": 5,
  "limit": null,
  "verbosity": 1,
  "timeout_seconds": 7200,
  "ask_inventory_on_launch": true,
  "ask_variables_on_launch": true,
  "ask_limit_on_launch": true,
  "survey_enabled": false,
  "created_at": "...",
  "modified_at": "..."
}
```

---

### 11.12 Ad Hoc Commands

**Base URL**: `/api/v1/ad-hoc-commands`

- `GET /api/v1/ad-hoc-commands`  
- `POST /api/v1/ad-hoc-commands`  
- `GET /api/v1/ad-hoc-commands/{id}`  
- `POST /api/v1/ad-hoc-commands/{id}/cancel`  
- `POST /api/v1/ad-hoc-commands/{id}/relaunch`  
- `GET /api/v1/ad-hoc-commands/{id}/events`  

Internally, Ad Hoc Commands may be represented as Jobs with `type: "ad_hoc_command"`.

---

### 11.13 Workflow Job Templates and Workflow Jobs

#### Workflow Job Templates

**Base URL**: `/api/v1/workflow-job-templates`

- `GET /api/v1/workflow-job-templates`  
- `POST /api/v1/workflow-job-templates`  
- `GET /api/v1/workflow-job-templates/{id}`  
- `PATCH /api/v1/workflow-job-templates/{id}`  
- `DELETE /api/v1/workflow-job-templates/{id}`  

Actions:

- `POST /api/v1/workflow-job-templates/{id}/launch` – start a workflow job.  

Subresources:

- `/api/v1/workflow-job-templates/{id}/nodes`  
- `/api/v1/workflow-job-templates/{id}/schedules`  
- `/api/v1/workflow-job-templates/{id}/labels`  

#### Workflow Jobs

**Base URL**: `/api/v1/workflow-jobs`

- `GET /api/v1/workflow-jobs`  
- `GET /api/v1/workflow-jobs/{id}`  
- `POST /api/v1/workflow-jobs/{id}/cancel`  
- `POST /api/v1/workflow-jobs/{id}/relaunch`  

Internally, workflow jobs can be Jobs with `type: "workflow_job"` and additional workflow-specific state.

---

### 11.14 Workflow Approvals

**Base URL**: `/api/v1/workflow-approvals`

- `GET /api/v1/workflow-approvals`  
- `GET /api/v1/workflow-approvals/{id}`  
- `POST /api/v1/workflow-approvals/{id}/approve`  
- `POST /api/v1/workflow-approvals/{id}/deny`  

Used for manual approval steps in workflows.

---

### 11.15 Schedules

**Base URL**: `/api/v1/schedules`

- `GET /api/v1/schedules`  
- `POST /api/v1/schedules`  
- `GET /api/v1/schedules/{id}`  
- `PATCH /api/v1/schedules/{id}`  
- `DELETE /api/v1/schedules/{id}`  

Schedules may be linked to job templates, workflow templates, or inventory sources.

Praetor MAY provide:

- `GET /api/v1/schedules/preview?rrule=...` – preview RRULE-based schedules.  

---

### 11.16 Notifications and Notification Templates

#### Notification Templates

**Base URL**: `/api/v1/notification-templates`

- `GET /api/v1/notification-templates`  
- `POST /api/v1/notification-templates`  
- `GET /api/v1/notification-templates/{id}`  
- `PATCH /api/v1/notification-templates/{id}`  
- `DELETE /api/v1/notification-templates/{id}`  
- `POST /api/v1/notification-templates/{id}/test` – send test notification.  

#### Notifications

**Base URL**: `/api/v1/notifications`

- `GET /api/v1/notifications`  
- `GET /api/v1/notifications/{id}`  

Notifications are usually generated as a result of Job / Workflow / System events and are read-only over the API.

---

### 11.17 Labels

**Base URL**: `/api/v1/labels`

- `GET /api/v1/labels`  
- `POST /api/v1/labels`  
- `GET /api/v1/labels/{id}`  
- `PATCH /api/v1/labels/{id}`  
- `DELETE /api/v1/labels/{id}`  

Labels are attached to jobs, job templates, workflow templates, etc., and power label filters in the UI.

---

### 11.18 Activity Stream

**Base URL**: `/api/v1/activity-stream`

- `GET /api/v1/activity-stream`  
- `GET /api/v1/activity-stream/{id}`  

Activity stream entries capture system-wide changes (resource creation, updates, deletes) and allow the UI to present audit history.

---

### 11.19 Unified Resources and Bulk Operations

Praetor MAY offer AWX-like “unified” views:

- `GET /api/v1/unified-job-templates` – aggregated view of job & workflow templates.  
- `GET /api/v1/unified-jobs` – aggregated view of jobs of all types.  

Bulk operations:

- `POST /api/v1/bulk/hosts/create` – bulk create hosts.  
- `POST /api/v1/bulk/hosts/delete` – bulk delete hosts.  
- `POST /api/v1/bulk/jobs/launch` – launch 1–25 jobs in stable input order.

Bulk job launch requires an `Idempotency-Key` header (1–128 characters,
`[A-Za-z0-9._:-]`, beginning with an alphanumeric character) and accepts:

```json
{
  "items": [
    {
      "identifier": "deploy-web",
      "unified_job_template_id": 42,
      "name": "Deploy web",
      "extra_vars": {"release": "2026.07.24"},
      "limit": "web-*"
    }
  ]
}
```

Every item passes through the canonical single-job launch checks, including
template execute access, inventory use access, survey/prompt validation,
client-supplied limit handling, relaunch validation, and the simultaneous-run
guard. Results preserve input order. A fully accepted batch returns `201`; a
completed batch containing rejections returns `207`. Missing and unauthorized
templates deliberately share the `not_found_or_forbidden` result so the bulk
surface cannot be used to enumerate inaccessible resources.

The key is scoped to the authenticated user. Replaying an identical request
returns the stored response without creating duplicate jobs. Reusing it for a
different request returns `409`. The request body is capped at 256 KiB and
accepted launches receive individual actor-attributed activity records.

Exact payloads MAY mirror AWX’s, but must be documented in the OpenAPI schema.

---

## Infrastructure and Kubernetes Abstractions

Praetor exposes **Kubernetes-backed capacity** via infrastructure abstractions rather than raw K8s APIs.

### 12.1 Execution Pools (Instance Groups)

Execution Pools are equivalent to AWX **Instance Groups** for the **execution plane**.

**Base URL**: `/api/v1/infra/execution-pools`

Representation:

```json
{
  "id": 10,
  "name": "prod-workers",
  "description": "Prod worker nodes in eu-west-1a",
  "labels": ["prod", "eu-west-1"],
  "kubernetes": {
    "namespace": "praetor-runs",
    "node_selector": {
      "node-role.kubernetes.io/worker": "true",
      "topology.kubernetes.io/zone": "eu-west-1a"
    },
    "tolerations": [
      {
        "key": "workload",
        "operator": "Equal",
        "value": "praetor",
        "effect": "NoSchedule"
      }
    ],
    "priority_class_name": "praetor-executors-high"
  },
  "concurrency_limit": 200,
  "created_at": "2025-12-01T10:00:00Z",
  "modified_at": "2025-12-10T09:30:00Z"
}
```

Endpoints:

- `GET /api/v1/infra/execution-pools` – list pools.  
- `POST /api/v1/infra/execution-pools` – create pool.  
- `GET /api/v1/infra/execution-pools/{id}` – pool details.  
- `PATCH /api/v1/infra/execution-pools/{id}` – update pool.  
- `DELETE /api/v1/infra/execution-pools/{id}` – delete pool (subject to constraints).  
- `GET /api/v1/infra/execution-pools/{id}/instances` – execution instances in the pool.  

Job Templates can reference `execution_pool_id` to control where runs are scheduled.

---

### 12.2 Execution Instances

Execution Instances are equivalent to AWX **Instances** in the execution plane (worker pods/nodes).

**Base URL**: `/api/v1/infra/instances`

Representation:

```json
{
  "id": "pod-praetor-exec-6f8c9b7d4f-xk2fz",
  "name": "praetor-exec-6f8c9b7d4f-xk2fz",
  "pool_id": 10,
  "kubernetes": {
    "namespace": "praetor-runs",
    "node_name": "ip-10-0-1-23.eu-west-1.compute.internal"
  },
  "state": "ready",
  "runs_active": 3,
  "runs_capacity": 10,
  "last_heartbeat_at": "2025-12-10T09:15:32Z",
  "created_at": "2025-12-10T08:55:00Z"
}
```

Endpoints:

- `GET /api/v1/infra/instances`
  - Filters: `pool_id`, `state`.  
- `GET /api/v1/infra/execution-pools/{id}/instances`
  - Instances for a specific pool.  
- `GET /api/v1/infra/instances/{instance_id}`
  - Instance details (read-only).  

These power the “Instance Group → Instances” screens in the UI.

---

### 12.3 Control Plane Instances

Control Plane Instances correspond to AWX “Instances” in the **control plane**; from the UI, you can:

- Enable/disable an instance.  
- Adjust declared CPU/RAM capacity.  

**Base URL**: `/api/v1/infra/control-plane-instances`

Representation:

```json
{
  "id": "cp-1",
  "name": "controller-0",
  "enabled": true,
  "role": "controller",          
  "cpu_cores": 4,
  "memory_mb": 8192,
  "current_cpu_usage": 0.35,
  "current_memory_mb": 4200,
  "kubernetes": {
    "node_name": "ip-10-0-0-10.eu-west-1.compute.internal",
    "pod_name": "praetor-controller-0",
    "namespace": "praetor-control"
  },
  "last_heartbeat_at": "2025-12-10T09:25:00Z",
  "created_at": "2025-12-01T10:00:00Z",
  "modified_at": "2025-12-08T11:22:00Z"
}
```

Endpoints:

- `GET /api/v1/infra/control-plane-instances`
- `GET /api/v1/infra/control-plane-instances/{id}`
- `PATCH /api/v1/infra/control-plane-instances/{id}`

Patch example:

```json
{
  "enabled": false,
  "cpu_cores": 8,
  "memory_mb": 16384
}
```

Optional explicit actions:

- `POST /api/v1/infra/control-plane-instances/{id}/enable`  
- `POST /api/v1/infra/control-plane-instances/{id}/disable`  

These allow the UI to implement AWX-style “toggle instance” and “adjust capacity” controls.

---

## Special Features and Actions

Praetor supports AWX-style special actions via sub-URL endpoints:

- **Copy** resources:
  - `POST /api/v1/projects/{id}/copy`
  - `POST /api/v1/job-templates/{id}/copy`
  - `POST /api/v1/workflow-job-templates/{id}/copy`
  - etc.

- **Launch** actions:
  - `POST /api/v1/job-templates/{id}/launch`
  - `POST /api/v1/workflow-job-templates/{id}/launch`

- **Cancel** actions:
  - `POST /api/v1/jobs/{id}/cancel`
  - `POST /api/v1/workflow-jobs/{id}/cancel`
  - `POST /api/v1/ad-hoc-commands/{id}/cancel`
  - `POST /api/v1/projects/{id}/cancel-sync` (optional).  

- **Relaunch** actions:
  - `POST /api/v1/jobs/{id}/relaunch`
  - `POST /api/v1/workflow-jobs/{id}/relaunch`
  - `POST /api/v1/ad-hoc-commands/{id}/relaunch`  

- **Variable Data** endpoints:
  - `GET/PUT /api/v1/inventories/{id}/variable-data`
  - `GET/PUT /api/v1/hosts/{id}/variable-data`
  - `GET/PUT /api/v1/groups/{id}/variable-data`

These actions allow the UI to match AWX workflows exactly.

---

## Error Handling

Praetor uses standard HTTP status codes and a structured JSON error format.

### Status Codes

- `200 OK` – Success.  
- `201 Created` – Resource created successfully.  
- `202 Accepted` – Async action accepted (e.g., cancel).  
- `204 No Content` – Success, no response body.  
- `400 Bad Request` – Invalid payload or parameters.  
- `401 Unauthorized` – Authentication required/invalid.  
- `403 Forbidden` – Not enough permissions.  
- `404 Not Found` – Resource not found.  
- `405 Method Not Allowed` – HTTP method not supported.  
- `409 Conflict` – Conflicting state (e.g. cancel on finished job).  
- `415 Unsupported Media Type` – Unsupported `Content-Type`.  
- `500 Internal Server Error` – Unexpected error.  

### Error Envelope

Standard error response:

```json
{
  "error": {
    "code": "validation_error",
    "message": "extra_vars is not valid JSON",
    "details": {
      "field": "extra_vars",
      "reason": "Expected object"
    }
  }
}
```

Field-level errors MAY also be returned by including a `details` object with per-field messages.

---

## URL Patterns and Naming

Praetor adheres to predictable patterns:

- **Collection**:  
  - `GET /api/v1/resources`  
  - `POST /api/v1/resources`  

- **Detail**:  
  - `GET /api/v1/resources/{id}`  
  - `PATCH /api/v1/resources/{id}`  
  - `DELETE /api/v1/resources/{id}`  

- **Action**:  
  - `POST /api/v1/resources/{id}/action-name`  

- **Subresource**:  
  - `GET /api/v1/resources/{id}/related-resources`  

ID formats:

- Most IDs are integers.  
- Some IDs (e.g. `run_id`, execution instances) are UUIDs or string identifiers.  

Praetor SHOULD be consistent in naming and ID formats and document them in the OpenAPI schema.

---

## References

- AWX REST API Reference (`/api/v2`) – conceptual baseline.  
- Praetor OpenAPI schema (`praetor_openapi_v1.yaml`) – machine-readable contract.  
- Praetor Architecture & Execution Model – internal design documents.  
- Kubernetes API and CRD Definitions – for controllers and schedulers.  

---

*This document describes the full REST surface expected for Praetor v1 to provide AWX-level UI parity on top of Praetor’s resilient Job/Run/event/log execution model and Kubernetes-native infrastructure.*
