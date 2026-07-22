#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_REF="${BASE_REF:-origin/main}"
BUILD_IMAGES=false

if [[ "${1:-}" == "--images" ]]; then
  BUILD_IMAGES=true
elif [[ $# -gt 0 ]]; then
  echo "usage: $0 [--images]" >&2
  exit 2
fi

cd "$ROOT"

if ! git rev-parse --verify "$BASE_REF" >/dev/null 2>&1; then
  echo "error: base ref '$BASE_REF' does not exist; fetch it or set BASE_REF" >&2
  exit 2
fi

paths="$({
  git diff --name-only "$BASE_REF"...HEAD
  git diff --name-only
  git ls-files --others --exclude-standard
} | sort -u)"

if [[ -z "$paths" ]]; then
  echo "verify-changed: no changes relative to $BASE_REF"
  exit 0
fi

plan="$(printf '%s\n' "$paths" | ./scripts/plan-ci.sh)"
echo "verify-changed: plan for changes relative to $BASE_REF"
printf '%s\n' "$plan"

plan_value() {
  local key="$1"
  printf '%s\n' "$plan" | sed -n "s/^${key}=//p"
}

if [[ "$(plan_value run_go)" == true ]]; then
  GOWORK=off go run ./cmd/compatcheck
  GOWORK=off go vet ./...
  GOWORK=off go build ./...
  GOWORK=off go test ./...
fi

if [[ "$(plan_value run_ui)" == true ]]; then
  if [[ ! -d web/node_modules ]]; then
    npm --prefix web ci
  fi
  npm --prefix web test
  npm --prefix web run build:check
fi

if [[ "$(plan_value run_database)" == true ]]; then
  GOWORK=off go test -count=1 ./services/api/handlers ./services/api/middleware
  if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
    echo "verify-changed: live database checks use TEST_DATABASE_URL when available"
  fi
fi

if [[ "$(plan_value run_deployment)" == true || "$(plan_value run_product)" == true ]]; then
  GOWORK=off make workflow-lint
  GOWORK=off ./scripts/check-product-validation-fast.sh
fi

if [[ "$(plan_value run_security)" == true ]]; then
  GOWORK=off make gosec
fi

if [[ "$BUILD_IMAGES" == true && "$(plan_value run_images)" == true ]]; then
  IFS=, read -r -a images <<<"$(plan_value images)"
  for image in "${images[@]}"; do
    case "$image" in
      api)
        docker build -f build/package/Dockerfile.api -t praetor-api:local-ci .
        ;;
      migrator)
        docker build -f build/package/Dockerfile.migrator -t praetor-migrator:local-ci .
        ;;
      ui)
        docker build -f web/Dockerfile -t praetor-ui:local-ci web
        ;;
      *)
        echo "error: unsupported planned image '$image'" >&2
        exit 2
        ;;
    esac
  done
fi

echo "verify-changed: all planned local gates passed"
