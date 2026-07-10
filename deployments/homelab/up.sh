#!/usr/bin/env bash
# Spin up (or recreate) the RHEL 9 (Rocky) home lab: 3 nodes as host Docker
# containers on a shared network, reachable from the k3d executor via
# host.k3d.internal:<published-port>, and from each other by container name.
#
# Reproducible + idempotent: safe to re-run. Registration into Praetor
# (inventory/hosts/credential/template) is done separately via the API.
set -euo pipefail

IMAGE=praetor-rocky9:homelab
NET=homelab-net
NODES=3
# Docker Desktop host gateway — the address the Gitea SCM URL (host.k3d.internal)
# and the ingestion callback both live behind. Rocky containers resolve
# host.docker.internal to this; we alias host.k3d.internal to it too so the
# host-runner's project archive fetch (http://host.k3d.internal:3002/...) resolves.
HOST_IP=192.168.65.254

cd "$(dirname "$0")"

# Stable host keys (gitignored) so a node keeps its SSH identity across rebuilds —
# otherwise the executor pins the old key and refuses the new one as a MITM.
mkdir -p hostkeys
[ -f hostkeys/ssh_host_ed25519_key ] || ssh-keygen -q -t ed25519 -f hostkeys/ssh_host_ed25519_key -N ""
[ -f hostkeys/ssh_host_rsa_key ]     || ssh-keygen -q -t rsa -b 2048 -f hostkeys/ssh_host_rsa_key -N ""
[ -f hostkeys/ssh_host_ecdsa_key ]   || ssh-keygen -q -t ecdsa -f hostkeys/ssh_host_ecdsa_key -N ""

docker build -q -t "$IMAGE" . >/dev/null
docker network inspect "$NET" >/dev/null 2>&1 || docker network create "$NET" >/dev/null

for n in $(seq 1 "$NODES"); do
  name="rocky$n"
  port="220$n"
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker run -d --name "$name" --hostname "$name" --network "$NET" \
    --privileged --cgroupns=host \
    --add-host "host.k3d.internal:$HOST_IP" \
    -p "$port:22" "$IMAGE" >/dev/null
  echo "started $name (ssh: host.k3d.internal:$port -> 22)"
done

# Wait for sshd on every node.
for n in $(seq 1 "$NODES"); do
  name="rocky$n"
  for _ in $(seq 1 20); do
    docker exec "$name" systemctl is-active sshd >/dev/null 2>&1 && break
    sleep 1
  done
  echo "$name sshd: $(docker exec "$name" systemctl is-active sshd 2>&1)"
done
