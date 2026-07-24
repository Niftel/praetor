#!/usr/bin/env bash
set -euo pipefail

# Rebuild and deploy the Praetor components owned by this repository to the
# local k3d cluster. Existing Helm values and PostgreSQL data are preserved.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLUSTER="${PRAETOR_K3D_CLUSTER:-praetor-test}"
RELEASE="${PRAETOR_HELM_RELEASE:-praetor}"
NAMESPACE="${PRAETOR_NAMESPACE:-praetor}"
CHART="$ROOT/deployments/helm/praetor-v2"
VALUES="$CHART/ci/values-k3d-local.yaml"
SCHEDULER_ROOT="${PRAETOR_SCHEDULER_ROOT:-$ROOT/../scheduler}"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command '$1' is not installed" >&2
    exit 1
  }
}

for command in docker k3d kubectl helm jq go; do
  need "$command"
done

if [[ ! -f "$SCHEDULER_ROOT/Dockerfile" ]]; then
  echo "error: scheduler checkout not found at '$SCHEDULER_ROOT'" >&2
  echo "set PRAETOR_SCHEDULER_ROOT to its location" >&2
  exit 1
fi

# A unique tag makes every local deployment immutable, including builds with
# uncommitted changes. The two revisions keep it traceable; the UTC build ID
# prevents collisions. Helm sees the image reference change and rolls workloads
# itself, so no forced restart is needed.
if [[ -n "${PRAETOR_IMAGE_TAG:-}" ]]; then
  TAG="$PRAETOR_IMAGE_TAG"
else
  need git
  PRAETOR_REV="$(git -C "$ROOT" rev-parse --short=12 HEAD)"
  SCHEDULER_REV="$(git -C "$SCHEDULER_ROOT" rev-parse --short=12 HEAD)"
  BUILD_ID="$(date -u +%Y%m%d%H%M%S)"
  TAG="local-${PRAETOR_REV}-${SCHEDULER_REV}-${BUILD_ID}"
fi

# Layer the exact released component set under the four locally built
# components. Never use image.tag here: it is a global override and would point
# externally owned services at local images that this script does not build.
PLATFORM_VALUES="$(mktemp "${TMPDIR:-/tmp}/praetor-local-platform.XXXXXX.yaml")"
LOCAL_IMAGE_VALUES="$(mktemp "${TMPDIR:-/tmp}/praetor-local-images.XXXXXX.yaml")"
RENDERED="$(mktemp "${TMPDIR:-/tmp}/praetor-local-rendered.XXXXXX.yaml")"
DESIRED_IMAGES="$(mktemp "${TMPDIR:-/tmp}/praetor-local-desired-images.XXXXXX.txt")"
RELEASE_IMAGES="$(mktemp "${TMPDIR:-/tmp}/praetor-local-release-images.XXXXXX.txt")"
HELM_PID=""
cleanup() {
  if [[ -n "$HELM_PID" ]] && kill -0 "$HELM_PID" 2>/dev/null; then
    kill "$HELM_PID" 2>/dev/null || true
    wait "$HELM_PID" 2>/dev/null || true
  fi
  rm -f "$PLATFORM_VALUES" "$LOCAL_IMAGE_VALUES" "$RENDERED" "$DESIRED_IMAGES" "$RELEASE_IMAGES"
}
trap cleanup EXIT INT TERM

(
  cd "$ROOT"
  GOWORK=off go run ./cmd/compatcheck -output helm-values
) >"$PLATFORM_VALUES"
(
  cd "$ROOT"
  GOWORK=off go run ./cmd/compatcheck -output images
) >"$RELEASE_IMAGES"

cat >"$LOCAL_IMAGE_VALUES" <<EOF
imageRegistries:
  api: ""
  consumer: ""
  executor: ""
  ingestion: ""
  migrator: ""
  reconciler: ""
  scheduler: ""
  ui: ""
imageTags:
  api: "$TAG"
  migrator: "$TAG"
  scheduler: "$TAG"
  ui: "$TAG"
EOF

# Recover a partial Docker/k3d restart before building. In particular, never
# leave serverlb crash-looping while server-0 is stopped.
"$ROOT/scripts/local-cluster.sh" start

echo "==> Building local images with immutable tag '$TAG'"
docker build -f "$ROOT/build/package/Dockerfile.api" \
  --provenance=false \
  -t "praetor-api:$TAG" "$ROOT"
docker build -f "$ROOT/build/package/Dockerfile.migrator" \
  --provenance=false \
  -t "praetor-migrator:$TAG" "$ROOT"
docker build -f "$ROOT/web/Dockerfile" \
  --provenance=false \
  -t "praetor-ui:$TAG" "$ROOT/web"
docker build -f "$SCHEDULER_ROOT/Dockerfile" \
  --provenance=false \
  -t "praetor-scheduler:$TAG" "$SCHEDULER_ROOT"

