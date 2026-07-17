#!/usr/bin/env bash
# Validate deployable services and independently released shared modules without
# allowing go.work or sibling source replacements to hide dependency problems.

set -uo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
workspace_dir=${PRAETOR_WORKSPACE_DIR:-$(dirname "$root_dir")}
cache_root=${PRAETOR_HEALTH_CACHE:-${TMPDIR:-/tmp}/praetor-workspace-health}
remote=false
scope=all
declare -a selected=()
default_services=(scheduler reconciler executor ingestion consumer)

usage() {
    printf 'usage: %s [--remote] [--services|--modules] [name ...]\n' "$0" >&2
}

while (($# > 0)); do
    case $1 in
        --remote) remote=true ;;
        --services) scope=services ;;
        --modules) scope=modules ;;
        --help|-h) usage; exit 0 ;;
        --*) usage; exit 2 ;;
        *) selected+=("$1") ;;
    esac
    shift
done

mkdir -p "$cache_root"
failures=0
checked=0

if ! module_output=$(
    cd "$root_dir" &&
        GOWORK=off GOCACHE="$cache_root/manifest-go-build" \
        go run ./cmd/compatcheck -output shared-modules
); then
    printf 'unable to load shared-module release metadata\n' >&2
    exit 1
fi
mapfile -t module_rows <<<"$module_output"

declare -A module_row_by_name=()
for row in "${module_rows[@]}"; do
    IFS=$'\t' read -r name _ <<<"$row"
    module_row_by_name["$name"]=$row
done

run_go_check() {
    local kind=$1
    local name=$2
    local repo=$3
    local check=$4
    local cache="$cache_root/go-build/$kind/$name"
    local modcache="$cache_root/go-mod/$kind/$name"

    if (cd "$repo" && GOWORK=off GOCACHE="$cache" GOMODCACHE="$modcache" go "$check" ./...); then
        printf 'PASS  %-8s %-12s %s\n' "$kind" "$name" "$check"
    else
        printf 'FAIL  %-8s %-12s %s\n' "$kind" "$name" "$check"
        failures=$((failures + 1))
    fi
}

run_format_check() {
    local kind=$1
    local name=$2
    local repo=$3
    local unformatted
    unformatted=$(cd "$repo" && find . -type f -name '*.go' -not -path './vendor/*' -print0 | xargs -0 gofmt -l)
    if [[ -z $unformatted ]]; then
        printf 'PASS  %-8s %-12s format\n' "$kind" "$name"
    else
        printf 'FAIL  %-8s %-12s format\n%s\n' "$kind" "$name" "$unformatted"
        failures=$((failures + 1))
    fi
}

check_repository() {
    local kind=$1
    local name=$2
    local repo=$3
    local security_sensitive=${4:-false}

    if [[ ! -f "$repo/go.mod" ]]; then
        printf 'MISS  %-8s %-12s expected %s/go.mod\n' "$kind" "$name" "$repo"
        failures=$((failures + 1))
        return
    fi
    checked=$((checked + 1))
    if [[ $security_sensitive == true ]] && ! find "$repo" -type f -name '*_test.go' -not -path '*/vendor/*' -print -quit | grep -q .; then
        printf 'FAIL  %-8s %-12s security-tests\n' "$kind" "$name"
        failures=$((failures + 1))
    elif [[ $security_sensitive == true ]]; then
        printf 'PASS  %-8s %-12s security-tests\n' "$kind" "$name"
    fi
    run_format_check "$kind" "$name" "$repo"
    run_go_check "$kind" "$name" "$repo" vet
    run_go_check "$kind" "$name" "$repo" build
    run_go_check "$kind" "$name" "$repo" test
}

resolve_module_repo() {
    local name=$1
    local repository=$2
    local version=$3
    if [[ $remote != true ]]; then
        printf '%s/%s\n' "$workspace_dir" "$name"
        return
    fi

    local repo="$cache_root/checkouts/$name"
    rm -rf "$repo"
    mkdir -p "$(dirname "$repo")"
    if ! git -c advice.detachedHead=false clone --quiet --depth 1 --branch "$version" "https://github.com/$repository.git" "$repo"; then
        printf 'FAIL  module   %-12s clone %s@%s\n' "$name" "$repository" "$version" >&2
        return 1
    fi
    printf '%s\n' "$repo"
}

printf 'Praetor workspace health\n'
printf 'Mode: GOWORK=off; source=%s\n\n' "$([[ $remote == true ]] && printf 'released tags' || printf 'local siblings')"

if [[ $scope != modules ]]; then
    services=("${default_services[@]}")
    if ((${#selected[@]} > 0)); then
        services=()
        for name in "${selected[@]}"; do
            if [[ $scope == services || -z ${module_row_by_name[$name]:-} ]]; then
                services+=("$name")
            fi
        done
    fi
    for service in "${services[@]}"; do
        check_repository service "$service" "$workspace_dir/$service"
    done
fi

if [[ $scope != services ]]; then
    modules=()
    if ((${#selected[@]} > 0)); then
        for name in "${selected[@]}"; do
            if [[ $scope == modules || -n ${module_row_by_name[$name]:-} ]]; then
                modules+=("$name")
            fi
        done
    else
        for row in "${module_rows[@]}"; do
            IFS=$'\t' read -r name _ <<<"$row"
            modules+=("$name")
        done
    fi

    for name in "${modules[@]}"; do
        row=${module_row_by_name[$name]:-}
        if [[ -z $row ]]; then
            printf 'MISS  module   %-12s not declared in platform-compatibility.yaml\n' "$name"
            failures=$((failures + 1))
            continue
        fi
        IFS=$'\t' read -r _ module repository version owner security_sensitive <<<"$row"
        if ! repo=$(resolve_module_repo "$name" "$repository" "$version"); then
            failures=$((failures + 1))
            continue
        fi
        printf 'META  module   %-12s %s %s owner=%s security=%s\n' "$name" "$module" "$version" "$owner" "$security_sensitive"
        check_repository module "$name" "$repo" "$security_sensitive"
    done
fi

printf '\n'
if ((failures > 0)); then
    printf 'Workspace health failed with %d failed or missing check(s).\n' "$failures"
    exit 1
fi

printf 'Workspace health passed for all %d checked repositories.\n' "$checked"
