#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLUSTER="${PRAETOR_STAGING_CLUSTER:-praetor-staging}"
CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-$CLUSTER}"
NAMESPACE="praetor-staging"
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-${HOME}/.local/share/praetor/staging}"
AGENTS="${PRAETOR_STAGING_AGENTS:-1}"
HTTP_PORT="${PRAETOR_STAGING_HTTP_PORT:-8080}"
HTTPS_PORT="${PRAETOR_STAGING_HTTPS_PORT:-8443}"
TIMEOUT="${PRAETOR_STAGING_TIMEOUT:-180s}"
POLICY="$ROOT/deployments/staging/namespace.yaml"

usage() {
  cat <<EOF
usage: $0 <plan|provision|status>

  plan       print the exact topology and commands without changing anything
  provision  create or reconcile the persistent staging prerequisites
  status     fail unless every staging prerequisite is healthy

Environment overrides:
  PRAETOR_STAGING_CLUSTER     cluster name (default: praetor-staging)
  PRAETOR_STAGING_DATA_ROOT   host-backed PVC root
  PRAETOR_STAGING_AGENTS      k3d agent count (default: 1)
  PRAETOR_STAGING_HTTP_PORT   host HTTP port (default: 8080)
  PRAETOR_STAGING_HTTPS_PORT  host HTTPS port (default: 8443)
EOF
}

die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
positive_integer() { [[ "$1" =~ ^[1-9][0-9]*$ ]] || die "$2 must be a positive integer"; }

