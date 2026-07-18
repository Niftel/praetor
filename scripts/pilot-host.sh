#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMMAND="${1:-}"
IMAGE="praetor-pilot-host:rocky9"
NETWORK="praetor-pilot"
TARGET="praetor-pilot-target"
SUBNET="${PRAETOR_PILOT_SUBNET:-172.29.50.0/24}"
ADDRESS="${PRAETOR_PILOT_ADDRESS:-172.29.50.10}"
SKIP_BUILD="${PRAETOR_PILOT_SKIP_BUILD:-0}"
DATA_ROOT="${PRAETOR_PILOT_DATA_ROOT:-$HOME/.local/share/praetor/pilot-host}"
STAGING_PREFIX="k3d-praetor-staging-"

usage() { echo "usage: $0 <plan|provision|status|reset>" >&2; exit 2; }
need() { command -v "$1" >/dev/null 2>&1 || { echo "error: required command '$1' is not installed" >&2; exit 1; }; }
for command in docker jq ssh-keygen; do need "$command"; done
[[ "$COMMAND" =~ ^(plan|provision|status|reset)$ ]] || usage

staging_nodes() {
  docker ps --format '{{.Names}}' | grep -E "^${STAGING_PREFIX}(server|agent)-" || true
}

staging_load_balancer() {
  docker ps --format '{{.Names}}' | grep -Fx "${STAGING_PREFIX}serverlb" || true
}

check_subnet_available() {
  local existing
  existing="$(docker network inspect "$NETWORK" --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}' 2>/dev/null || true)"
  if [[ -n "$existing" && "$existing" != "$SUBNET" ]]; then
    echo "error: network $NETWORK uses $existing, requested $SUBNET" >&2
    exit 1
  fi
  if [[ -z "$existing" ]] && docker network inspect $(docker network ls -q) 2>/dev/null |
      jq -e --arg subnet "$SUBNET" '[.[].IPAM.Config[]?.Subnet] | any(. == $subnet)' >/dev/null; then
    echo "error: requested pilot subnet $SUBNET is already allocated" >&2
    exit 1
  fi
}

