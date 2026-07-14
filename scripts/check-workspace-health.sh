#!/usr/bin/env bash
# Check extracted Praetor services as independent repositories. This is a local
# integration-repository command: service CI remains owned by each service.

set -u

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
workspace_dir=${PRAETOR_WORKSPACE_DIR:-$(dirname "$root_dir")}
cache_root=${PRAETOR_HEALTH_CACHE:-${TMPDIR:-/tmp}/praetor-workspace-health}
default_services=(scheduler reconciler executor ingestion consumer)
if (($# > 0)); then
    services=("$@")
else
    services=("${default_services[@]}")
fi
failures=0

run_check() {
    local service=$1
    local check=$2
    local repo="$workspace_dir/$service"
    local cache="$cache_root/$service"

    if (cd "$repo" && GOWORK=off GOCACHE="$cache" go "$check" ./...); then
        printf 'PASS  %-11s %s\n' "$service" "$check"
    else
        printf 'FAIL  %-11s %s\n' "$service" "$check"
        failures=$((failures + 1))
    fi
}

printf 'Praetor workspace: %s\n' "$workspace_dir"
printf 'Mode: GOWORK=off\n\n'

for service in "${services[@]}"; do
    repo="$workspace_dir/$service"
    if [[ ! -f "$repo/go.mod" ]]; then
        printf 'MISS  %-11s expected %s/go.mod\n' "$service" "$repo"
        failures=$((failures + 1))
        continue
    fi

    run_check "$service" vet
    run_check "$service" build
    run_check "$service" test
done

printf '\n'
if ((failures > 0)); then
    printf 'Workspace health failed with %d failed or missing check(s).\n' "$failures"
    exit 1
fi

printf 'Workspace health passed for all %d extracted services.\n' "${#services[@]}"
