# RHEL 9 home lab (Rocky Linux 9)

A 3-node RHEL 9-compatible fleet of **host Docker containers** that Praetor manages
as Ansible target hosts — a realistic managed-node lab for the pushable-pack model.
Rocky Linux 9 (1:1 RHEL 9 rebuild) running systemd, sshd only, **no Ansible/Python
preinstalled** (the Execution Pack brings its own).

## Topology

| Node   | Role            | Reached by Praetor      | Reached by peers |
|--------|-----------------|-------------------------|------------------|
| rocky1 | runner/control  | `host.k3d.internal:2201`| —                |
| rocky2 | managed         | (via rocky1)            | `rocky2:22`      |
| rocky3 | managed         | (via rocky1)            | `rocky3:22`      |

Praetor SSHes to **rocky1** (the runner), pushes the self-contained pack, and
rocky1's bundled Ansible manages all three (rocky1 local; rocky2/rocky3 over SSH by
container name on the `homelab-net` network — the SSH key is pushed to rocky1 at
bootstrap).

## Bring it up

```sh
./up.sh                       # builds the image, (re)creates rocky1/2/3
BASE=https://praetor.localhost/api/v1 TOKEN=<admin-jwt> ./register.sh
```

`up.sh` bakes **stable SSH host keys** (gitignored `hostkeys/`, generated once) so
recreating a node keeps its identity — otherwise the executor pins the old key in
`known_hosts` and refuses the new one as a possible MITM.

## Two environment requirements (host-based targets)

Because the nodes are host containers (not in-cluster pods), two addresses must line
up — both handled by the committed config, but noted here:

1. **Ingestion callback.** The host-runner reports to the executor's
   `HOST_RUNNER_CALLBACK_URL`. It is set to `http://192.168.65.254:8081` (the Docker
   Desktop host IP), reachable from both the nodes (`host.docker.internal`) and
   in-cluster pods (`host.k3d.internal`). **Ingestion must be exposed on the host:**
   `kubectl port-forward --address 0.0.0.0 -n praetor svc/praetor-ingestion 8081:8081`
   (keep it running). See `deployments/helm/praetor-v2/ci/values-k3d-local.yaml`.
2. **Gitea SCM URL.** Projects are fetched from `host.k3d.internal:3002`. `up.sh`
   adds `--add-host host.k3d.internal:192.168.65.254` so the nodes resolve it.

## Credential

Reuses the org-1 `sandbox-machine` Machine credential (user `root`); its public key
is authorized on the nodes via `authorized_keys`. The private key stays in Praetor.

## Tear down

```sh
docker rm -f rocky1 rocky2 rocky3 && docker network rm homelab-net
```
