#!/usr/bin/env bash
set -euo pipefail

# Manage the local k3d cluster as one unit. Docker restart policies do not know
# that serverlb depends on server-0, so a Docker Desktop restart can otherwise
# leave nginx crash-looping against a stopped k3s server.

CLUSTER="${PRAETOR_K3D_CLUSTER:-praetor-test}"
CONTEXT="${PRAETOR_KUBE_CONTEXT:-k3d-$CLUSTER}"
START_TIMEOUT="${PRAETOR_CLUSTER_START_TIMEOUT:-180}"
STOP_TIMEOUT="${PRAETOR_CLUSTER_STOP_TIMEOUT:-60}"
POLL_INTERVAL="${PRAETOR_CLUSTER_POLL_INTERVAL:-2}"
K3D_COMMAND_TIMEOUT="${PRAETOR_K3D_COMMAND_TIMEOUT:-45}"

usage() {
  cat <<EOF
usage: $0 <create|status|start|stop|recover>

  create   create the supported ingress-enabled local cluster
  status   show k3d component state and fail if the cluster is split/unhealthy
  start    start a healthy stopped cluster, or recover a partial cluster
  stop     stop the entire cluster with a graceful k3s shutdown allowance
  recover  break load-balancer restart loops, stop the cluster, then start it

Environment:
  PRAETOR_K3D_CLUSTER             cluster name (default: praetor-test)
  PRAETOR_CLUSTER_START_TIMEOUT  readiness timeout in seconds (default: 180)
  PRAETOR_CLUSTER_STOP_TIMEOUT   per-container stop timeout (default: 60)
  PRAETOR_K3D_COMMAND_TIMEOUT    k3d command timeout in seconds (default: 45)
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"
}

for command in docker k3d kubectl; do
  need "$command"
done

[[ "$START_TIMEOUT" =~ ^[1-9][0-9]*$ ]] || die "PRAETOR_CLUSTER_START_TIMEOUT must be a positive integer"
[[ "$STOP_TIMEOUT" =~ ^[1-9][0-9]*$ ]] || die "PRAETOR_CLUSTER_STOP_TIMEOUT must be a positive integer"
[[ "$POLL_INTERVAL" =~ ^[1-9][0-9]*$ ]] || die "PRAETOR_CLUSTER_POLL_INTERVAL must be a positive integer"
[[ "$K3D_COMMAND_TIMEOUT" =~ ^[1-9][0-9]*$ ]] || die "PRAETOR_K3D_COMMAND_TIMEOUT must be a positive integer"

run_bounded() {
  local timeout="$1"
  shift
  local pid watcher status=0
  "$@" &
  pid=$!
  (
    sleep "$timeout"
    if kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null || true
      sleep 2
      kill -KILL "$pid" 2>/dev/null || true
    fi
  ) &
  watcher=$!
  wait "$pid" || status=$?
  kill "$watcher" 2>/dev/null || true
  wait "$watcher" 2>/dev/null || true
  if [[ "$status" == 143 || "$status" == 137 ]]; then
    return 124
  fi
  return "$status"
}

cluster_exists() {
  k3d cluster list --no-headers 2>/dev/null | awk '{print $1}' | grep -Fxq "$CLUSTER"
}

components() {
  docker ps -a \
    --filter "label=k3d.cluster=$CLUSTER" \
    --format '{{.Names}}' | sort
}

component_for_role() {
  local role="$1"
  docker ps -a \
    --filter "label=k3d.cluster=$CLUSTER" \
    --filter "label=k3d.role=$role" \
    --format '{{.Names}}' | sort | head -n 1
}

state_of() {
  docker inspect --format '{{.State.Status}}' "$1" 2>/dev/null || echo missing
}

restart_count_of() {
  docker inspect --format '{{.RestartCount}}' "$1" 2>/dev/null || echo 0
}

discover() {
  SERVER="$(component_for_role server)"
  LOAD_BALANCER="$(component_for_role loadbalancer)"
  [[ -n "$SERVER" ]] || die "k3d server container for '$CLUSTER' was not found"
  [[ -n "$LOAD_BALANCER" ]] ||
    die "k3d load-balancer container for '$CLUSTER' was not found; recreate it with '$0 create'"
}

traefik_disabled() {
  docker inspect --format '{{json .Config.Cmd}}' "$SERVER" 2>/dev/null |
    grep -Eq '"--disable(=|","?)traefik"'
}

load_balancer_exposes() {
  local container_port="$1"
  docker inspect --format "{{with index .HostConfig.PortBindings \"$container_port/tcp\"}}{{range .}}{{.HostPort}}{{end}}{{end}}" \
    "$LOAD_BALANCER" 2>/dev/null | grep -Fxq "$container_port"
}

