# Praetor Helm Chart — Design Blueprint

> **Status: design artifact, not an implementation.** The existing chart under
> [`praetor/`](praetor/) is deprecated and unsupported (see its README). This
> document is the source-grounded plan for a *replacement* chart. Building it is a
> genuine project and must be validated against a real cluster — nothing here has
> been. Until it exists, deploy with [`docker-compose.yml`](../../docker-compose.yml),
> which remains the source of truth for topology.
>
> Produced by a Fable-5 design consult grounded in: `docker-compose.yml`,
> `build/package/*.Dockerfile`, `cmd/*/main.go`, `pkg/transport/nats/bus.go`,
> `pkg/objectstore/jetstream.go`, `services/scheduler/core/scheduler.go`,
> `web/nginx.conf`, `deployments/nats/nats.conf`, `deployments/gitea/Dockerfile`.
> Line-number references reflect the tree at the time of the consult and may drift.

## 1. Service inventory → workload mapping

### Stateless Deployments

| Service | Kind | Replicas | Multi-replica safety (verified in source) |
|---|---|---|---|
| **api** | Deployment | 2+ | Pure HTTP over Postgres; JWT is stateless. Safe. |
| **ingestion** | Deployment | 2+ | Stateless HTTP; log blobs live in the NATS JetStream **object store** (`pkg/objectstore/jetstream.go`, bucket `PRAETOR_LOGS`), not local disk — so ingestion itself carries no volume. Safe. |
| **scheduler** | Deployment | 1 (2 allowed for HA) | **Multi-replica-safe by design**: dispatch claims with `FOR UPDATE SKIP LOCKED` (`scheduler.go:124`), the outbox relay uses `SKIP LOCKED` + stale-claim recovery + JetStream dedup window, and the code comment at `scheduler.go:391` explicitly says "safe to run from multiple schedulers". The heartbeat check and `JOB_RETENTION_DAYS` pruning are idempotent time-predicate UPDATE/DELETEs. Run 1 by default (extra replicas only add redundant polling), allow 2 for HA. |
| **consumer** | Deployment | 2+ | `JS.QueueSubscribe(..., QueueGroupConsumer, nats.Durable(...))` for both events and log chunks (`bus.go:234,265`) — queue-group semantics, natively horizontally scalable. |
| **reconciler** | Deployment | 1 | Pull-based SSH harvester on a `RECONCILE_INTERVAL` timer. Nothing in it uses SKIP LOCKED-style claiming; run 1 replica. Note the SSH known_hosts problem in §3. |
| **ui** | Deployment | 2+ | nginx + static SPA. Safe. But see the hardcoded `http://api:8080` + Docker DNS resolver `127.0.0.11` in `web/nginx.conf` — must be fixed for K8s (§3, networking). |
| **docs** | Deployment (optional, `docs.enabled=false` by default) | 1 | Static nginx; compose gates it behind a profile — mirror that with a values toggle. |

### StatefulSets + PVCs

