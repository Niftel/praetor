#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SECRETS_ROOT="${PRAETOR_SECRETS_ROOT:-$ROOT/../praetor-secrets}"
SCHEDULER_ROOT="${PRAETOR_SCHEDULER_ROOT:-$ROOT/../scheduler}"
EXECUTOR_ROOT="${PRAETOR_EXECUTOR_ROOT:-$ROOT/../executor}"
INGESTION_ROOT="${PRAETOR_INGESTION_ROOT:-$ROOT/../ingestion}"
CONSUMER_ROOT="${PRAETOR_CONSUMER_ROOT:-$ROOT/../consumer}"
RECONCILER_ROOT="${PRAETOR_RECONCILER_ROOT:-$ROOT/../reconciler}"
NAMESPACE="${PRAETOR_VALIDATION_NAMESPACE:-praetor-secrets}"
TRUST_DOMAIN="${PRAETOR_VALIDATION_TRUST_DOMAIN:-praetor.local}"
CLUSTER="${PRAETOR_K3D_CLUSTER:-praetor-validation}"
SECRETS_IMAGE="${PRAETOR_VALIDATION_SECRETS_IMAGE:-praetor-secrets:validation}"
CHART="$ROOT/deployments/helm/praetor-v2"
SECRETS_CHART="$SECRETS_ROOT/charts/praetor-secrets-stack"

for command in docker go helm k3d kubectl; do command -v "$command" >/dev/null || { echo "error: $command is required" >&2; exit 1; }; done
[[ -f "$SECRETS_ROOT/cmd/praetor-dev-bootstrap/main.go" ]] || { echo "error: praetor-secrets checkout not found at $SECRETS_ROOT" >&2; exit 1; }
for component_root in "$SCHEDULER_ROOT" "$EXECUTOR_ROOT" "$INGESTION_ROOT" "$CONSUMER_ROOT" "$RECONCILER_ROOT"; do
  [[ -f "$component_root/Dockerfile" ]] || { echo "error: component checkout with Dockerfile not found at $component_root" >&2; exit 1; }
done
[[ "$SECRETS_IMAGE" == *:* && "$SECRETS_IMAGE" != *@* ]] || { echo "error: PRAETOR_VALIDATION_SECRETS_IMAGE must be a tagged image name" >&2; exit 1; }
kubectl get --raw=/readyz >/dev/null

docker build --tag "$SECRETS_IMAGE" "$SECRETS_ROOT"
validation_tag="validation"
docker build --file "$ROOT/build/package/Dockerfile.api" --tag "praetor-api:$validation_tag" "$ROOT"
docker build --file "$ROOT/build/package/Dockerfile.migrator" --tag "praetor-migrator:$validation_tag" "$ROOT"
docker build --tag "praetor-ui:$validation_tag" "$ROOT/web"
docker build --tag "praetor-scheduler:$validation_tag" "$SCHEDULER_ROOT"
docker build --tag "praetor-executor:$validation_tag" "$EXECUTOR_ROOT"
docker build --tag "praetor-ingestion:$validation_tag" "$INGESTION_ROOT"
docker build --tag "praetor-consumer:$validation_tag" "$CONSUMER_ROOT"
docker build --tag "praetor-reconciler:$validation_tag" "$RECONCILER_ROOT"
k3d image import --cluster "$CLUSTER" \
  "$SECRETS_IMAGE" \
  "praetor-api:$validation_tag" \
  "praetor-migrator:$validation_tag" \
  "praetor-ui:$validation_tag" \
  "praetor-scheduler:$validation_tag" \
  "praetor-executor:$validation_tag" \
  "praetor-ingestion:$validation_tag" \
  "praetor-consumer:$validation_tag" \
  "praetor-reconciler:$validation_tag"

# Do not start Helm when an image import was incomplete. Without this check,
# Kubernetes falls back to Docker Hub and the fixture wastes several minutes in
# ImagePullBackOff before Helm eventually times out.
validation_images=(
  "$SECRETS_IMAGE"
  "praetor-api:$validation_tag"
  "praetor-migrator:$validation_tag"
  "praetor-ui:$validation_tag"
  "praetor-scheduler:$validation_tag"
  "praetor-executor:$validation_tag"
  "praetor-ingestion:$validation_tag"
  "praetor-consumer:$validation_tag"
  "praetor-reconciler:$validation_tag"
)
cluster_nodes=()
while IFS= read -r node; do
  cluster_nodes+=("$node")
