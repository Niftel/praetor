#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SECRETS_ROOT="${PRAETOR_SECRETS_ROOT:-$ROOT/../praetor-secrets}"
SCHEDULER_ROOT="${PRAETOR_SCHEDULER_ROOT:-$ROOT/../scheduler}"
EXECUTOR_ROOT="${PRAETOR_EXECUTOR_ROOT:-$ROOT/../executor}"
INGESTION_ROOT="${PRAETOR_INGESTION_ROOT:-$ROOT/../ingestion}"
CONSUMER_ROOT="${PRAETOR_CONSUMER_ROOT:-$ROOT/../consumer}"
RECONCILER_ROOT="${PRAETOR_RECONCILER_ROOT:-$ROOT/../reconciler}"
USE_RELEASED_COMPONENTS="${PRAETOR_VALIDATION_USE_RELEASED_COMPONENTS:-false}"
NAMESPACE="${PRAETOR_VALIDATION_NAMESPACE:-praetor-secrets}"
TRUST_DOMAIN="${PRAETOR_VALIDATION_TRUST_DOMAIN:-praetor.local}"
CLUSTER="${PRAETOR_K3D_CLUSTER:-praetor-validation}"
SECRETS_IMAGE="${PRAETOR_VALIDATION_SECRETS_IMAGE:-praetor-secrets:validation}"
CHART="$ROOT/deployments/helm/praetor-v2"
SECRETS_CHART="$SECRETS_ROOT/charts/praetor-secrets-stack"

for command in docker go helm k3d kubectl; do command -v "$command" >/dev/null || { echo "error: $command is required" >&2; exit 1; }; done
[[ -f "$SECRETS_ROOT/cmd/praetor-dev-bootstrap/main.go" ]] || { echo "error: praetor-secrets checkout not found at $SECRETS_ROOT" >&2; exit 1; }
if [[ "$USE_RELEASED_COMPONENTS" != true ]]; then
  for component_root in "$SCHEDULER_ROOT" "$EXECUTOR_ROOT" "$INGESTION_ROOT" "$CONSUMER_ROOT" "$RECONCILER_ROOT"; do
    [[ -f "$component_root/Dockerfile" ]] || { echo "error: component checkout with Dockerfile not found at $component_root" >&2; exit 1; }
  done
fi
[[ "$SECRETS_IMAGE" == *:* && "$SECRETS_IMAGE" != *@* ]] || { echo "error: PRAETOR_VALIDATION_SECRETS_IMAGE must be a tagged image name" >&2; exit 1; }
kubectl get --raw=/readyz >/dev/null

docker build --tag "$SECRETS_IMAGE" "$SECRETS_ROOT"
validation_tag="validation"
docker build --file "$ROOT/build/package/Dockerfile.api" --tag "praetor-api:$validation_tag" "$ROOT"
docker build --file "$ROOT/build/package/Dockerfile.migrator" --tag "praetor-migrator:$validation_tag" "$ROOT"
docker build --tag "praetor-ui:$validation_tag" "$ROOT/web"

released_component_ref() {
  local component="$1"
  awk -v component="$component" '
    $1 == "registry:" { registry = $2 }
    $1 == component ":" { selected = 1; next }
    selected && $1 == "image:" { image = $2 }
    selected && $1 == "digest:" { print registry "/" image "@" $2; exit }
  ' "$ROOT/deployments/staging/release-lock.yaml"
}

if [[ "$USE_RELEASED_COMPONENTS" == true ]]; then
  # The sibling repositories are unchanged by this PR. Reuse the immutable,
  # digest-addressed platform release instead of rebuilding five repositories.
  # Retagging keeps the existing air-gapped k3d import and pullPolicy=Never
  # checks identical to the source-build path.
  docker_arch="$(docker version --format '{{.Server.Arch}}')"
  case "$docker_arch" in
    x86_64) docker_arch=amd64 ;;
    aarch64) docker_arch=arm64 ;;
  esac
  pull_released_component() {
    local component="$1" released_ref index_json platform_digest platform_ref
    released_ref="$(released_component_ref "$component")"
    [[ "$released_ref" == ghcr.io/niftel/*@sha256:* ]] || { echo "error: release lock has no immutable $component image" >&2; exit 1; }
    index_json="$(docker buildx imagetools inspect --raw "$released_ref")"
    platform_digest="$(jq -r --arg arch "$docker_arch" 'first(.manifests[]? | select(.platform.os == "linux" and .platform.architecture == $arch) | .digest) // empty' <<<"$index_json")"
    platform_ref="$released_ref"
    if [[ -n "$platform_digest" ]]; then
      platform_ref="${released_ref%@*}@$platform_digest"
    fi
    docker pull "$platform_ref"
    docker tag "$platform_ref" "praetor-$component:$validation_tag"
  }
  released_pids=()
  for component in scheduler executor ingestion consumer reconciler; do
    pull_released_component "$component" &
    released_pids+=("$!")
  done
  released_pull_failed=0
  for pid in "${released_pids[@]}"; do
    wait "$pid" || released_pull_failed=1
  done
  (( released_pull_failed == 0 )) || { echo "error: one or more released validation images could not be pulled" >&2; exit 1; }
else
  docker build --tag "praetor-scheduler:$validation_tag" "$SCHEDULER_ROOT"
  docker build --tag "praetor-executor:$validation_tag" "$EXECUTOR_ROOT"
  docker build --tag "praetor-ingestion:$validation_tag" "$INGESTION_ROOT"
  docker build --tag "praetor-consumer:$validation_tag" "$CONSUMER_ROOT"
  docker build --tag "praetor-reconciler:$validation_tag" "$RECONCILER_ROOT"
fi
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

# Direct mode avoids the tools-node shared-tarball race. Import each image in a
# separate stream: a single multi-image stream can fail part-way through with a
# missing content digest, leaving an indeterminate subset in containerd.
for image in "${validation_images[@]}"; do
  k3d image import --mode direct --cluster "$CLUSTER" "$image"
done

# Do not start Helm when an image import was incomplete. Without this check,
# Kubernetes falls back to Docker Hub and the fixture wastes several minutes in
# ImagePullBackOff before Helm eventually times out.
probe_index=0
for image in "${validation_images[@]}"; do
  probe="praetor-image-probe-$probe_index"
  kubectl run "$probe" --image="$image" --image-pull-policy=Never --restart=Never \
    --command -- /bin/sh -c 'exit 0' >/dev/null
  phase=""
  for _ in {1..20}; do
    phase="$(kubectl get pod "$probe" -o jsonpath='{.status.phase}')"
    [[ "$phase" == Succeeded || "$phase" == Failed ]] && break
    sleep 1
  done
  if [[ "$phase" != Succeeded ]]; then
    echo "error: imported image $image could not start with imagePullPolicy=Never" >&2
    kubectl describe pod "$probe" >&2 || true
    kubectl delete pod "$probe" --ignore-not-found --wait=false >/dev/null 2>&1 || true
    exit 1
  fi
  kubectl delete pod "$probe" --wait=false >/dev/null
  probe_index=$((probe_index + 1))
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

# A clean cluster has no retained pack PVC content. Seed the deterministic,
# validation-only runtime before any journey launches a real job.
PRAETOR_EXECUTOR_ROOT="$EXECUTOR_ROOT" \
  "$ROOT/scripts/stage-validation-execution-pack.sh"
