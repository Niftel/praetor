#!/usr/bin/env bash
set -euo pipefail

# Convert a newline-delimited list of repository paths into the smallest safe
# Praetor validation plan. This is deliberately dependency-free so the exact
# same planner runs before a local push and inside GitHub Actions.

run_go=false
run_ui=false
run_database=false
run_deployment=false
run_security=false
run_product=false
image_api=false
image_migrator=false
image_ui=false
force_all=false

if [[ "${1:-}" == "--all" ]]; then
  force_all=true
elif [[ $# -gt 0 ]]; then
  echo "usage: $0 [--all]" >&2
  exit 2
fi

select_all_gates() {
  run_go=true
  run_ui=true
  run_database=true
  run_deployment=true
  run_security=true
  run_product=true
}

if [[ "$force_all" == true ]]; then
  select_all_gates
  image_api=true
  image_migrator=true
  image_ui=true
else
  saw_path=false
  saw_non_docs=false
  while IFS= read -r path; do
    [[ -z "$path" ]] && continue
    saw_path=true

    case "$path" in
      *.md|docs-site/*|.github/ISSUE_TEMPLATE/*|.github/PULL_REQUEST_TEMPLATE*)
        continue
        ;;
    esac
    saw_non_docs=true

    # Changes to the planner or its callers deliberately exercise every gate.
    case "$path" in
      scripts/plan-ci.sh|scripts/github-ci-plan.sh)
        select_all_gates
        image_api=true
        image_migrator=true
        image_ui=true
        continue
        ;;
      scripts/verify-changed.sh|Makefile|.github/workflows/test.yml)
        select_all_gates
        continue
        ;;
    esac

    case "$path" in
      *.go|go.mod|go.sum)
        run_go=true
        run_security=true
        ;;
    esac

    case "$path" in
      go.mod|go.sum)
        run_database=true
        image_api=true
        image_migrator=true
        ;;
      cmd/api/*|services/api/*|pkg/*|internal/*)
        image_api=true
        ;;
      cmd/migrator/*|db/migrations/*|build/package/Dockerfile.migrator)
        image_migrator=true
        ;;
      build/package/Dockerfile.api)
        image_api=true
        ;;
    esac

    case "$path" in
      web/*)
        run_ui=true
        case "$path" in
          *.test.ts|*.test.tsx|web/test/*) ;;
          *) image_ui=true ;;
        esac
        ;;
    esac

    case "$path" in
      db/*|services/api/handlers/*|services/api/middleware/*|scripts/database-compatibility.sh|tests/database_compatibility_test.go)
        run_database=true
        ;;
    esac

    case "$path" in
      deployments/*|build/package/*|scripts/*deploy*|scripts/*cluster*|scripts/*helm*|tests/*deploy*|.github/workflows/image.yml)
        run_deployment=true
        ;;
    esac

    case "$path" in
      .github/workflows/*)
        run_deployment=true
        ;;
    esac

    case "$path" in
      .github/gosec-high-baseline.json|.github/workflows/gosec.yml|.github/workflows/govulncheck.yml|scripts/check-gosec-baseline.sh)
        run_security=true
        ;;
    esac

    case "$path" in
      deployments/product-validation/*|deployments/helm/praetor-v2/*|scripts/bootstrap-product-validation-base.sh|scripts/check-product-validation-fast.sh|scripts/classify-product-validation.sh|scripts/plan-product-validation.sh|scripts/product-validation-fixture.sh|scripts/generate-readiness-report.sh|scripts/validate-*-e2e.sh|scripts/validate-ldap-operator-journey.sh|scripts/test-secrets-execution-e2e.sh|cmd/readiness-report/*|internal/readiness/*|playbooks/validate-execution-recovery.yml|tests/dynamic_inventory_staging_contract_test.go|tests/inventory_sync_history_contract_test.go|.github/workflows/product-validation-fixture.yml)
        run_product=true
        ;;
    esac

    case "$path" in
      .github/workflows/codeql.yml)
        run_go=true
        run_ui=true
        run_security=true
        ;;
      .github/workflows/image.yml)
        image_api=true
        image_migrator=true
        image_ui=true
        ;;
    esac
  done

  # New non-documentation areas must not silently bypass CI. The Go gate is the
  # conservative fallback until the path is explicitly classified.
  if [[ "$saw_path" == true && "$saw_non_docs" == true && "$run_go" == false && "$run_ui" == false && "$run_database" == false && "$run_deployment" == false && "$run_security" == false && "$run_product" == false ]]; then
    run_go=true
  fi
fi

images=()
matrix=()
if [[ "$image_api" == true ]]; then
  images+=(api)
  matrix+=('{"name":"api","context":".","dockerfile":"build/package/Dockerfile.api"}')
fi
if [[ "$image_migrator" == true ]]; then
  images+=(migrator)
  matrix+=('{"name":"migrator","context":".","dockerfile":"build/package/Dockerfile.migrator"}')
fi
if [[ "$image_ui" == true ]]; then
  images+=(ui)
  matrix+=('{"name":"ui","context":"web","dockerfile":"Dockerfile"}')
fi

join_by() {
  local separator="$1"
  shift
  local result=""
  local value
  for value in "$@"; do
    [[ -n "$result" ]] && result+="$separator"
    result+="$value"
  done
  printf '%s' "$result"
}

image_names="$(join_by , "${images[@]:-}")"
matrix_entries="$(join_by , "${matrix[@]:-}")"
run_images=false
[[ ${#images[@]} -gt 0 ]] && run_images=true

codeql=()
if [[ "$run_security" == true ]]; then
  codeql+=('{"language":"go","build_mode":"autobuild"}')
fi
if [[ "$run_ui" == true ]]; then
  codeql+=('{"language":"javascript-typescript","build_mode":"none"}')
fi
codeql_entries="$(join_by , "${codeql[@]:-}")"
run_codeql=false
[[ ${#codeql[@]} -gt 0 ]] && run_codeql=true

printf 'run_go=%s\n' "$run_go"
printf 'run_ui=%s\n' "$run_ui"
printf 'run_database=%s\n' "$run_database"
printf 'run_deployment=%s\n' "$run_deployment"
printf 'run_security=%s\n' "$run_security"
printf 'run_product=%s\n' "$run_product"
printf 'run_images=%s\n' "$run_images"
printf 'images=%s\n' "$image_names"
printf 'image_matrix={"include":[%s]}\n' "$matrix_entries"
printf 'run_codeql=%s\n' "$run_codeql"
printf 'codeql_matrix={"include":[%s]}\n' "$codeql_entries"
