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
| scheduler | Deployment + optional Service | mTLS claim listener Service exists only when secrets integration is enabled |
| consumer, reconciler | Deployment | no HTTP server; consumer scales horizontally |
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

## Praetor Secrets Service

The provider-independent secrets service is opt-in. The chart does not accept
certificate or private-key contents in values and does not generate a private
CA. Create two Kubernetes Secrets from files issued by your workload PKI:

```sh
kubectl -n praetor create secret generic praetor-scheduler-identity \
  --from-file=ca.crt=secrets-service-ca.pem \
  --from-file=tls.crt=scheduler-client.pem \
  --from-file=tls.key=scheduler-client-key.pem \
  --from-file=claim.crt=scheduler-claim-server.pem \
  --from-file=claim.key=scheduler-claim-server-key.pem \
  --from-file=executor-ca.crt=executor-workload-ca.pem

kubectl -n praetor create secret generic praetor-executor-identity \
  --from-file=ca.crt=scheduler-claim-ca.pem \
  --from-file=secrets-ca.crt=praetor-secrets-server-ca.pem \
  --from-file=tls.crt=executor-client.pem \
  --from-file=tls.key=executor-client-key.pem
```

The scheduler client certificate requires the URI SAN
`spiffe://<trust-domain>/workload/praetor-scheduler`. The executor certificate
requires exactly one URI SAN,
`spiffe://<trust-domain>/workload/praetor-executor/<instance>`. The claim-server
certificate must cover the in-cluster DNS name
`<release>-scheduler.<namespace>.svc` (and any fully-qualified cluster suffix
used by the executor).

The executor Secret deliberately contains two server trust roots: `ca.crt`
verifies the scheduler claim listener, while `secrets-ca.crt` verifies Praetor
Secrets. They may contain the same CA, but keeping separate keys avoids silently
coupling two independent TLS boundaries. The executor presents `tls.crt` and
`tls.key` to both services.

Enable the integration without placing key material on the Helm command line:

```sh
helm upgrade praetor deployments/helm/praetor-v2 -n praetor \
  --set secretsService.enabled=true \
  --set secretsService.url=https://praetor-secrets.praetor-secrets.svc:8443 \
  --set secretsService.trustDomain=praetor.internal \
  --set secretsService.schedulerIdentitySecret=praetor-scheduler-identity \
  --set secretsService.executorIdentitySecret=praetor-executor-identity
```

Static Kubernetes Secrets provide one executor identity, so this chart mode
requires `executor.replicas=1`. Scaling requires a workload-identity issuer that
mounts a different certificate into each StatefulSet pod; sharing one private
key across replicas would let any replica resolve credentials claimed by another.
The chart fails rendering instead of allowing that weakened configuration.

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
(`registry: ""`) rather than pulling from a registry. Add a break-glass login
with `--set bootstrapAdmin.enabled=true --set bootstrapAdmin.username=admin
--set bootstrapAdmin.password=admin`.

Manage an existing local cluster through the repository lifecycle commands,
not by starting or stopping its Docker containers individually:

```sh
make local-cluster-status
make local-cluster-stop
make local-cluster-start
```

If Docker Desktop was restarted while k3s was shutting down, `server-0` can be
left stopped while `serverlb` loops because Docker restart policies do not model
that dependency. Recover the cluster as one unit:

```sh
make local-cluster-recover
```

Recovery temporarily disables the load balancer's restart policy and stops the
cluster through k3d. It then bypasses k3d's temporary tools-node startup phase,
which can hang on affected Docker Desktop builds, and starts the existing k3s
server before the load balancer. It does not report success until the k3s API is
ready and the load balancer can resolve the server container. The wrapper also
gives k3s 60 seconds to stop and unmount pod volumes before k3d handles the
remaining containers, instead of allowing a one-second Docker Desktop shutdown
to force-kill the server.

k3d commands are bounded to 45 seconds. If k3d starts the containers but hangs
while cleaning up its temporary tools node, the wrapper terminates that command,
removes the orphaned tools node, and accepts recovery only after the independent
DNS and Kubernetes API readiness checks pass.

`scripts/update-local-cluster.sh` runs this readiness/recovery check before any
image build or import.

### Optional: LDAP (in-cluster mock)

Exercise the AAP-style group→role mapping against a seeded OpenLDAP mock that
runs *inside* the cluster (a stand-in for a real directory):

```sh
kubectl -n praetor create configmap openldap-seed \
  --from-file=bootstrap.ldif=deployments/ldap/bootstrap.ldif
kubectl apply -f deployments/helm/praetor-v2/ci/openldap.yaml

helm upgrade praetor deployments/helm/praetor-v2 \
  -f deployments/helm/praetor-v2/ci/values-k3d.yaml \
  -f deployments/helm/praetor-v2/ci/values-k3d-ldap.yaml -n praetor
```

`values-k3d-ldap.yaml` points `ldap.config` at the in-cluster `praetor-openldap`
Service. The purpose-named demo accounts all use the demo-only password
`praetor123`:

| LDAP username | Expected Praetor access |
|---|---|
| `demo-admin` | Full platform administrator |
| `demo-auditor` | Read-only platform auditor |
| `demo-operator` | Engineering organization and non-admin `backend-team` member |
| `demo-denied` | Login rejected because it belongs to no allowed LDAP group |

Organizations and teams are created from group membership on first login.

### Optional: Gitea pack registry (external)

The executor pulls Execution Packs anonymously from a Gitea generic registry
(kept external by design). Point the chart at any reachable Gitea that has the
packs published — e.g. a host-side Gitea reached from k3d via `host.k3d.internal`:

```sh
helm upgrade praetor deployments/helm/praetor-v2 \
  -f deployments/helm/praetor-v2/ci/values-k3d.yaml \
  --set gitea.internalUrl=http://host.k3d.internal:3002 --set gitea.owner=praetor -n praetor
kubectl -n praetor rollout restart statefulset/praetor-executor   # pick up the ConfigMap change
```

The executor fetches `{gitea.internalUrl}/api/packages/{owner}/generic/execpack-<pack>/current/<pack>-linux-<arch>.tar.gz`
with no token (only publishing needs one).

### Optional: URLs via ingress + trusted TLS

By default the k3d values disable ingress (reach the UI with a port-forward).
To serve the stack on real URLs (`https://praetor.localhost`) through k3s's
built-in Traefik — with a browser-trusted cert, exactly like the compose env:

```sh
# 1. Map host 80/443 into the cluster (at create time, or edit a live cluster):
k3d cluster edit praetor-test \
  --port-add "80:80@loadbalancer" --port-add "443:443@loadbalancer"

# 2. Load the machine's mkcert cert as a TLS secret (private key is NOT in git).
#    Run `mkcert -install` once so the local CA is trusted by your browser.
kubectl -n praetor create secret tls praetor-localhost-tls \
  --cert=deployments/traefik/certs/localhost.pem \
  --key=deployments/traefik/certs/localhost-key.pem

# 3. Enable ingress with the mkcert cert:
helm upgrade praetor deployments/helm/praetor-v2 \
  -f deployments/helm/praetor-v2/ci/values-k3d.yaml \
  -f deployments/helm/praetor-v2/ci/values-k3d-ingress.yaml -n praetor
```

Then open `https://praetor.localhost/login` — no cert warning, because the cert
is signed by the mkcert CA already in your trust store. The cert is
machine-specific; regenerate it (covering `praetor.localhost` and
`*.praetor.localhost`) with the `mkcert` command in
`deployments/traefik/dynamic/`. Without a `secretName`, Traefik falls back to
its self-signed default cert (works, but the browser warns).
