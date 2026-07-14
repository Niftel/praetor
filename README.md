# Praetor Platform

Praetor is a Kubernetes-native automation platform designed as a resilient, scalable, and API-compatible alternative to Ansible Tower/AWX.

This is Praetor's **platform integration repository**. It owns the control-plane
API, database migrations, web UI, host-side tooling, deployment composition, and
cross-service tests. Scheduler, reconciler, executor, ingestion, and consumer
are independently versioned repositories.

See [Repository Topology and Ownership](docs/repository-topology.md) for the
current repository boundaries and development modes.
The released component set is declared in
[`platform-compatibility.yaml`](platform-compatibility.yaml); validate it with
`make compat-check`.

With the extracted services checked out beside this repository, run
`make workspace-health` to vet, build, and test every service independently with
`go.work` disabled.

Release candidates are promoted with the procedure in
[`docs/releasing.md`](docs/releasing.md). `make release-preflight` deliberately
refuses a manifest still marked as development.

Canonical cross-service JSON fixtures live under `tests/contracts`; run
`make contract-test` to verify the released shared event types against them.

## Architecture

Praetor consists of decoupled services coordinated through PostgreSQL and NATS:

1. **API**: REST API (`/api/v1`) for the UI and API clients.
2. **Scheduler**: Claims pending work and advances scheduling state.
3. **Executor**: Dispatches and supervises self-contained execution on targets.
4. **Ingestion**: Receives executor events, log streams, heartbeats, and results.
5. **Reconciler**: Recovers interrupted reporting and converges runs to executor truth.
6. **Consumer**: Processes asynchronous platform events and reactions.

## Directory Structure

- `cmd/api/`, `services/api/`: Control-plane API entrypoint and implementation.
- `cmd/host-runner/`: Checkpointed target-side runner.
- `cmd/migrator/`, `db/migrations/`: Platform schema lifecycle.
- `cmd/execpack/`, `cmd/packbuilder/`: Execution-pack tooling.
- `web/`: React/Vite control-plane UI.
- `deployments/`: Helm and development infrastructure.
- `tests/`: Platform contract, integration, and resilience tests.
- `docs/`: Architecture, ownership, API, and product documentation.

## Getting Started

### Prerequisites

- Go 1.26.5+
- PostgreSQL 13+
- NATS 2.10+
- Docker with Compose for the full local stack
- Kubernetes cluster and Helm for Kubernetes development (optional)

### Installation

1.  Clone the repository.
2. Copy `.env.example` to `.env` and set development secrets.
3. Build the API and repository-owned Go commands:

   ```bash
   make build
   ```

### Running

Run the API against an existing PostgreSQL instance:

Run just the API:

```bash
make run-api
```

For the full source-development stack, check out `scheduler`, `reconciler`,
`executor`, `ingestion`, and `consumer` beside this repository, then run:

```bash
make up

# Or build, load, and install the Kubernetes development stack.
make dev-k8s
```

### Verification

Check API health:
```bash
curl http://localhost:8080/api/v1/ping
```

Run repository tests:
```bash
make test
```

CI intentionally runs without `go.work` and resolves released module versions.
For the equivalent independent-build check locally:

```bash
GOWORK=off go build ./...
GOWORK=off go test ./...
```