status() {
  local nodes node ssh_uid
  docker inspect "$TARGET" --format '{{.State.Running}}' 2>/dev/null | grep -Fxq true || {
    echo "error: pilot target is not running" >&2; exit 1;
  }
  [[ "$(docker inspect "$TARGET" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')" == "$ADDRESS" ]] || {
    echo "error: pilot target does not have expected address $ADDRESS" >&2; exit 1;
  }
  docker exec "$TARGET" /usr/sbin/sshd -t -f /etc/ssh/sshd_config
  docker exec "$TARGET" pgrep -x sshd >/dev/null
  docker inspect "$TARGET" | jq -e '
    .[0].HostConfig.Privileged == false and
    .[0].HostConfig.ReadonlyRootfs == true and
    (.[0].HostConfig.PortBindings | length) == 0 and
    (.[0].HostConfig.Binds | all(endswith(":ro"))) and
    ((.[0].HostConfig.Tmpfs | keys) as $tmpfs | ["/run", "/tmp", "/home/praetor", "/var/lib/praetor", "/opt/praetor", "/usr/local/bin", "/usr/local/share/praetor"] | all(. as $path | $tmpfs | index($path) != null)) and
    ((.[0].HostConfig.Tmpfs["/opt/praetor"] | split(",")) | index("exec") != null) and
    ((.[0].HostConfig.Tmpfs["/usr/local/bin"] | split(",")) | index("exec") != null) and
    (.[0].HostConfig.CapDrop | index("ALL")) != null
  ' >/dev/null || {
    echo "error: pilot target runtime isolation contract is not satisfied" >&2; exit 1;
  }
  [[ -s "$DATA_ROOT/ssh/id_ed25519" && -s "$DATA_ROOT/ssh/known_hosts" ]] || {
    echo "error: pilot SSH identity or known-hosts pin is missing" >&2; exit 1;
  }
  ssh_uid="$(docker run --rm --network "$NETWORK" --read-only --cap-drop ALL \
    -v "$DATA_ROOT/ssh/id_ed25519:/run/praetor/id_ed25519:ro" \
    -v "$DATA_ROOT/ssh/known_hosts:/run/praetor/known_hosts:ro" \
    --entrypoint /usr/bin/ssh "$IMAGE" -i /run/praetor/id_ed25519 \
    -o BatchMode=yes -o StrictHostKeyChecking=yes \
    -o UserKnownHostsFile=/run/praetor/known_hosts \
    "praetor@$TARGET" /usr/bin/id -u)"
  [[ "$ssh_uid" == 1000 ]] || { echo "error: pilot key did not authenticate as non-root UID 1000" >&2; exit 1; }
  docker run --rm --network "$NETWORK" --read-only --cap-drop ALL \
    -v "$DATA_ROOT/ssh/id_ed25519:/run/praetor/id_ed25519:ro" \
    -v "$DATA_ROOT/ssh/known_hosts:/run/praetor/known_hosts:ro" \
    --entrypoint /usr/bin/ssh "$IMAGE" -i /run/praetor/id_ed25519 \
    -o BatchMode=yes -o StrictHostKeyChecking=yes \
    -o UserKnownHostsFile=/run/praetor/known_hosts \
    "praetor@$TARGET" 'sudo -n mkdir -p /var/lib/praetor/.bootstrap-check && sudo -n rmdir /var/lib/praetor/.bootstrap-check' || {
      echo "error: pilot target does not permit the executor's non-interactive bootstrap" >&2; exit 1;
    }
  if docker run --rm --network "$NETWORK" --read-only --cap-drop ALL \
      -v "$DATA_ROOT/ssh/id_ed25519:/run/praetor/id_ed25519:ro" \
      -v "$DATA_ROOT/ssh/known_hosts:/run/praetor/known_hosts:ro" \
      --entrypoint /usr/bin/ssh "$IMAGE" -i /run/praetor/id_ed25519 \
      -o BatchMode=yes -o StrictHostKeyChecking=yes \
      -o UserKnownHostsFile=/run/praetor/known_hosts \
      "root@$TARGET" true >/dev/null 2>&1; then
    echo "error: pilot target accepted a prohibited root SSH login" >&2
    exit 1
  fi
  nodes="$(staging_nodes)"
  [[ -n "$nodes" ]] || { echo "error: persistent staging k3d nodes are not running" >&2; exit 1; }
  while IFS= read -r node; do
    docker network inspect "$NETWORK" --format '{{json .Containers}}' | jq -e --arg name "$node" 'any(.[]; .Name == $name)' >/dev/null || {
      echo "error: staging node $node is not attached to $NETWORK" >&2; exit 1;
    }
    docker exec "$node" sh -c "timeout 3 telnet '$ADDRESS' 22 </dev/null" >/dev/null || {
      echo "error: $node cannot reach pilot SSH at $ADDRESS:22" >&2; exit 1;
    }
  done <<<"$nodes"
  load_balancer="$(staging_load_balancer)"
  [[ -n "$load_balancer" ]] || { echo "error: persistent staging load balancer is not running" >&2; exit 1; }
  docker network inspect "$NETWORK" --format '{{json .Containers}}' | jq -e --arg name "$load_balancer" 'any(.[]; .Name == $name)' >/dev/null || {
    echo "error: staging load balancer is not attached to $NETWORK" >&2; exit 1;
  }
  [[ "$(docker exec "$TARGET" getent ahostsv4 ingest.praetor-staging.localhost | awk 'NR==1 {print $1}')" == "$(docker inspect "$load_balancer" --format "{{(index .NetworkSettings.Networks \"$NETWORK\").IPAddress}}")" ]] || {
    echo "error: pilot target cannot resolve the private staging ingestion alias" >&2; exit 1;
  }
  [[ "$(docker exec "$TARGET" curl -sS -o /dev/null -w '%{http_code}' "http://ingest.praetor-staging.localhost/api/v1/runs/00000000-0000-0000-0000-000000000000/logs/cursor")" == 401 ]] || {
    echo "error: pilot target cannot reach the authenticated staging ingestion route" >&2; exit 1;
  }
  echo "healthy: isolated pilot target is reachable from persistent staging at $ADDRESS:22"
}

if [[ "$COMMAND" == plan ]]; then
  check_subnet_available
  cat <<EOF
Pilot managed-host plan
  image:       $IMAGE (digest-pinned Rocky Linux 9 base)
  target:      $TARGET at $ADDRESS:22
  network:     $NETWORK ($SUBNET), no published ports
  identity:    non-root key-only SSH; keys below $DATA_ROOT
  staging:     attach running $STAGING_PREFIX server/agent nodes only
  reset:       disposable target/network/image/data only
EOF
  exit 0
fi

if [[ "$COMMAND" == reset ]]; then
  docker rm -f "$TARGET" >/dev/null 2>&1 || true
  while IFS= read -r node; do [[ -z "$node" ]] || docker network disconnect "$NETWORK" "$node" >/dev/null 2>&1 || true; done < <(staging_nodes)
  load_balancer="$(staging_load_balancer)"; [[ -z "$load_balancer" ]] || docker network disconnect "$NETWORK" "$load_balancer" >/dev/null 2>&1 || true
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
  docker image rm "$IMAGE" >/dev/null 2>&1 || true
  rm -rf "$DATA_ROOT"
  echo "removed disposable pilot target state only"
  exit 0
fi

if [[ "$COMMAND" == status ]]; then status; exit 0; fi

check_subnet_available
mkdir -p "$DATA_ROOT/ssh" "$DATA_ROOT/hostkeys"
chmod 700 "$DATA_ROOT" "$DATA_ROOT/ssh" "$DATA_ROOT/hostkeys"
[[ -f "$DATA_ROOT/ssh/id_ed25519" ]] || ssh-keygen -q -t ed25519 -N '' -C praetor-pilot -f "$DATA_ROOT/ssh/id_ed25519"
[[ -f "$DATA_ROOT/hostkeys/ssh_host_ed25519_key" ]] || ssh-keygen -q -t ed25519 -N '' -C praetor-pilot-host -f "$DATA_ROOT/hostkeys/ssh_host_ed25519_key"
chmod 600 "$DATA_ROOT/ssh/id_ed25519" "$DATA_ROOT/hostkeys/ssh_host_ed25519_key"
chmod 644 "$DATA_ROOT/ssh/id_ed25519.pub" "$DATA_ROOT/hostkeys/ssh_host_ed25519_key.pub"
printf '%s %s\n' "$TARGET" "$(cat "$DATA_ROOT/hostkeys/ssh_host_ed25519_key.pub")" >"$DATA_ROOT/ssh/known_hosts"
chmod 600 "$DATA_ROOT/ssh/known_hosts"

if [[ "$SKIP_BUILD" == 1 ]]; then
  docker image inspect "$IMAGE" >/dev/null 2>&1 || { echo "error: $IMAGE is not available for a build-skipping reprovision" >&2; exit 1; }
else
  echo "==> Building digest-pinned pilot target image"
  docker build -t "$IMAGE" "$ROOT/deployments/pilot-host"
fi
docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create --driver bridge --subnet "$SUBNET" "$NETWORK" >/dev/null
while IFS= read -r node; do
  [[ -z "$node" ]] || docker network connect "$NETWORK" "$node" >/dev/null 2>&1 || true
done < <(staging_nodes)
load_balancer="$(staging_load_balancer)"
if [[ -n "$load_balancer" ]]; then
  docker network disconnect "$NETWORK" "$load_balancer" >/dev/null 2>&1 || true
  docker network connect --alias ingest.praetor-staging.localhost "$NETWORK" "$load_balancer"
fi

if docker inspect "$TARGET" >/dev/null 2>&1; then
  desired_image="$(docker image inspect "$IMAGE" --format '{{.Id}}')"
  current_image="$(docker inspect "$TARGET" --format '{{.Image}}')"
  current_address="$(docker inspect "$TARGET" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')"
  running="$(docker inspect "$TARGET" --format '{{.State.Running}}')"
  runtime_valid="$(docker inspect "$TARGET" | jq -r '
    .[0].HostConfig.Privileged == false and
    .[0].HostConfig.ReadonlyRootfs == true and
    (.[0].HostConfig.PortBindings | length) == 0 and
    ((.[0].HostConfig.Tmpfs | keys) as $tmpfs | ["/run", "/tmp", "/home/praetor", "/var/lib/praetor", "/opt/praetor", "/usr/local/bin", "/usr/local/share/praetor"] | all(. as $path | $tmpfs | index($path) != null)) and
    ((.[0].HostConfig.Tmpfs["/opt/praetor"] | split(",")) | index("exec") != null) and
    ((.[0].HostConfig.Tmpfs["/usr/local/bin"] | split(",")) | index("exec") != null) and
    (.[0].HostConfig.CapDrop | index("ALL")) != null and
    (.[0].HostConfig.CapAdd | index("CAP_AUDIT_WRITE")) != null and
    (.[0].HostConfig.CapAdd | index("CAP_DAC_OVERRIDE")) != null and
    (.[0].HostConfig.CapAdd | index("CAP_DAC_READ_SEARCH")) != null and
    (.[0].HostConfig.CapAdd | index("CAP_KILL")) != null and
    (.[0].HostConfig.CapAdd | index("CAP_SYS_CHROOT")) != null
  ')"
  if [[ "$current_image" != "$desired_image" || "$current_address" != "$ADDRESS" || "$running" != true || "$runtime_valid" != true ]]; then docker rm -f "$TARGET" >/dev/null; fi
fi
if ! docker inspect "$TARGET" >/dev/null 2>&1; then
  docker run -d --name "$TARGET" --hostname "$TARGET" --network "$NETWORK" --ip "$ADDRESS" \
    --read-only --tmpfs /run:rw,noexec,nosuid,size=16m --tmpfs /tmp:rw,nosuid,size=64m \
    --tmpfs /home/praetor:rw,nosuid,size=32m --tmpfs /var/lib/praetor:rw,nosuid,size=128m \
    --tmpfs /opt/praetor:rw,exec,nosuid,size=512m --tmpfs /usr/local/bin:rw,exec,nosuid,size=32m \
    --tmpfs /usr/local/share/praetor:rw,nosuid,size=16m \
    --cap-drop ALL --cap-add AUDIT_WRITE --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add DAC_READ_SEARCH --cap-add KILL --cap-add SETUID --cap-add SETGID --cap-add NET_BIND_SERVICE --cap-add SYS_CHROOT \
    -v "$DATA_ROOT/ssh/id_ed25519.pub:/run/praetor/authorized_keys:ro" \
    -v "$DATA_ROOT/hostkeys:/run/praetor/hostkeys:ro" "$IMAGE" >/dev/null
fi
for _ in $(seq 1 30); do status >/dev/null 2>&1 && break; sleep 1; done
status
