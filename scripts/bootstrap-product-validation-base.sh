#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SECRETS_ROOT="${PRAETOR_SECRETS_ROOT:-$ROOT/../praetor-secrets}"
NAMESPACE="${PRAETOR_VALIDATION_NAMESPACE:-praetor-secrets}"
TRUST_DOMAIN="${PRAETOR_VALIDATION_TRUST_DOMAIN:-praetor.local}"
CHART="$ROOT/deployments/helm/praetor-v2"
SECRETS_CHART="$SECRETS_ROOT/charts/praetor-secrets-stack"

for command in docker go helm kubectl; do command -v "$command" >/dev/null || { echo "error: $command is required" >&2; exit 1; }; done
[[ -f "$SECRETS_ROOT/cmd/praetor-dev-bootstrap/main.go" ]] || { echo "error: praetor-secrets checkout not found at $SECRETS_ROOT" >&2; exit 1; }
kubectl get --raw=/readyz >/dev/null

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl apply -n "$NAMESPACE" -f "$ROOT/deployments/product-validation/base-datastores.yaml" >/dev/null
kubectl rollout status deployment/praetor-validation-secrets-postgres -n "$NAMESPACE" --timeout=180s
kubectl rollout status deployment/praetor-validation-audit-postgres -n "$NAMESPACE" --timeout=180s

work="$(mktemp -d "${TMPDIR:-/tmp}/praetor-validation-bootstrap.XXXXXX")"
trap 'rm -rf "$work"' EXIT
chmod 700 "$work"
printf '%s\n' "postgres://postgres:validation-only@praetor-validation-secrets-postgres:5432/postgres?sslmode=disable" >"$work/secrets-db-url"
printf '%s\n' "postgres://postgres:validation-only@praetor-validation-audit-postgres:5432/postgres?sslmode=disable" >"$work/audit-db-url"
chmod 600 "$work/secrets-db-url" "$work/audit-db-url"
(
  cd "$SECRETS_ROOT"
  go run ./cmd/praetor-dev-bootstrap -output "$work/generated" -namespace "$NAMESPACE" -trust-domain "$TRUST_DOMAIN" -secrets-database-url-file "$work/secrets-db-url" -audit-database-url-file "$work/audit-db-url"
)
"$work/generated/kubectl-secrets.sh"

apply_identity() {
  local name="$1" directory="$2"; shift 2
  kubectl -n "$NAMESPACE" create secret generic "$name" --from-file="$directory" "$@" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}
apply_identity praetor-api-identity "$work/generated/clients/praetor-api"
apply_identity praetor-scheduler-identity "$work/generated/clients/praetor-scheduler"
apply_identity praetor-executor-identity "$work/generated/clients/praetor-executor"

helm dependency build "$SECRETS_CHART" >/dev/null
helm upgrade --install praetor-secrets-stack "$SECRETS_CHART" -n "$NAMESPACE" \
  --set praetor-secrets.trustDomain="$TRUST_DOMAIN" \
  --set praetor-secrets.secrets.runtimeSecret=praetor-secrets-runtime \
  --set praetor-secrets.secrets.serverTLSSecret=praetor-secrets-server \
  --set praetor-secrets.secrets.auditSinkTLSSecret=praetor-secrets-audit-client \
  --set praetor-audit-sink.trustDomain="$TRUST_DOMAIN" \
  --set praetor-audit-sink.secrets.runtimeSecret=praetor-audit-runtime \
  --set praetor-audit-sink.secrets.serverTLSSecret=praetor-audit-server \
  --wait --timeout 5m

release_values="$work/release-values.yaml"
(cd "$ROOT" && go run ./cmd/compatcheck -output helm-values) >"$release_values"
helm upgrade --install praetor "$CHART" -n "$NAMESPACE" \
  -f "$ROOT/deployments/helm/praetor-v2/ci/values-k3d-local.yaml" -f "$release_values" \
  --set ingress.enabled=false \
  --set secretsService.enabled=true \
  --set secretsService.url="https://praetor-secrets.$NAMESPACE.svc:8443" \
  --set secretsService.trustDomain="$TRUST_DOMAIN" \
  --set secretsService.apiIdentitySecret=praetor-api-identity \
  --set secretsService.schedulerIdentitySecret=praetor-scheduler-identity \
  --set secretsService.executorIdentitySecret=praetor-executor-identity \
  --wait --timeout 10m
