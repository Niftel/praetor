# Praetor Helm chart

Deploys the Praetor **control plane** (api, ingestion, scheduler, consumer,
reconciler, executor, ui) to Kubernetes, with optional bundled PostgreSQL and
NATS/JetStream datastores. This is the validated replacement for the deprecated
chart under [`../praetor/`](../praetor/); the design rationale lives in
[`../CHART-DESIGN.md`](../CHART-DESIGN.md).

> **Scope.** The cluster runs the control plane only. Managed target hosts and the
> host-runner are **out of cluster** by design — they're inventory rows and live
> inside the Execution Pack, not K8s objects. The chart's external touch-points are
> SSH egress to targets and an ingestion callback endpoint (`hostRunner.callbackUrl`).

## Layout

| Workload | Kind | Notes |
|---|---|---|
| api, ingestion, ui | Deployment + Service | HTTP; api probes `/api/v1/ping`, ingestion `/health` |
| scheduler, consumer, reconciler | Deployment | no HTTP server; scheduler/consumer scale horizontally |
| executor | **StatefulSet** | per-replica PVCs (WAL `/var/lib/praetor`, packs, `~/.ssh`) + `/dev/shm`; safe to scale >1 |
| migrator | Job (revisioned) | runs schema migrations; services gate on it via an init container |
| postgresql, nats | StatefulSet | bundled datastores (optional; override with external) |

Ordering is handled without Helm hooks: the migrator Job waits for the DB, and
every schema-dependent service has a `wait-for-migrations` init container that
blocks until the `organizations` table exists.

## Quick start (bundled datastores)

```sh
helm install praetor deployments/helm/praetor-v2 \
  --namespace praetor --create-namespace \
  --set image.registry=<your-registry> \
  --set secrets.secretKey=<32-byte-key> \
  --set secrets.jwtSecret=<jwt-secret> \
  --set secrets.internalToken=<shared-internal-token> \
  --set gitea.internalUrl=http://<gitea>:3000 \
  --set hostRunner.callbackUrl=https://ingest.<domain>
```

The seven service images must be reachable from the cluster (built + pushed to
`image.registry`, tagged `image.tag` or the chart's `appVersion`).

## Production notes

- **Secrets.** `secrets.secretKey` must be exactly 32 bytes; the chart refuses to
  render without it unless `allowInsecureDefaults=true`. Bring your own Secret with
  `secrets.existingSecret`. `PRAETOR_INTERNAL_TOKEN` is defined once and shared by
  api/ingestion/executor.
- **External datastores** (recommended): set `database.external.url` and
  `nats.external.url` to disable the bundled StatefulSets. For a managed Postgres/NATS
  via official subcharts instead, see CHART-DESIGN.md §3.
- **Callback URL.** `hostRunner.callbackUrl` must be routable *from your target
  hosts* (an external ingress address), never a cluster-internal `*.svc` name, or
  remote runs drift into `reconciling`.
- **Log store coupling.** `ingestion.logStoreMaxMB` must stay below the NATS
  `maxFileStore` — the chart enforces this at render time (JetStream is the run-log
  datastore here, not just a bus).
- **Ingress** is off by default. Enable with `ingress.enabled=true` and set `host`
  (ui) + `ingestionHost` (callbacks). The ui pod proxies `/api/` to the api Service.

## Authentication

Praetor authenticates users against an **LDAP/Active Directory** directory — that
is the intended production model, and it's why there is no built-in first-admin
bootstrap: users, organizations, and teams sync in from the directory, so the
first real login "just works". Configure it with:

```sh
helm install praetor deployments/helm/praetor-v2 -n praetor \
  --set ldap.enabled=true \
  --set-file ldap.config=my-ldap.yaml \
  --set secrets.ldapBindPassword=<bind-password>   # stays in the Secret, never in the file
```

`ldap.config` is rendered verbatim to `/etc/praetor/ldap.yaml`; point it at your
real endpoint (see `../../ldap/ldap-config.yaml` for the full shape):

```yaml
server:
  url: "ldaps://ldap.corp.example.com:636"
  bind_dn: "cn=svc-praetor,ou=svc,dc=corp,dc=example,dc=com"
  bind_password_env: PRAETOR_LDAP_BIND_PASSWORD   # sourced from secrets.ldapBindPassword
  start_tls: false
  insecure_skip_verify: false                     # verify TLS against a real directory
users:         { search_base: "ou=users,dc=corp,dc=example,dc=com", ... }
organizations: { enabled: true, search_base: "...", ... }
teams:         { enabled: true, search_base: "...", ... }
```

> The `ldap` container in `docker-compose.yml` (osixia/openldap) is a **local-dev
> mock only** — a stand-in for a real directory. Do not model production on it;
> point `ldap.config` at your own LDAP/AD instead. `insecure_skip_verify: true` in
> the demo config exists solely because the mock speaks plaintext `ldap://`.

**Demo / kick-the-tires without LDAP.** With `ldap.enabled=false` there is no auth
source, so seed one superuser directly (the only case that needs it):

```sh
HASH=$(go run ./cmd/genhash)   # bcrypt hash of the literal "password"
kubectl -n praetor exec -it praetor-postgresql-0 -- psql -U postgres -d praetor -c \
  "INSERT INTO users (username,password_hash,email,first_name,last_name,is_superuser)
   VALUES ('admin','$HASH','admin@example.com','Admin','User',true);"
# then log in as admin / password — change it immediately
```

## Local validation (k3d)

The chart is smoke-tested end-to-end on k3d with locally-built images:

```sh
k3d cluster create praetor-test
for s in api ingestion scheduler consumer reconciler executor ui migrator; do
  docker build -f build/package/Dockerfile.$s -t praetor-$s:dev .   # ui uses web/Dockerfile
done
k3d image import -c praetor-test praetor-{api,ingestion,scheduler,consumer,reconciler,executor,ui,migrator}:dev
helm install praetor deployments/helm/praetor-v2 -f deployments/helm/praetor-v2/ci/values-k3d.yaml \
  -n praetor --create-namespace
```

All nine service pods reach Ready; `ci/values-k3d.yaml` imports images bare
(`registry: ""`) rather than pulling from a registry.