echo "==> Importing images into k3d cluster '$CLUSTER'"
IMPORT_IMAGES=(
  "praetor-api:$TAG"
  "praetor-migrator:$TAG"
  "praetor-ui:$TAG"
  "praetor-scheduler:$TAG"
)
while IFS= read -r image; do
  case "$image" in
    */praetor-api:*|*/praetor-migrator:*|*/praetor-scheduler:*|*/praetor-ui:*)
      continue
      ;;
  esac
  echo "==> Pulling released dependency image '$image'"
  if ! docker pull "$image"; then
    echo "error: cannot pull private released image '$image'" >&2
    echo "authenticate Docker to ghcr.io, then rerun this updater" >&2
    exit 1
  fi
  if [[ ! "$image" =~ ^ghcr\.io/niftel/[a-z0-9._-]+:[A-Za-z0-9._-]+$ ]]; then
    echo "error: released image has an unexpected reference: '$image'" >&2
    exit 1
  fi
  local_image="${image##*/}"
  printf 'FROM %s\n' "$image" |
    docker build --provenance=false -t "$local_image" -
  IMPORT_IMAGES+=("$local_image")
done <"$RELEASE_IMAGES"
for image in "${IMPORT_IMAGES[@]}"; do
  k3d image import -c "$CLUSTER" "$image"
  expected="docker.io/library/$image"
  docker exec "k3d-$CLUSTER-server-0" ctr -n k8s.io images ls -q |
    grep -Fxq "$expected" || {
      echo "error: k3d reported success but '$expected' is absent from the node" >&2
      exit 1
    }
done

echo "==> Verifying local and released image ownership"
helm template "$RELEASE" "$CHART" \
  -f "$VALUES" \
  -f "$PLATFORM_VALUES" \
  -f "$LOCAL_IMAGE_VALUES" \
  -n "$NAMESPACE" >"$RENDERED"
awk '$1 == "image:" { print $2 }' "$RENDERED" | sort -u >"$DESIRED_IMAGES"
for image in api migrator scheduler ui; do
  grep -Fq "image: praetor-$image:$TAG" "$RENDERED" || {
    echo "error: rendered local $image image does not use imported tag '$TAG'" >&2
    exit 1
  }
done
while IFS= read -r image; do
  case "$image" in
    */praetor-api:*|*/praetor-migrator:*|*/praetor-scheduler:*|*/praetor-ui:*)
      continue
      ;;
  esac
  local_image="${image##*/}"
  grep -Fq "image: $local_image" "$RENDERED" || {
    echo "error: rendered release is missing externally owned image '$image'" >&2
    exit 1
  }
done <"$RELEASE_IMAGES"

echo "==> Upgrading Helm release '$RELEASE' in namespace '$NAMESPACE'"
"$ROOT/scripts/helm-statefulset-preflight.sh" "$RELEASE" "$NAMESPACE" "$CHART" \
  -f "$VALUES" \
  -f "$PLATFORM_VALUES" \
  -f "$LOCAL_IMAGE_VALUES"
helm upgrade --install "$RELEASE" "$CHART" \
  -f "$VALUES" \
  -f "$PLATFORM_VALUES" \
  -f "$LOCAL_IMAGE_VALUES" \
  -n "$NAMESPACE" \
  --create-namespace \
  --wait \
  --timeout 10m &
HELM_PID=$!

# Helm's waiter reports an unavailable release only at the timeout. Surface
# deterministic image/configuration failures as soon as kubelet records them.
while kill -0 "$HELM_PID" 2>/dev/null; do
  failures="$(
    kubectl get pods -n "$NAMESPACE" \
      -l app.kubernetes.io/instance="$RELEASE" \
      -o json 2>/dev/null |
      jq -r --rawfile desired "$DESIRED_IMAGES" '
        .items[] as $pod
        | (($pod.status.initContainerStatuses // []) + ($pod.status.containerStatuses // []))[]
        | . as $status
        | select(($desired | split("\n") | index($status.image)) != null)
        | select(.state.waiting.reason |
            IN("ErrImagePull", "ImagePullBackOff", "InvalidImageName", "CreateContainerConfigError"))
        | "\($pod.metadata.name)\t\(.name)\t\(.image)\t\(.state.waiting.reason)\t\(.state.waiting.message // "")"
      ' || true
  )"
  if [[ -n "$failures" ]]; then
    echo "error: local release entered an unrecoverable container state:" >&2
    echo "$failures" >&2
    kill "$HELM_PID" 2>/dev/null || true
    wait "$HELM_PID" 2>/dev/null || true
    HELM_PID=""
    exit 1
  fi
  sleep 2
done
wait "$HELM_PID"
HELM_PID=""

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