done < <(k3d node list --no-headers | awk -v cluster="k3d-$CLUSTER-" '$1 ~ "^" cluster {print $1}')
(( ${#cluster_nodes[@]} > 0 )) || { echo "error: no nodes found for k3d cluster $CLUSTER" >&2; exit 1; }
for node in "${cluster_nodes[@]}"; do
  imported_images="$(docker exec "$node" k3s ctr --namespace k8s.io images list --quiet)"
  for image in "${validation_images[@]}"; do
    if ! grep -Fxq "docker.io/library/$image" <<<"$imported_images" && ! grep -Fxq "$image" <<<"$imported_images"; then
      echo "error: k3d image import did not load $image into $node" >&2
      exit 1
    fi
  done
done
secrets_repository="${SECRETS_IMAGE%:*}"
secrets_tag="${SECRETS_IMAGE##*:}"

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
  --set praetor-secrets.image.repository="$secrets_repository" \
  --set praetor-secrets.image.tag="$secrets_tag" \
  --set praetor-secrets.image.pullPolicy=IfNotPresent \
  --set praetor-secrets.trustDomain="$TRUST_DOMAIN" \
  --set praetor-secrets.secrets.runtimeSecret=praetor-secrets-runtime \
  --set praetor-secrets.secrets.serverTLSSecret=praetor-secrets-server \
  --set praetor-secrets.secrets.auditSinkTLSSecret=praetor-secrets-audit-client \
  --set praetor-audit-sink.trustDomain="$TRUST_DOMAIN" \
  --set praetor-audit-sink.image.repository="$secrets_repository" \
  --set praetor-audit-sink.image.tag="$secrets_tag" \
  --set praetor-audit-sink.image.pullPolicy=IfNotPresent \
  --set praetor-audit-sink.secrets.runtimeSecret=praetor-audit-runtime \
  --set praetor-audit-sink.secrets.serverTLSSecret=praetor-audit-server \
  --wait --timeout 5m

release_values="$work/release-values.yaml"
(cd "$ROOT" && go run ./cmd/compatcheck -output helm-values) >"$release_values"
praetor_helm_args=(
  -f "$ROOT/deployments/helm/praetor-v2/ci/values-k3d-local.yaml" -f "$release_values" \
  --set image.registry= \
  --set image.tag="$validation_tag" \
  --set ingress.enabled=false \
  --set hostRunner.callbackUrl="http://praetor-ingestion:8081" \
  --set secretsService.enabled=true \
  --set secretsService.url="https://praetor-secrets.$NAMESPACE.svc:8443" \
  --set secretsService.trustDomain="$TRUST_DOMAIN" \
  --set secretsService.apiIdentitySecret=praetor-api-identity \
  --set secretsService.schedulerIdentitySecret=praetor-scheduler-identity \
  --set secretsService.executorIdentitySecret=praetor-executor-identity
)
"$ROOT/scripts/helm-statefulset-preflight.sh" praetor "$NAMESPACE" "$CHART" "${praetor_helm_args[@]}"
helm upgrade --install praetor "$CHART" -n "$NAMESPACE" \
  "${praetor_helm_args[@]}" \
  --wait --timeout 10m

# The validation bootstrap intentionally rotates its generated workload
# identities on every run. Those identities are supplied through externally
# managed Secrets, so Helm cannot detect their content changes and would leave
# existing pods using stale certificates. Restart every client after the
# release is reconciled so repeated bootstraps remain deterministic.
kubectl rollout restart \
  deployment/praetor-api \
  deployment/praetor-scheduler \
  deployment/praetor-ingestion \
  deployment/praetor-consumer \
  deployment/praetor-reconciler \
  statefulset/praetor-executor \
  -n "$NAMESPACE"
for workload in \
  deployment/praetor-api \
  deployment/praetor-scheduler \
  deployment/praetor-ingestion \
  deployment/praetor-consumer \
  deployment/praetor-reconciler \
  statefulset/praetor-executor; do
  kubectl rollout status "$workload" -n "$NAMESPACE" --timeout=180s
done
