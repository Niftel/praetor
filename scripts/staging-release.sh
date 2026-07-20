#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMMAND="${1:-}"
CLUSTER="praetor-staging"
CONTEXT="k3d-$CLUSTER"
NAMESPACE="praetor-staging"
RELEASE="praetor-staging"
CHART="$ROOT/deployments/helm/praetor-v2"
STAGING_VALUES="$ROOT/deployments/staging/values.yaml"
LOCK="$ROOT/deployments/staging/release-lock.yaml"
SECRET_NAME="praetor-staging-runtime"
REGISTRY_SECRET_NAME="praetor-staging-registry"
INGRESS_TLS_SECRET_NAME="praetor-staging-ingress-tls"
LDAP_TLS_SECRET_NAME="praetor-staging-ldap-tls"
LDAP_CONFIG_NAME="praetor-staging-ldap-config"
IDENTITY_SECRET_NAMES=(praetor-api-identity praetor-scheduler-identity praetor-executor-identity)
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-$HOME/.local/share/praetor/staging}"
export GOCACHE="${PRAETOR_STAGING_CACHE:-${TMPDIR:-/tmp}/praetor-staging-release/go-build}"

usage() {
  echo "usage: $0 <plan|deploy|status>" >&2
  exit 2
}

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "error: required command '$1' is not installed" >&2; exit 1; }
}

for command in go helm kubectl jq docker; do need "$command"; done
[[ "$COMMAND" =~ ^(plan|deploy|status)$ ]] || usage

generated="$(mktemp "${TMPDIR:-/tmp}/praetor-staging-values.yaml.XXXXXX")"
rendered="$(mktemp "${TMPDIR:-/tmp}/praetor-staging-rendered.yaml.XXXXXX")"
cleanup() {
  local status=$?
  trap - EXIT
  rm -f "$generated" "$rendered"
  exit "$status"
}
trap cleanup EXIT

cd "$ROOT"
go run ./cmd/compatcheck >/dev/null
go run ./cmd/stagingrelease -lock "$LOCK" -output helm-values >"$generated"
go run ./cmd/stagingrelease -lock "$LOCK"

echo "==> Verifying locked registry manifests"
target_arch="${PRAETOR_STAGING_ARCH:-$(kubectl --context "$CONTEXT" get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}' 2>/dev/null || true)}"
[[ -n "$target_arch" ]] || {
  echo "error: cannot determine staging architecture; set PRAETOR_STAGING_ARCH" >&2
  exit 1
}
while IFS=@ read -r tagged digest; do
  manifest="$(docker buildx imagetools inspect "$tagged" --format '{{json .Manifest}}')"
  remote="$(jq -r '.digest // .Digest' <<<"$manifest")"
  [[ "$remote" == "$digest" ]] || {
    echo "error: $tagged resolved to $remote, lock requires $digest" >&2
    exit 1
  }
  platforms="$(jq -r '(.manifests // .Manifests)[] | (.platform.os // .Platform.OS) + "/" + (.platform.architecture // .Platform.Architecture)' <<<"$manifest")"
  grep -Fxq "linux/$target_arch" <<<"$platforms" || {
    echo "error: $tagged does not publish linux/$target_arch required by staging" >&2
    exit 1
  }
done < <(go run ./cmd/stagingrelease -lock "$LOCK" -output artifacts)

helm lint "$CHART" -f "$STAGING_VALUES" -f "$generated" >/dev/null
helm template "$RELEASE" "$CHART" -n "$NAMESPACE" \
  -f "$STAGING_VALUES" -f "$generated" >"$rendered"

if grep -Eq '^[[:space:]]*image:[[:space:]]+[^@[:space:]]+:(latest|dev|main|master)([[:space:]]|$)' "$rendered"; then
  echo "error: rendered staging release contains a floating image" >&2
  grep -En '^[[:space:]]*image:[[:space:]]+[^@[:space:]]+:(latest|dev|main|master)([[:space:]]|$)' "$rendered" >&2
  exit 1
fi
if grep -E '^[[:space:]]*image:' "$rendered" | grep -vq '@sha256:'; then
  echo "error: every rendered staging workload image must be digest-pinned" >&2
  grep -E '^[[:space:]]*image:' "$rendered" | grep -v '@sha256:' >&2
  exit 1
fi

if [[ "$COMMAND" == plan ]]; then
  echo "Staging release plan passed: lock, remote manifests, chart schema, and rendered digests agree."
  exit 0
fi

"$ROOT/scripts/staging-environment.sh" status >/dev/null
kubectl --context "$CONTEXT" get secret "$SECRET_NAME" -n "$NAMESPACE" >/dev/null 2>&1 || {
  echo "error: required Secret $NAMESPACE/$SECRET_NAME does not exist" >&2
  echo "Create it outside Git; deployment accepts secret references only." >&2
  exit 1
}
kubectl --context "$CONTEXT" get secret "$REGISTRY_SECRET_NAME" -n "$NAMESPACE" \
  -o json | jq -e '.type == "kubernetes.io/dockerconfigjson" and (.data | has(".dockerconfigjson"))' >/dev/null || {
    echo "error: required read-only registry Secret $NAMESPACE/$REGISTRY_SECRET_NAME is missing or invalid" >&2
    exit 1
  }
