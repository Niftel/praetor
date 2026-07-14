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

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command '$1' is not installed" >&2
    exit 1
  }
}

for command in docker k3d kubectl helm; do
  need "$command"
done

if ! k3d cluster list --no-headers 2>/dev/null | awk '{print $1}' | grep -Fxq "$CLUSTER"; then
  echo "error: k3d cluster '$CLUSTER' is not running" >&2
  exit 1
fi

echo "==> Building local images"
docker build -f "$ROOT/build/package/Dockerfile.api" \
  -t "praetor-api:$TAG" "$ROOT"
docker build -f "$ROOT/build/package/Dockerfile.migrator" \
  -t "praetor-migrator:$TAG" "$ROOT"
docker build -f "$ROOT/web/Dockerfile" \
  -t "praetor-ui:$TAG" "$ROOT/web"

echo "==> Importing images into k3d cluster '$CLUSTER'"
k3d image import -c "$CLUSTER" \
  "praetor-api:$TAG" \
  "praetor-migrator:$TAG" \
  "praetor-ui:$TAG"

echo "==> Upgrading Helm release '$RELEASE' in namespace '$NAMESPACE'"
helm upgrade --install "$RELEASE" "$CHART" \
  -f "$VALUES" \
  -n "$NAMESPACE" \
  --create-namespace \
  --wait \
  --timeout 10m

# The local deployment deliberately reuses the mutable :dev tag. Restart the
# long-running workloads after import so containerd resolves the new image.
echo "==> Restarting API and UI"
kubectl rollout restart \
  "deployment/$RELEASE-api" \
  "deployment/$RELEASE-ui" \
  -n "$NAMESPACE"

kubectl rollout status "deployment/$RELEASE-api" -n "$NAMESPACE" --timeout=5m
kubectl rollout status "deployment/$RELEASE-ui" -n "$NAMESPACE" --timeout=5m

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
