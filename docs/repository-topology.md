# Repository Topology and Ownership

Praetor is developed as a poly-repo platform. This repository is the platform
integration repository and currently owns the control-plane API, the web UI,
the database schema, host-side tooling, deployment composition, and platform
tests. Runtime services and reusable Go modules are versioned independently.

This document records the intended boundary. A directory being convenient to
place here is not, by itself, a reason for this repository to own it.

## This repository owns

| Area | Source | Responsibility |
| --- | --- | --- |
| Control-plane API | `cmd/api`, `services/api` | External REST API, authentication, authorization enforcement, and resource management |
| Database lifecycle | `cmd/migrator`, `db/migrations` | The ordered platform schema and bootstrap data |
| Web control plane | `web` | Operator and user-facing UI; currently released with the platform |
| Host-side tooling | `cmd/host-runner`, `cmd/execpack`, `cmd/packbuilder` | Checkpointed host execution and execution-pack construction |
| Platform composition | `docker-compose.yml`, `deployments` | A compatible full-platform assembly for development and deployment |
| Platform verification | `tests` | Cross-service contracts, end-to-end behavior, and resilience claims |

The repository is therefore not only the API repository. It is the place where
a compatible Praetor platform is assembled and verified. A future extraction of
the API or UI should first replace the integration responsibilities they carry.

## Deployable service repositories

These repositories are expected to be independently buildable, testable, and
releasable. For local source development they are normally checked out beside
this repository.

| Repository | Responsibility |
| --- | --- |
| `praetordev/scheduler` | Claims pending work and advances scheduling state |
| `praetordev/reconciler` | Recovers and reconciles runs toward executor truth |
| `praetordev/executor` | Dispatches and supervises execution on targets |
| `praetordev/ingestion` | Accepts events, logs, heartbeats, and executor results |
| `praetordev/consumer` | Consumes asynchronous platform events and applies reactions |

## Shared module repositories

Shared modules fall into four broad groups:

- Contracts and domain vocabulary: `models`, `events`, `launch`, `packspec`
- Infrastructure adapters: `db`, `eventbus`, `objectstore`, `metrics`, `hostconn`
- Security: `crypto`, `credentials`, `runtoken`, `rbac`
- Application utilities: `env`, `plog`, `render`, `registry`, `notify`, `store`

The authoritative module/repository/version/owner inventory is
`sharedModules` in `platform-compatibility.yaml`. `make shared-module-health`
checks local sibling sources; `make shared-module-health-remote` downloads and
checks the exact declared module versions, including Go pseudo-versions.

Shared modules must not import deployable services. Cross-process payloads need
explicit compatibility rules; ordinary implementation details should not be
promoted into shared modules merely to avoid a small amount of duplication.

## Development modes

Praetor supports two deliberately different dependency modes.

### Released-dependency mode

CI and an isolated checkout resolve the versions in `go.mod`. This is the test
that the repository is independently buildable. `go.work` must not be required.

```bash
GOWORK=off go build ./...
GOWORK=off go test ./...
```

### Local source mode

A developer-owned, ignored `go.work` can point at sibling checkouts. Docker
Compose likewise builds deployable services from `../scheduler`, `../executor`,
and the other service directories. This mode is for coordinated source changes,
not for defining a release.

```text
workspace/
  praetor/
  scheduler/
  reconciler/
  executor/
  ingestion/
  consumer/
  ...shared modules...
```

The compatible released component and contract versions are declared in
`platform-compatibility.yaml`. Run `make compat-check` after changing a contract,
component set, or database migration. Adjacent branch heads remain local
overrides and do not alter that declaration.

## Ownership rules

1. The executor and host runner own execution truth. The API presents and
   converges that truth; it does not infer a final outcome from control-plane
   availability.
2. This repository owns the shared database schema until an explicit schema
   ownership mechanism replaces it. Services may propose migrations here but
   must remain compatible with the declared platform version.
3. Unit and service integration tests live with their implementation. Tests in
   this repository exercise public contracts or multi-service behavior.
4. A deployable service must build and test without sibling source checkouts.
5. A shared module receives a breaking major version only with a documented
   consumer migration plan.
6. Platform composition pins compatible per-component versions. `latest` and local
   sibling branches are development conveniences, not release specifications.

## Known transition items

- Authorization decisions use `github.com/praetordev/rbac/v4`; Praetor-specific
  capability vocabulary, assignment persistence, and handler contracts live in
  this repository under `pkg/accesscontrol`.
- Several tests under `tests` still describe extracted services. They need to be
  classified as platform contract tests or moved to the owning service.
- The UI client is handwritten. The external API contract should eventually
  generate its request and response types.
- Docker Compose uses sibling build contexts, while releases need an explicit
  component compatibility manifest and pinned images.