for key in DATABASE_URL PRAETOR_SECRET_KEY JWT_SECRET PRAETOR_INTERNAL_TOKEN PRAETOR_LDAP_BIND_PASSWORD; do
  kubectl --context "$CONTEXT" get secret "$SECRET_NAME" -n "$NAMESPACE" -o json |
    jq -e --arg key "$key" '.data | has($key) and (.[$key] | @base64d | length > 0)' >/dev/null || {
      echo "error: Secret $NAMESPACE/$SECRET_NAME is missing or has an empty key $key" >&2
      exit 1
    }
done
kubectl --context "$CONTEXT" get secret "$SECRET_NAME" -n "$NAMESPACE" -o json |
  jq -e '.data | has("PRAETOR_SECRET_KEY_OLD")' >/dev/null || {
    echo "error: Secret $NAMESPACE/$SECRET_NAME is missing key PRAETOR_SECRET_KEY_OLD" >&2
    exit 1
  }
kubectl --context "$CONTEXT" get secret "$INGRESS_TLS_SECRET_NAME" -n "$NAMESPACE" -o json |
  jq -e '.type == "kubernetes.io/tls" and (.data | has("tls.crt") and has("tls.key"))' >/dev/null || {
    echo "error: trusted ingress TLS Secret $NAMESPACE/$INGRESS_TLS_SECRET_NAME is missing or invalid" >&2
    exit 1
  }
kubectl --context "$CONTEXT" get secret "$LDAP_TLS_SECRET_NAME" -n "$NAMESPACE" -o json |
  jq -e '.data | has("tls.crt") and has("tls.key") and has("ca.crt")' >/dev/null || {
    echo "error: verified LDAPS Secret $NAMESPACE/$LDAP_TLS_SECRET_NAME is missing or invalid" >&2
    exit 1
  }
kubectl --context "$CONTEXT" get configmap "$LDAP_CONFIG_NAME" -n "$NAMESPACE" -o json |
  jq -e '.data | has("ldap.yaml") and has("ca.crt")' >/dev/null || {
    echo "error: LDAP configuration bundle $NAMESPACE/$LDAP_CONFIG_NAME is missing ldap.yaml or ca.crt" >&2
    exit 1
  }
for identity in "${IDENTITY_SECRET_NAMES[@]}"; do
  kubectl --context "$CONTEXT" get secret "$identity" -n "$NAMESPACE" -o json |
    jq -e '.data | has("ca.crt") and has("tls.crt") and has("tls.key")' >/dev/null || {
      echo "error: workload identity Secret $NAMESPACE/$identity is missing required certificate keys" >&2
      exit 1
    }
done
for prerequisite in \
  statefulset/praetor-staging-ldap \
  statefulset/praetor-staging-secrets-postgres \
  statefulset/praetor-staging-audit-postgres \
  deployment/praetor-secrets \
  deployment/praetor-audit-sink; do
  kubectl --context "$CONTEXT" rollout status "$prerequisite" -n "$NAMESPACE" --timeout=180s >/dev/null || {
    echo "error: staging integration prerequisite $prerequisite is not healthy" >&2
    exit 1
  }
done

if [[ "$COMMAND" == deploy ]]; then
  echo "==> Applying immutable platform release"
  helm upgrade --install "$RELEASE" "$CHART" \
    --kube-context "$CONTEXT" --namespace "$NAMESPACE" \
    -f "$STAGING_VALUES" -f "$generated" \
    --force-conflicts --rollback-on-failure --wait --timeout 15m
fi

echo "==> Verifying deployed revisions"
expected="$(go run ./cmd/stagingrelease -lock "$LOCK" -output images | sort)"
actual="$(kubectl --context "$CONTEXT" get deployments,statefulsets,jobs -n "$NAMESPACE" \
  -l "app.kubernetes.io/instance=$RELEASE" -o json |
  jq -r '.items[].spec.template.spec | ((.initContainers // []) + (.containers // []))[]?.image' | sort -u)"
release_manifest="$(helm get manifest "$RELEASE" --kube-context "$CONTEXT" -n "$NAMESPACE")"
while IFS= read -r image; do
  if [[ "$image" == */praetor-migrator@* ]]; then
    # The revisioned migration Job is intentionally removed ten minutes after
    # success. Its immutable image remains in Helm's stored release manifest.
    grep -Fq "image: $image" <<<"$release_manifest" || { echo "error: expected migrator image is absent from Helm release: $image" >&2; exit 1; }
  else
    grep -Fxq "$image" <<<"$actual" || { echo "error: expected deployed image is absent: $image" >&2; exit 1; }
  fi
done <<<"$expected"

platform_version="$(go run ./cmd/stagingrelease -lock "$LOCK" -output summary | awk '{print $3}' | tr -d ':')"
revision="$(helm status "$RELEASE" --kube-context "$CONTEXT" -n "$NAMESPACE" -o json | jq -r .version)"
evidence_dir="$DATA_ROOT/evidence/$platform_version"
mkdir -p "$evidence_dir"
chmod 700 "$DATA_ROOT" "$DATA_ROOT/evidence" "$evidence_dir" 2>/dev/null || true
evidence="$evidence_dir/revision-$revision.json"
jq -n --arg platformVersion "$platform_version" --arg release "$RELEASE" \
  --arg namespace "$NAMESPACE" --argjson helmRevision "$revision" \
  --arg recordedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson images "$(printf '%s\n' "$actual" | jq -Rsc 'split("\n") | map(select(length > 0))')" \
  '{schemaVersion:1, platformVersion:$platformVersion, release:$release, namespace:$namespace, helmRevision:$helmRevision, recordedAt:$recordedAt, images:$images}' \
  >"$evidence"
chmod 600 "$evidence"
echo "Staging release is healthy; sanitized evidence: $evidence"