validate_ingress_topology() {
  local invalid=0
  if traefik_disabled; then
    echo "error: cluster '$CLUSTER' was created with Traefik disabled" >&2
    invalid=1
  fi
  if ! load_balancer_exposes 80; then
    echo "error: cluster '$CLUSTER' does not expose host port 80 through its load balancer" >&2
    invalid=1
  fi
  if ! load_balancer_exposes 443; then
    echo "error: cluster '$CLUSTER' does not expose host port 443 through its load balancer" >&2
    invalid=1
  fi
  if (( invalid )); then
    echo "error: recreate it with '$0 create'; the supported Praetor dev cluster always includes Traefik and ports 80/443" >&2
    return 1
  fi
}

split_cluster() {
  local server_state load_balancer_state
  server_state="$(state_of "$SERVER")"
  load_balancer_state="$(state_of "$LOAD_BALANCER")"
  [[ "$server_state" != running && ( "$load_balancer_state" == running || "$load_balancer_state" == restarting ) ]]
}

print_status() {
  local name role state restarts exit_code
  printf '%-36s %-14s %-11s %-8s %s\n' NAME ROLE STATE RESTARTS EXIT
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    role="$(docker inspect --format '{{index .Config.Labels "k3d.role"}}' "$name")"
    state="$(state_of "$name")"
    restarts="$(restart_count_of "$name")"
    exit_code="$(docker inspect --format '{{.State.ExitCode}}' "$name" 2>/dev/null || echo '?')"
    printf '%-36s %-14s %-11s %-8s %s\n' "$name" "$role" "$state" "$restarts" "$exit_code"
  done < <(components)
}

graceful_stop_server() {
  if [[ "$(state_of "$SERVER")" == running ]]; then
    echo "==> Allowing k3s up to ${STOP_TIMEOUT}s to stop cleanly"
    if ! run_bounded "$((STOP_TIMEOUT + 5))" docker stop --time "$STOP_TIMEOUT" "$SERVER" >/dev/null; then
      echo "warning: graceful k3s stop timed out; forcing the server container to stop" >&2
      run_bounded 10 docker kill "$SERVER" >/dev/null || die "could not stop '$SERVER'"
    fi
  fi
}

quiesce_load_balancer() {
  local state
  state="$(state_of "$LOAD_BALANCER")"
  if [[ "$state" == restarting || "$state" == running ]]; then
    echo "==> Quiescing load balancer before the k3s server"
    run_bounded 10 docker update --restart=no "$LOAD_BALANCER" >/dev/null || die "could not disable '$LOAD_BALANCER' restart policy"
    if ! run_bounded 15 docker stop --time 10 "$LOAD_BALANCER" >/dev/null 2>&1; then
      run_bounded 10 docker kill "$LOAD_BALANCER" >/dev/null 2>&1 || die "could not stop '$LOAD_BALANCER'"
    fi
  fi
}

restore_restart_policy() {
  local name
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    run_bounded 10 docker update --restart=unless-stopped "$name" >/dev/null || die "could not restore restart policy on '$name'"
  done < <(components)
}

remove_orphaned_tools_nodes() {
  local name
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    echo "==> Removing orphaned k3d tools node '$name'"
    if ! run_bounded 10 docker rm -f "$name" >/dev/null; then
      echo "warning: could not remove orphaned tools node '$name'; continuing with ordered recovery" >&2
    fi
  done < <(docker ps -a \
    --filter "name=^/k3d-$CLUSTER-tools$" \
    --format '{{.Names}}')
}

ordered_start() {
  echo "==> Ordered start: k3s server, then load balancer"
  if [[ "$(state_of "$SERVER")" != running ]]; then
    run_bounded 20 docker start "$SERVER" >/dev/null || die "could not start '$SERVER'; Docker Desktop may be unresponsive"
  fi
  local deadline=$((SECONDS + START_TIMEOUT))
  while [[ "$(state_of "$SERVER")" != running ]]; do
    (( SECONDS < deadline )) || die "'$SERVER' did not enter running state"
    sleep "$POLL_INTERVAL"
  done
  if [[ "$(state_of "$LOAD_BALANCER")" != running ]]; then
    run_bounded 20 docker start "$LOAD_BALANCER" >/dev/null || die "could not start '$LOAD_BALANCER'; Docker Desktop may be unresponsive"
  fi
}

