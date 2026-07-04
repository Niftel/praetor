---
sidebar_position: 2
title: Getting Started
---

# Getting Started

## Prerequisites

- **Docker** + Docker Compose.
- That's it for the control plane. Managed hosts need only **sshd + a POSIX shell + `tar` + `python3`** (glibc — Debian/Ubuntu/RHEL/etc.; Alpine/musl targets are not supported because the pack's CPython is glibc).

## Bring up the stack

```bash
docker compose up -d
```

This starts the Go services, Postgres, NATS, LDAP, the Gitea artifact registry, and Traefik. Traefik routes `*.localhost` hostnames (which resolve to `127.0.0.1` in browsers):

| URL | Service |
|---|---|
| `https://praetor.localhost` | Web UI |
| `https://api.praetor.localhost` | REST API |
| `https://traefik.localhost` | Traefik dashboard |
| `http://gitea.localhost` | Gitea (artifact registry) |
| `http://localhost:3005` | Grafana ([metrics](./operations/observability.md)) |
| `http://localhost:9090` | Prometheus |

HTTPS uses a locally-trusted **mkcert** certificate. If browsers warn, run `mkcert -install` once and regenerate the cert per `deployments/traefik/dynamic/tls.yml`.

## Run your first job

The moving parts of a job:

1. **Organization** — the RBAC boundary everything belongs to.
2. **Inventory** — the hosts to target. Mark one host as the *runner host* (`is_runner_host`); the pack is pushed there and the play runs from it.
3. **Machine credential** — a username + SSH **private** key + become settings. You install the matching **public** key on the hosts (see below). See [Credentials](./concepts/credentials.md).
4. **Job template** — playbook + inventory + credential + which [Execution Pack](./concepts/execution-packs.md) to use.

### Prepare the hosts

Install the automation user + public key + passwordless sudo on each target:

```bash
./scripts/bootstrap-nodes.sh -u ansible -a root node1 node2 node3
# or, for local containers:
./scripts/bootstrap-nodes.sh -u ansible --docker $(docker ps --format '{{.Names}}')
```

Then create a **Machine credential** in Praetor holding the matching **private** key.

### Launch

Create a job template pointing at your inventory + credential, then launch it — from the UI, or the API:

```bash
curl -X POST https://api.praetor.localhost/api/v1/jobs \
  -H "Authorization: Bearer $PRAETOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"unified_job_template_id": 4}'
```

The executor SSHes to the runner host, pushes the Execution Pack, and runs the play. Watch it live in the UI (events + streamed stdout), or via the [API](./api/authentication.md).

:::tip
Get a `$PRAETOR_TOKEN` by creating a [personal access token](./api/authentication.md) in the UI (**API Tokens**).
:::

## Next steps

- [Execution Packs](./concepts/execution-packs.md) — how the pushed runtime is built and delivered.
- [Workflows](./concepts/workflows.md) — chain templates into DAGs with approvals.
- [Operations](./operations/observability.md) — metrics, resilience, retention, cancellation.
