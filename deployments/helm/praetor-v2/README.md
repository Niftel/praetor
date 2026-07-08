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

Praetor authenticates users against an **LDAP/Active Directory** directory the
AAP/AWX way: bind at login, then map **LDAP groups → Praetor roles**. There is no
background sync and no first-admin bootstrap flow — a user's orgs/teams/flags are
resolved from their group membership on login. Configure it with:

```sh
helm install praetor deployments/helm/praetor-v2 -n praetor \
  --set ldap.enabled=true \
  --set-file ldap.config=my-ldap.yaml \
  --set secrets.ldapBindPassword=<bind-password>   # stays in the Secret, never in the file
```

`ldap.config` is rendered verbatim to `/etc/praetor/ldap.yaml`; point it at your
real endpoint (see `../../ldap/ldap-config.yaml` for a full example):

```yaml
server:
  url: "ldaps://ldap.corp.example.com:636"
  bind_dn: "cn=svc-praetor,ou=svc,dc=corp,dc=example,dc=com"
  bind_password_env: PRAETOR_LDAP_BIND_PASSWORD   # sourced from secrets.ldapBindPassword
  insecure_skip_verify: false                     # verify TLS against a real directory
users:
  search_base: "ou=users,dc=corp,dc=example,dc=com"
group_type:
  type: member_of                                 # or member_dn / posix / nested
user_flags_by_group:
  is_superuser: ["cn=praetor-admins,ou=groups,dc=corp,dc=example,dc=com"]
organization_map:                                 # keyed by Praetor org NAME (created on match)
  Engineering:
    admins: ["cn=eng-leads,ou=groups,dc=corp,dc=example,dc=com"]
    users:  ["cn=engineering,ou=groups,dc=corp,dc=example,dc=com"]
    remove_users: true                            # revoke when the user leaves the group
team_map:                                         # keyed by team NAME
  platform: { organization: Engineering, users: ["cn=platform,ou=groups,..."], remove: true }
```

> The `ldap` container in `docker-compose.yml` (osixia/openldap) is a **local-dev
> mock only** — a stand-in for a real directory. Do not model production on it;
> point `ldap.config` at your own LDAP/AD instead. `insecure_skip_verify: true` in
> the demo config exists solely because the mock speaks plaintext `ldap://`.

### Break-glass local superuser

Independent of LDAP, a user row with a local password and `ldap_dn NULL` always
authenticates locally and is **never** LDAP-managed — so a misconfigured or
unreachable directory can't lock you out. It's also the way to get a first
superuser before any LDAP group grants one. Enable it in the chart:

```sh
helm install praetor deployments/helm/praetor-v2 -n praetor \
  --set bootstrapAdmin.enabled=true \
  --set bootstrapAdmin.username=admin \
  --set bootstrapAdmin.password=<strong-password>   # or supply PRAETOR_BOOTSTRAP_ADMIN_PASSWORD via secrets.existingSecret
```

The migrator creates (or, on upgrade, resets to the configured password) this local
superuser — the chart is the source of truth, so a redeploy always restores access.
A same-named LDAP user is never overwritten. Log in with the username/password
above; rotate it by changing the value and upgrading.

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
