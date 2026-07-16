#!/usr/bin/env bash
set -euo pipefail

# Deploy the exact released component set from platform-compatibility.yaml to
# the local k3d cluster. Existing local URLs, LDAP configuration, secrets, and
# PostgreSQL data are preserved by layering generated image values over the
# checked-in local values file.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RELEASE="${PRAETOR_HELM_RELEASE:-praetor}"
NAMESPACE="${PRAETOR_NAMESPACE:-praetor}"
CHART="$ROOT/deployments/helm/praetor-v2"
LOCAL_VALUES="$CHART/ci/values-k3d-local.yaml"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command '$1' is not installed" >&2
    exit 1
  }
}

for command in go helm kubectl; do
  need "$command"
done

RELEASE_VALUES="$(mktemp "${TMPDIR:-/tmp}/praetor-release-values.XXXXXX.yaml")"
RENDERED="$(mktemp "${TMPDIR:-/tmp}/praetor-release-rendered.XXXXXX.yaml")"
trap 'rm -f "$RELEASE_VALUES" "$RENDERED"' EXIT

(
  cd "$ROOT"
  go run ./cmd/compatcheck -output helm-values
) >"$RELEASE_VALUES"

echo "==> Validating exact platform images"
helm template "$RELEASE" "$CHART" \
  -f "$LOCAL_VALUES" \
  -f "$RELEASE_VALUES" \
  -n "$NAMESPACE" >"$RENDERED"

if grep -Eq 'image: .+:(latest|dev)([[:space:]]|$)' "$RENDERED"; then
  echo "error: rendered release contains a mutable latest/dev image tag" >&2
  grep -En 'image: .+:(latest|dev)([[:space:]]|$)' "$RENDERED" >&2
  exit 1
fi

"$ROOT/scripts/local-cluster.sh" start

echo "==> Deploying platform compatibility release"
(
  cd "$ROOT"
  go run ./cmd/compatcheck
)
helm upgrade --install "$RELEASE" "$CHART" \
  -f "$LOCAL_VALUES" \
  -f "$RELEASE_VALUES" \
  -n "$NAMESPACE" \
  --create-namespace \
  --wait \
  --timeout 10m

echo "==> Deployed first-party images"
kubectl get deployments,statefulsets -n "$NAMESPACE" \
  -l app.kubernetes.io/instance="$RELEASE" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.template.spec.containers[*]}{.image}{" "}{end}{"\n"}{end}'

echo "==> Local Praetor release is ready"
kubectl get pods -n "$NAMESPACE" \
  -l app.kubernetes.io/instance="$RELEASE"
echo "Open https://praetor.localhost and hard-refresh the page."
