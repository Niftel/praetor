#!/usr/bin/env bash
set -euo pipefail

# Rebuild and deploy the Praetor components owned by this repository to the
# local k3d cluster. Existing Helm values and PostgreSQL data are preserved.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLUSTER="${PRAETOR_K3D_CLUSTER:-praetor-test}"
RELEASE="${PRAETOR_HELM_RELEASE:-praetor}"
NAMESPACE="${PRAETOR_NAMESPACE:-praetor}"
TAG="${PRAETOR_IMAGE_TAG:-dev}"
CHART="$ROOT/deployments/helm/praetor-v2"
VALUES="$CHART/ci/values-k3d-local.yaml"
SCHEDULER_ROOT="${PRAETOR_SCHEDULER_ROOT:-$ROOT/../scheduler}"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command '$1' is not installed" >&2
    exit 1
  }
}

for command in docker k3d kubectl helm; do
  need "$command"
done

# Recover a partial Docker/k3d restart before building. In particular, never
# leave serverlb crash-looping while server-0 is stopped.
"$ROOT/scripts/local-cluster.sh" start

echo "==> Building local images"
docker build -f "$ROOT/build/package/Dockerfile.api" \
  -t "praetor-api:$TAG" "$ROOT"
docker build -f "$ROOT/build/package/Dockerfile.migrator" \
  -t "praetor-migrator:$TAG" "$ROOT"
docker build -f "$ROOT/web/Dockerfile" \
  -t "praetor-ui:$TAG" "$ROOT/web"
if [[ ! -f "$SCHEDULER_ROOT/Dockerfile" ]]; then
  echo "error: scheduler checkout not found at '$SCHEDULER_ROOT'" >&2
  echo "set PRAETOR_SCHEDULER_ROOT to its location" >&2
  exit 1
fi
docker build -f "$SCHEDULER_ROOT/Dockerfile" \
  -t "praetor-scheduler:$TAG" "$SCHEDULER_ROOT"

echo "==> Importing images into k3d cluster '$CLUSTER'"
k3d image import -c "$CLUSTER" \
  "praetor-api:$TAG" \
  "praetor-migrator:$TAG" \
  "praetor-ui:$TAG" \
  "praetor-scheduler:$TAG"

echo "==> Upgrading Helm release '$RELEASE' in namespace '$NAMESPACE'"
helm upgrade --install "$RELEASE" "$CHART" \
  -f "$VALUES" \
  -n "$NAMESPACE" \
  --create-namespace \
  --wait \
  --timeout 10m

# The local deployment deliberately reuses the mutable :dev tag. Restart the
# long-running workloads after import so containerd resolves the new image.
echo "==> Restarting API, UI, and scheduler"
kubectl rollout restart \
  "deployment/$RELEASE-api" \
  "deployment/$RELEASE-ui" \
  "deployment/$RELEASE-scheduler" \
  -n "$NAMESPACE"

kubectl rollout status "deployment/$RELEASE-api" -n "$NAMESPACE" --timeout=5m
kubectl rollout status "deployment/$RELEASE-ui" -n "$NAMESPACE" --timeout=5m
kubectl rollout status "deployment/$RELEASE-scheduler" -n "$NAMESPACE" --timeout=5m

MIGRATION_JOB="$(
  kubectl get jobs -n "$NAMESPACE" \
    -l app.kubernetes.io/instance="$RELEASE",app.kubernetes.io/component=migrator \
    --sort-by=.metadata.creationTimestamp \
    -o jsonpath='{.items[-1:].metadata.name}'
)"

if [[ -n "$MIGRATION_JOB" ]]; then
  echo "==> Latest migration job: $MIGRATION_JOB"
  kubectl wait --for=condition=complete "job/$MIGRATION_JOB" -n "$NAMESPACE" --timeout=5m
  kubectl logs "job/$MIGRATION_JOB" -n "$NAMESPACE" --tail=40
fi

echo "==> Local Praetor cluster updated"
kubectl get pods -n "$NAMESPACE" \
  -l app.kubernetes.io/instance="$RELEASE"
echo "Open https://praetor.localhost and hard-refresh the page."