validate_inputs() {
  positive_integer "$AGENTS" PRAETOR_STAGING_AGENTS
  positive_integer "$HTTP_PORT" PRAETOR_STAGING_HTTP_PORT
  positive_integer "$HTTPS_PORT" PRAETOR_STAGING_HTTPS_PORT
  [[ "$HTTP_PORT" != "$HTTPS_PORT" ]] || die "staging HTTP and HTTPS ports must differ"
  [[ "$CLUSTER" != praetor-test && "$CLUSTER" != praetor-validation ]] ||
    die "staging cannot reuse a development or validation cluster name"
  [[ "$DATA_ROOT" = /* ]] || die "PRAETOR_STAGING_DATA_ROOT must be an absolute path"
}

cluster_exists() {
  k3d cluster list --no-headers 2>/dev/null | awk '{print $1}' | grep -Fxq "$CLUSTER"
}

print_plan() {
  cat <<EOF
Persistent Praetor staging plan
  cluster:        $CLUSTER
  context:        $CONTEXT
  namespace:      $NAMESPACE
  agents:         $AGENTS
  ingress:        http://127.0.0.1:$HTTP_PORT, https://127.0.0.1:$HTTPS_PORT
  PVC data root:  $DATA_ROOT/storage
  namespace policy: $POLICY

Create command (only when the cluster is absent):
  k3d cluster create $CLUSTER --servers 1 --agents $AGENTS --port $HTTP_PORT:80@loadbalancer --port $HTTPS_PORT:443@loadbalancer --volume $DATA_ROOT/storage:/var/lib/rancher/k3s/storage@server:0 --volume $DATA_ROOT/storage:/var/lib/rancher/k3s/storage@agent:* --wait

Reconcile command:
  kubectl --context $CONTEXT apply -f $POLICY
EOF
}

container_for_role() {
  docker ps -a --filter "label=k3d.cluster=$CLUSTER" --filter "label=k3d.role=$1" \
    --format '{{.Names}}' | sort | head -n1
}

assert_container_running() {
  local role="$1" name state
  name="$(container_for_role "$role")"
  [[ -n "$name" ]] || die "k3d $role container is missing for '$CLUSTER'"
  state="$(docker inspect --format '{{.State.Status}}' "$name")"
  [[ "$state" == running ]] || die "$name is $state, expected running"
}

assert_storage_mounts() {
  local name role mounted checked=0
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    role="$(docker inspect --format '{{index .Config.Labels "k3d.role"}}' "$name")"
    [[ "$role" == server || "$role" == agent ]] || continue
    checked=$((checked + 1))
    mounted="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/rancher/k3s/storage"}}{{.Source}}{{end}}{{end}}' "$name")"
    [[ -n "$mounted" ]] || die "$name has no host-backed k3s storage mount; recreate the empty staging cluster"
  done < <(docker ps -a --filter "label=k3d.cluster=$CLUSTER" --format '{{.Names}}' | sort)
  [[ "$checked" -eq $((AGENTS + 1)) ]] ||
    die "staging expected $((AGENTS + 1)) storage nodes, found $checked"
}

storage_probe() {
  local probe="praetor-staging-storage-probe"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pod "$probe" --ignore-not-found --wait=true >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pvc "$probe" --ignore-not-found --wait=true >/dev/null
  if ! kubectl --context "$CONTEXT" -n "$NAMESPACE" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: $probe
  labels:
    app.kubernetes.io/part-of: praetor-staging-prerequisite-check
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: local-path
  resources:
    requests:
      storage: 1Mi
---
apiVersion: v1
kind: Pod
metadata:
  name: $probe
  labels:
    app.kubernetes.io/part-of: praetor-staging-prerequisite-check
spec:
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    fsGroup: 65532
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: storage-probe
      image: busybox:1.36.1
      command: [sh, -c, "echo staging-storage-ready > /probe/health && sleep 300"]
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop: [ALL]
      resources:
        requests:
          cpu: 5m
          memory: 4Mi
        limits:
          cpu: 10m
          memory: 16Mi
      volumeMounts:
        - name: probe
          mountPath: /probe
  volumes:
    - name: probe
      persistentVolumeClaim:
        claimName: $probe
EOF
  then
    kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pod "$probe" --ignore-not-found --wait=true >/dev/null || true
    kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pvc "$probe" --ignore-not-found --wait=true >/dev/null || true
    return 1
  fi
  if ! kubectl --context "$CONTEXT" -n "$NAMESPACE" wait --for=condition=Ready \
    "pod/$probe" --timeout="$TIMEOUT" >/dev/null; then
    kubectl --context "$CONTEXT" -n "$NAMESPACE" describe "pod/$probe" >&2 || true
    kubectl --context "$CONTEXT" -n "$NAMESPACE" describe "pvc/$probe" >&2 || true
    kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pod "$probe" --ignore-not-found --wait=true >/dev/null || true
    kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pvc "$probe" --ignore-not-found --wait=true >/dev/null || true
    return 1
  fi
  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$probe" -- test -s /probe/health
  kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pod "$probe" --wait=true >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pvc "$probe" --wait=true >/dev/null
}

status() {
  for command in docker k3d kubectl; do need "$command"; done
  cluster_exists || die "staging cluster '$CLUSTER' does not exist; run '$0 provision'"
  assert_container_running server
  assert_container_running loadbalancer
  assert_storage_mounts
  kubectl --context "$CONTEXT" get --raw=/readyz >/dev/null
  kubectl --context "$CONTEXT" -n kube-system wait --for=create deployment/traefik --timeout="$TIMEOUT" >/dev/null
  kubectl --context "$CONTEXT" -n kube-system rollout status deployment/traefik --timeout="$TIMEOUT" >/dev/null
  kubectl --context "$CONTEXT" wait --for=create storageclass/local-path --timeout="$TIMEOUT" >/dev/null
  kubectl --context "$CONTEXT" get storageclass local-path >/dev/null
  kubectl --context "$CONTEXT" get namespace "$NAMESPACE" >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" get resourcequota praetor-staging >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" get limitrange praetor-staging >/dev/null
  storage_probe
  echo "healthy: persistent staging prerequisites are ready in '$CONTEXT'"
}

provision() {
  for command in docker k3d kubectl; do need "$command"; done
  install -d -m 0700 "$DATA_ROOT" "$DATA_ROOT/storage"
  if cluster_exists; then
    echo "==> Staging cluster '$CLUSTER' already exists; preserving it"
  else
    echo "==> Creating isolated persistent staging cluster '$CLUSTER'"
    k3d cluster create "$CLUSTER" --servers 1 --agents "$AGENTS" \
      --port "$HTTP_PORT:80@loadbalancer" \
      --port "$HTTPS_PORT:443@loadbalancer" \
      --volume "$DATA_ROOT/storage:/var/lib/rancher/k3s/storage@server:0" \
      --volume "$DATA_ROOT/storage:/var/lib/rancher/k3s/storage@agent:*" \
      --wait
  fi
  kubectl --context "$CONTEXT" apply -f "$POLICY" >/dev/null
  status
}

validate_inputs
case "${1:-}" in
  plan) print_plan ;;
  provision) provision ;;
  status) status ;;
  *) usage >&2; exit 2 ;;
esac