| Service | Kind | Storage |
|---|---|---|
| **executor** | **StatefulSet** with `volumeClaimTemplates` | Three pieces of per-replica state from compose: `/var/lib/praetor` (localhost-run WAL, crash recovery per #45 — losing it makes recovery impossible), `/opt/praetor/packs` (extracted packs), and `/home/praetor/.ssh` (TOFU known_hosts). One PVC per replica via volumeClaimTemplates; a pod that restarts re-attaches its own WAL and recovers its own interrupted runs. Also needs `shm_size: 1gb` → `emptyDir{medium: Memory, sizeLimit: 1Gi}` mounted at `/dev/shm`. Scaling: the JetStream pull consumer is shared-durable so N executors pull work natively ("Multiple executors sharing the durable pull work natively", `bus.go:180`). Start with `replicas: 1`; see risk #2 (now resolved). |
| **nats** | StatefulSet (via subchart, §3) | JetStream file store; must be sized ≥ `max_file_store` (10GB in `deployments/nats/nats.conf`) because it holds the run-log object store (default cap 5GiB via `PRAETOR_LOG_STORE_MAX_MB`) plus event/request streams. |
| **postgres** | StatefulSet (via subchart) or external | `pgdata`. |
| **gitea** | Recommend **external prerequisite**, not in-chart (§3). If bundled for demo: StatefulSet, 1 replica, PVC at `/var/lib/gitea` (compose marks the volume `external: true` precisely because it must survive teardown — in K8s use a PVC with `helm.sh/resource-policy: keep`). |

### Jobs
- **migrator** → Helm **pre-install,pre-upgrade hook Job** (`helm.sh/hook-weight` before everything, `hook-delete-policy: before-hook-creation,hook-succeeded`). It replaces every service's `depends_on: migrator: service_completed_successfully`. Note it also *seeds the automation identity* and needs `PRAETOR_SECRET_KEY`; the optional `./keys` one-time import can be an optional Secret mount (`migrator.importKeysSecret`), off by default.

### Not in the chart at all
- **web1/web2/db1/target-host** (demo Ansible targets), **ldap** mock, **traefik** (→ Ingress), **docker-socket-proxy** (exists solely to feed traefik's Docker provider — meaningless in K8s), **buildkitd + packbuilder** (recommend dropping, §3), **prometheus/grafana** (use cluster observability + a `ServiceMonitor` toggle).

## 2. Config & secrets

### Secret (`praetor-secrets`, one Secret, referenced via `envFrom`/`secretKeyRef`)
- `PRAETOR_SECRET_KEY` (32 bytes exactly — validate length in `values.schema.json` or a template `fail`), `PRAETOR_SECRET_KEY_OLD` (rotation), `JWT_SECRET` (api only), `PRAETOR_INTERNAL_TOKEN` (shared by api ↔ ingestion ↔ executor — must match; define **once**), `PRAETOR_LDAP_BIND_PASSWORD` (api, only if LDAP enabled), `DATABASE_URL` (contains the DB password), `GITEA_TOKEN` (only if packbuilder kept).
- Support `existingSecret:` so operators can bring ExternalSecrets/sealed-secrets.
- **Hard-set `PRAETOR_ALLOW_INSECURE_DEFAULTS: "false"` in the chart** (not a value). The compose dev default is `true`; a production chart should make missing secrets fail hard, exactly as the compose comments instruct.

### ConfigMap / plain env
- `NATS_URL`, `INGESTION_URL`, `API_URL`, `GITEA_INTERNAL_URL`, `GITEA_OWNER` — all derived from Service names, computed in `_helpers.tpl` (below), not free-form values.
- `HOST_RUNNER_CALLBACK_URL` — **required value, no default** (§3).
- Tunables: `JOB_RETENTION_DAYS` (scheduler, default 90), `PRAETOR_LOG_STORE_MAX_MB` / `PRAETOR_LOG_STORE_MAX_AGE_DAYS` (ingestion), `RECONCILE_INTERVAL` (reconciler), `EXECUTOR_WORKERS`, `ANSIBLE_FORKS`, `INGESTION_PORT`/`PORT`, `PRAETOR_LOG_FORMAT`/`PRAETOR_LOG_LEVEL` (global, from `pkg/plog`).
- `PRAETOR_LDAP_CONFIG` → mount `deployments/ldap/ldap-config.yaml`-shaped content from a ConfigMap at `/etc/praetor/ldap.yaml` (values: `ldap.enabled`, `ldap.config: {}` rendered to YAML).

### Template-once pattern
In `_helpers.tpl`:
```
praetor.databaseUrl   → external value OR postgres://…@{{ .Release.Name }}-postgresql:5432/praetor
praetor.natsUrl       → nats://{{ .Release.Name }}-nats:4222
praetor.ingestionUrl  → http://{{ include "praetor.fullname" . }}-ingestion:8081
```
DATABASE_URL goes into the shared Secret once; NATS/INGESTION/API URLs into the shared ConfigMap once; every workload does `envFrom: [configMapRef, secretRef]` plus a small per-service env block. `PRAETOR_INTERNAL_TOKEN` is a single Secret key referenced by api, ingestion, and executor — impossible to skew.

## 3. The hard parts

### In-cluster control plane vs. out-of-cluster managed hosts
The cluster runs **only the control plane**. Managed hosts (SSH targets) and the **host-runner** (shipped *inside* the Execution Pack — the executor Dockerfile comment is explicit that the pack is the single source of the daemon) are **not K8s concerns**. The chart's obligations are exactly two:
1. **Egress**: executor and reconciler pods must reach targets on :22. Document it; optionally template an egress NetworkPolicy. No Service/Endpoint objects for targets — they're rows in Postgres (inventory), not cluster resources.
2. **Ingress for callbacks**: `HOST_RUNNER_CALLBACK_URL` must be a URL routable **from the targets** — i.e., the ingestion Ingress host (`https://ingest.<domain>`), never a cluster-internal DNS name. The compose default `host.docker.internal:8090` is the compose analog of this same requirement. Make it a required, validated value and default it to `https://{{ .Values.ingress.ingestionHost }}` when ingress is enabled. This also means ingestion's ingress must be reachable from the managed-host network — call that out in the README/NOTES.txt.

### Pack building (#46): drop it
Recommendation: **exclude packbuilder + buildkitd from the chart.** Reasons from the source: buildkitd is `privileged: true`; packbuilder is compose-coupled beyond just BuildKit — `TRAEFIK_CONTAINER: praetor-traefik`, Gitea host resolution, build add-hosts pinning, and a shared `./build` bind mount. That's a rewrite, not a port. Packs are release artifacts: build them in CI (`make execpack` / packbuilder against a CI BuildKit) and publish to the Gitea generic package registry; the executor only needs `GITEA_INTERNAL_URL` + `GITEA_OWNER` to pull. If someone insists later, the correct K8s shape would be packbuilder Deployment + buildkitd as a **separate** Deployment (its cache PVC and privilege boundary shouldn't share a pod), but don't ship it in v1. Note the executor's `/tmp/build` local-tarball fallback also disappears — registry-only in K8s (acceptable; compose calls it a legacy fallback).

> **Update since consult:** the compose buildkitd gRPC listener is now secured
> with mTLS (a shared CA + daemon/client certs). If a future chart ever does
> bundle buildkitd, carry that mTLS material as a Secret rather than reverting to
> an open listener.

### Gitea / pack registry: external prerequisite
Make Gitea an **external prerequisite** by default: `gitea.internalUrl` + `gitea.owner` values pointing at an existing instance. The in-repo image is demo-grade (sqlite, config bootstrapped onto the volume by `scripts/gitea-volume.sh` — there's no in-image app.ini). Optionally add the **official `gitea-charts/gitea`** as a disabled-by-default subchart (`gitea.bundled.enabled`) for kick-the-tires installs; do NOT port `deployments/gitea/Dockerfile`. The registry outlives releases — pack data loss means executors can't provision (compose protects it with `external: true` for the same reason).

### NATS: official subchart. Postgres: bitnami subchart OR external.
- **NATS**: use the official `nats-io/nats` chart as a dependency. Do not hand-roll — JetStream StatefulSet + clustering + lame-duck shutdown is exactly what it does well. Values must reproduce `deployments/nats/nats.conf`: JetStream enabled, `fileStore.maxSize: 10Gi` (must exceed `PRAETOR_LOG_STORE_MAX_MB` + streams — keep the two values coupled via a comment or a template assert), monitor port 8222 for probes. Single server is fine initially; 3-node R3 later without chart changes.
- **Postgres**: `bitnami/postgresql` subchart (`postgresql.enabled: true`) for self-contained installs, with first-class `externalDatabase.url` (→ RDS/CloudNativePG) as the production recommendation. `DATABASE_URL` helper resolves whichever is active. Compose uses postgres:15 — pin subchart major accordingly.

### Ingress
Compose traefik hosts map to one Ingress (or three) with cert-manager annotations:
- `praetor.<domain>` → ui:80 (was `praetor.localhost`)
- `api.<domain>` → api:8080 (was `api.praetor.localhost`)
- `ingest.<domain>` → ingestion:8081 — **new vs compose**, needed because managed hosts call back (compose exposed :8090 for this)
- `gitea.<domain>` → only if bundled gitea
- `docs.<domain>` → optional

Values: `ingress.className`, `ingress.annotations` (cert-manager cluster-issuer), `ingress.tls.enabled`, per-host overrides. TLS termination at ingress; compose's per-router HTTP→HTTPS redirect becomes an ingress annotation. Alternative worth noting: since the UI's nginx proxies `/api/`, you *could* serve everything off one host — but the SPA is built calling `/api/v1/...` relative, so single-host `praetor.<domain>` with a `/api` path route to the api Service is actually the simplest and avoids CORS; keep `api.<domain>` optional.

### Networking/DNS — hardcoded compose names to fix
All Go code reads names from env with compose-name **defaults only**, so env templating covers them; the chart must always set them explicitly (never rely on defaults):
- `services/api/handlers/jobs.go:287` — fallback `http://ingestion:8081` (set `INGESTION_URL`)
- `services/executor/core/inventory_sync.go:64` — fallback `http://ingestion:8081`
- `cmd/reconciler/main.go:41` — fallback `http://ingestion:8081`
- `cmd/packbuilder/main.go:116` — fallback `http://gitea-host:3000` (moot if dropped)
- `pkg/transport/nats.DefaultURL` / `db.DefaultDSN` — set `NATS_URL`/`DATABASE_URL` everywhere
- **The one real hardcode: `web/nginx.conf`** — `resolver 127.0.0.11` (Docker embedded DNS) and `set $api_upstream http://api:8080` are **baked into the ui image**. In K8s this breaks (no 127.0.0.11; no `api` name). Fix: mount a chart-rendered ConfigMap over `/etc/nginx/conf.d/default.conf` with `resolver kube-dns.kube-system.svc.cluster.local` (or drop the variable-upstream trick entirely — kube Service VIPs are stable, unlike compose container IPs, so a plain `proxy_pass http://<fullname>-api:8080` is fine) — or bypass the nginx proxy and route `/api` at the Ingress. Recommend the ConfigMap override; zero image changes.

## 4. Chart structure & practices

**Single application chart with dependencies** (not an umbrella): `charts/praetor` with `Chart.yaml` dependencies on `nats` (official), `postgresql` (bitnami, condition-gated), `gitea` (gitea-charts, condition-gated, default off). Place at `deployments/helm/praetor-v2` or replace in place since the old one is deprecated.

```
templates/
  _helpers.tpl            # fullname, labels, databaseUrl, natsUrl, ingestionUrl
  configmap.yaml          # shared non-secret env
  secret.yaml             # gated on !existingSecret
  migrator-job.yaml       # pre-install,pre-upgrade hook
  api/{deployment,service,pdb}.yaml
  ingestion/{deployment,service,pdb}.yaml
  scheduler/deployment.yaml
  consumer/deployment.yaml
  reconciler/deployment.yaml
  executor/{statefulset,service}.yaml     # headless svc for the STS
  ui/{deployment,service,configmap-nginx}.yaml
  docs/…                  # gated
  ldap-configmap.yaml     # gated on ldap.enabled
  ingress.yaml
  networkpolicy.yaml      # optional
  servicemonitor.yaml     # optional, /metrics exists (prometheus scrape in compose)
  NOTES.txt               # callback-URL and gitea prerequisites
values.schema.json        # enforce secretKey length, required callbackUrl
```

**values.yaml skeleton shape:**
```yaml
global:
  image: { registry: ghcr.io/…, tag: ""   # defaults to .Chart.AppVersion
         , pullPolicy: IfNotPresent, pullSecrets: [] }
  logFormat: json
  logLevel: info

secrets:
  existingSecret: ""        # if set, all keys below ignored
  secretKey: ""             # 32 bytes, required
  secretKeyOld: ""
  jwtSecret: ""
  internalToken: ""

postgresql: { enabled: true, auth: {...} }   # bitnami passthrough
externalDatabase: { url: "" }                # wins over subchart
nats: { enabled: true, config: { jetstream: { fileStore: { pvc: { size: 12Gi }, maxSize: 10Gi }}}}

gitea:
  internalUrl: ""           # REQUIRED (external registry)
  owner: praetor
  bundled: { enabled: false }

hostRunner:
  callbackUrl: ""           # REQUIRED; targets must reach this

api:        { replicas: 2, resources: {}, extraEnv: [], podAnnotations: {} }
ingestion:  { replicas: 2, logStoreMaxMB: 5120, logStoreMaxAgeDays: 0, resources: {} }
scheduler:  { replicas: 1, jobRetentionDays: 90 }
consumer:   { replicas: 2 }
reconciler: { replicas: 1, interval: 30s }
executor:
  replicas: 1
  workers: 2
  ansibleForks: 1
  persistence:
    jobs:  { size: 5Gi, storageClass: "" }   # /var/lib/praetor
    packs: { size: 10Gi }                    # /opt/praetor/packs
    ssh:   { size: 100Mi }                   # /home/praetor/.ssh
ui:   { replicas: 2 }
docs: { enabled: false }
ldap: { enabled: false, config: {}, bindPassword: "" }
ingress: { enabled: true, className: nginx, host: praetor.example.com,
           apiHost: "", ingestionHost: ingest.example.com,
           annotations: {}, tls: { enabled: true } }
metrics: { serviceMonitor: { enabled: false } }
networkPolicy: { enabled: false }
```

**Probes** (from compose healthchecks + source):
- ingestion: HTTP GET `/health` :8081 (exists, `cmd/ingestion/main.go:117`)
- api: HTTP GET `/api/v1/ping` :8080 (**there is no `/health` on the api** — `router.go:62`; either use ping or add /health upstream)
- ui/docs: HTTP GET `/` :80
- nats: subchart handles `/healthz` :8222
- scheduler/consumer/reconciler/executor: **no HTTP servers** — if a metrics port exists use TCP/HTTP probe on it, else start with no probe. Don't invent probes that will flap.

**Other practices:** PDBs (`minAvailable: 1`) for api/ingestion/ui only; migrator hook Job as above; `image.tag` defaults to `.Chart.AppVersion` shared by all seven service images with per-service `image.repository` overrides; **host-runner is explicitly not an image in this chart** — it's pinned inside the pack spec (`host_runner` field, checksum-verified), document that in the README so nobody "helpfully" adds it. Pods: `runAsNonRoot`, uid 1000 (`praetor` user exists in the executor image; note the executor image's gosu/entrypoint — check `executor_entrypoint.sh` expectations when setting securityContext, since it currently starts as root and drops privileges).

## 5. Explicit non-goals / out-of-cluster
- Demo targets web1/web2/db1/target-host (compose `demo` profile)
- osixia/openldap mock (chart supports pointing at a real LDAP via `ldap.config`)
- traefik + docker-socket-proxy (compose-specific edge; Ingress replaces both)
- buildkitd + packbuilder (CI concern; see §3)
- prometheus/grafana (`observability` profile → cluster-level stack; chart offers only ServiceMonitor)
- host-runner binary/image (ships in the Execution Pack)
- `./keys` demo keypair flow (Machine credentials come from the DB; migrator import is an optional one-time Secret)
- Pack build assets under `./build` (registry-only in K8s)

## Top 5 risks/gotchas specific to this codebase

1. **`HOST_RUNNER_CALLBACK_URL` reachability.** The single most likely broken-on-day-one item: host-runner daemons on external machines must reach ingestion *from outside the cluster*. If it's set to a cluster DNS name, pushes/heartbeats silently fail and every remote run drifts into `reconciling`. Make it required, validate it's not `*.svc`/`localhost` in the template, and surface it in NOTES.txt.
2. **Executor `DeleteConsumer` on startup** (`bus.go`): *(RESOLVED upstream — the bind is now idempotent; it only deletes a legacy push consumer and otherwise re-binds the shared pull consumer, verified with executor replicas=2. Executor scaling is no longer gated on this.)* Historically every executor boot deleted the shared durable `praetor-executor` consumer before re-creating it, so with replicas > 1 a starting pod could nuke the consumer out from under a running one. Still start at `replicas: 1` by default; scale deliberately.
3. **Executor/reconciler SSH state.** Compose shares one `praetor-ssh` volume between executor and reconciler for consistent TOFU known_hosts. In K8s that's an RWX volume across two workloads — don't. Give each its own PVC and accept independent TOFU, or (better, upstream) move host-key pinning into Postgres. Also: per-run credential resolution writes ephemeral key files — ensure they land on emptyDir/tmpfs, not the WAL PVC. And the executor's WAL recovery (#45) assumes *the same instance* reattaches its state — StatefulSet stable identity + per-replica PVC is load-bearing; a Deployment with a shared PVC would corrupt/orphan WALs.
4. **NATS is a datastore here, not just a bus.** Run logs live in the JetStream object store (5GiB default cap) plus durable event/request streams under a 10GB `max_file_store`. The nats subchart's PVC must be sized above `max_file_store`, and `PRAETOR_LOG_STORE_MAX_MB` (ingestion env) must stay below it — two values in different places that can silently diverge; add a template-level check. Losing the NATS PVC loses run logs and unreplayed events.
5. **Baked-in compose DNS in the ui image** (`web/nginx.conf`: `resolver 127.0.0.11`, `http://api:8080`). This isn't env-overridable — it must be a ConfigMap override of `default.conf` (or Ingress-path routing for `/api`). Related second-order item: every Go service falls back to compose hostnames (`ingestion:8081`, `gitea-host:3000`, `db`, `nats`) when env is unset, which *masks* missing env in a namespace that happens to have similarly-named services — set every URL env explicitly in the chart and set `PRAETOR_ALLOW_INSECURE_DEFAULTS=false` so misconfiguration fails loudly instead of running on the dev key.

Honorable mention: the Gitea pack registry is deliberately teardown-proof in compose (`external: true` volume + backup scripts) — preserve that intent with `helm.sh/resource-policy: keep` on its PVC if bundled, or better, keep it external entirely.
</content>
</invoke>
