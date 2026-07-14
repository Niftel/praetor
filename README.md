# Praetor Backend

Praetor is a Kubernetes-native automation platform designed as a resilient, scalable, and API-compatible alternative to Ansible Tower/AWX.

## Architecture

Praetor consists of decoupled microservices communicating via a PostgreSQL database:

1.  **API Service**: REST API (`/api/v1`) for User/UI interaction.
2.  **Scheduler Service**: Polling service that transitions `pending` Jobs to `ExecutionRuns`.
3.  **Controller Service**: Kubernetes Operator-like service that watches `ExecutionRuns` and launches K8s Jobs.
4.  **Ingestion Service**: High-throughput endpoint (`/api/v1/runs/{id}/events`) for receiving Ansible log streams.

## Directory Structure

- `cmd/`: Entrypoints for each service.
- `pkg/`: Shared libraries (`models`, `db`).
- `services/`: Service-specific logic (`api`, `scheduler`, `controller`, `ingestion`).
- `db/migrations/`: SQL migration files.
- `docs/`: Project documentation and OpenAPI specs.

## Getting Started

### Prerequisites

- Go 1.25+
- PostgreSQL 13+
- Kubernetes Cluster (Minikube, Kind, or remote) - *Optional for API/Scheduler dev*
- `migrate` CLI tool (golang-migrate)

### Installation

1.  Clone the repository.
2.  Initialize the database:
    ```bash
    createdb praetor
    make migrate-up
    ```
3.  Build all services:
    ```bash
    make build
    ```

### Running

This repo is the **API service** plus shared tooling (migrator, host-runner, pack
builder). The other services — scheduler, ingestion, consumer, executor, and
reconciler — live in their own repos at `github.com/praetordev/<service>`.

Run just the API:

```bash
make run-api
```

Or bring up the full multi-service stack (builds the sibling service repos, which
are expected to be checked out alongside this one):

```bash
make up        # docker-compose
# or
make dev-k8s   # build images, load into k3d/kind, helm install
```

### Verification

Check API health:
```bash
curl http://localhost:8080/api/v1/ping
```

Run integration tests:
```bash
make test
```
