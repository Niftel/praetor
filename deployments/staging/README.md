# Persistent staging environment

This environment is the durable release-candidate proving ground for Praetor.
It is deliberately separate from the disposable `praetor-validation` fixture
and the mutable `praetor-test` development cluster.

## Topology and trust boundaries

| Boundary | Contract |
|---|---|
| Cluster | Dedicated k3d cluster `praetor-staging`; never shared with the local dev or CI fixture clusters |
| Namespace | Application workloads live in `praetor-staging`, with baseline Pod Security admission and a bounded resource quota |
| Ingress | k3s Traefik is reachable on host ports `8080` and `8443`; no router or public DNS exposure is created |
| Storage | Every k3s server and agent mounts `~/.local/share/praetor/staging/storage`; the local-path provisioner writes PVC data below that protected host directory |
| Credentials | Provisioning accepts no application secrets. Later deployment work must use pre-existing Kubernetes Secret references |
| Identities | Production LDAP accounts, production certificates, and production workload identities are prohibited |

This is a single-workstation staging topology, suitable for release-candidate
acceptance and recovery exercises. It is not a production high-availability
design. The default capacity policy permits 4 requested CPU cores, 8 GiB of
requested memory, 16 PVCs, and 100 GiB of requested storage.

## Provisioning

Review the exact, non-mutating plan first:

```sh
make staging-environment-plan
```

Provision or reconcile the environment:

```sh
make staging-environment-provision
```

Provisioning is idempotent. If the cluster already exists it is never deleted
or replaced; the namespace policy is reapplied and all prerequisites are
checked. Override the host data root only when necessary:

```sh
PRAETOR_STAGING_DATA_ROOT=/encrypted/praetor-staging \
  make staging-environment-provision
```

The data root is created with mode `0700`. PVC contents survive workload,
k3d-container, Docker Desktop, and workstation restarts while the PVC and
cluster metadata exist. Deleting a PVC or its namespace invokes the
StorageClass `Delete` reclaim policy and destroys that claim's data. Routine
tests must not delete the staging namespace, cluster, PVCs, or data root.

## Health and recovery

```sh
make staging-environment-status
```

The command fails unless the Kubernetes API is ready, the server and load
balancer containers are running, Traefik is available, the `local-path`
StorageClass exists, the staging namespace policy is present, and a temporary
PVC can bind. The PVC probe is deleted after the check; no application data is
touched.

After Docker Desktop or the workstation restarts, use the existing ordered
cluster lifecycle wrapper with the staging cluster name:

```sh
PRAETOR_K3D_CLUSTER=praetor-staging make local-cluster-start
PRAETOR_K3D_CLUSTER=praetor-staging make local-cluster-recover
```

There is intentionally no staging teardown target. Destruction requires a
separate, explicit operational decision: first archive or verify the storage
root, then delete the k3d cluster manually. Never remove the data root as part
of automated validation.