k3d_stop() {
  if run_bounded "$K3D_COMMAND_TIMEOUT" k3d cluster stop "$CLUSTER"; then
    return 0
  fi
  if [[ "$(state_of "$SERVER")" != running && "$(state_of "$LOAD_BALANCER")" != running ]]; then
    echo "warning: k3d stop timed out after ${K3D_COMMAND_TIMEOUT}s, but cluster containers are stopped" >&2
    return 0
  fi
  die "k3d cluster stop failed or timed out with components still running"
}

k3d_start() {
  if run_bounded "$K3D_COMMAND_TIMEOUT" k3d cluster start "$CLUSTER"; then
    return 0
  fi
  echo "warning: k3d start did not return within ${K3D_COMMAND_TIMEOUT}s; verifying independent readiness" >&2
  remove_orphaned_tools_nodes
  ordered_start
}

wait_for_cluster() {
  local deadline now server_state load_balancer_state
  deadline=$((SECONDS + START_TIMEOUT))
  while (( SECONDS < deadline )); do
    server_state="$(state_of "$SERVER")"
    load_balancer_state="$(state_of "$LOAD_BALANCER")"
    if [[ "$server_state" == running && "$load_balancer_state" == running ]]; then
      if docker exec "$LOAD_BALANCER" getent hosts "$SERVER" >/dev/null 2>&1 &&
         kubectl --context "$CONTEXT" get --raw=/readyz >/dev/null 2>&1; then
        echo "==> Cluster '$CLUSTER' is ready; load-balancer DNS resolves '$SERVER'"
        return 0
      fi
    fi
    if [[ "$load_balancer_state" == restarting ]]; then
      echo "waiting: load balancer is restarting while server is $server_state" >&2
    fi
    sleep "$POLL_INTERVAL"
  done

  echo "error: cluster '$CLUSTER' did not become ready within ${START_TIMEOUT}s" >&2
  print_status >&2
  docker logs --tail 20 "$LOAD_BALANCER" >&2 2>/dev/null || true
  return 1
}

stop_cluster() {
  discover
  quiesce_load_balancer
  graceful_stop_server
  echo "==> Stopping k3d cluster '$CLUSTER'"
  k3d_stop
  restore_restart_policy
}

start_cluster() {
  discover
  validate_ingress_topology
  if split_cluster; then
    echo "==> Partial cluster detected; running ordered recovery"
    recover_cluster
    return
  fi
  restore_restart_policy
  echo "==> Starting k3d cluster '$CLUSTER'"
  k3d_start
  wait_for_cluster
  remove_orphaned_tools_nodes
}

create_cluster() {
  if cluster_exists; then
    die "k3d cluster '$CLUSTER' already exists; creation never replaces existing data"
  fi
  echo "==> Creating ingress-enabled k3d cluster '$CLUSTER'"
  run_bounded "$K3D_COMMAND_TIMEOUT" k3d cluster create "$CLUSTER" \
    --port "80:80@loadbalancer" \
    --port "443:443@loadbalancer" ||
    die "could not create k3d cluster '$CLUSTER'"
  discover
  validate_ingress_topology
  wait_for_cluster
}

recover_cluster() {
  discover
  quiesce_load_balancer
  graceful_stop_server
  echo "==> Resetting cluster lifecycle through k3d"
  k3d_stop
  restore_restart_policy
  # k3d start creates a temporary tools node before starting the server. On the
  # affected Docker Desktop builds that helper can hang and wedge the daemon.
  # Recovery already has complete cluster metadata, so bypass only that broken
  # start phase and enforce the dependency order directly.
  remove_orphaned_tools_nodes
  ordered_start
  wait_for_cluster
  remove_orphaned_tools_nodes
}

case "${1:-}" in
  create)
    create_cluster
    ;;
  status)
    cluster_exists || die "k3d cluster '$CLUSTER' does not exist; run '$0 create'"
    discover
    validate_ingress_topology
    print_status
    if split_cluster; then
      die "partial cluster: '$SERVER' is stopped while '$LOAD_BALANCER' is active"
    fi
    if [[ "$(state_of "$SERVER")" == running && "$(state_of "$LOAD_BALANCER")" == restarting ]]; then
      die "load balancer is restart-looping"
    fi
    ;;
  start)
    cluster_exists || die "k3d cluster '$CLUSTER' does not exist; run '$0 create'"
    start_cluster
    ;;
  stop)
    cluster_exists || die "k3d cluster '$CLUSTER' does not exist; run '$0 create'"
    stop_cluster
    ;;
  recover)
    cluster_exists || die "k3d cluster '$CLUSTER' does not exist; run '$0 create'"
    discover
    validate_ingress_topology
    recover_cluster
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
