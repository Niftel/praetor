#!/usr/bin/env bash
# Validate that a manifest is promotable and, optionally, that all referenced
# release artifacts exist. No release state is changed by this command.

set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cache_root=${PRAETOR_RELEASE_CACHE:-${TMPDIR:-/tmp}/praetor-release-preflight}
remote=false
development=false

for arg in "$@"; do
    case $arg in
        --remote) remote=true ;;
        --development) development=true ;;
        *) printf 'usage: %s [--development] [--remote]\n' "$0" >&2; exit 2 ;;
    esac
done

cd "$root_dir"
export GOWORK=off
export GOCACHE="$cache_root/go-build"

compat_args=()
release_kind=development
if [[ $development != true ]]; then
    compat_args=(-release)
    release_kind=stable
fi

printf 'Checking %s-release invariants...\n' "$release_kind"
summary=$(go run ./cmd/compatcheck "${compat_args[@]}" -output summary)
printf '%s\n' "$summary"
if [[ $development == true && $summary != *"(development):"* ]]; then
    printf 'development preflight requires releaseStatus: development\n' >&2
    exit 1
fi

if [[ $remote != true ]]; then
    printf 'Local release preflight passed. Use --remote to verify published artifacts.\n'
    exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
    printf 'docker is required for remote image verification\n' >&2
    exit 1
fi

printf '\nChecking published container images...\n'
while IFS= read -r image; do
    printf '  %s\n' "$image"
    docker manifest inspect "$image" >/dev/null
done < <(go run ./cmd/compatcheck "${compat_args[@]}" -output images)

printf '\nChecking component repository tags...\n'
while IFS=@ read -r repository version; do
    printf '  %s@%s\n' "$repository" "$version"
    if ! git ls-remote --exit-code "https://github.com/$repository.git" "refs/tags/$version" >/dev/null; then
        printf 'missing component tag: %s@%s\n' "$repository" "$version" >&2
        exit 1
    fi
done < <(go run ./cmd/compatcheck "${compat_args[@]}" -output repositories)

printf '\nChecking published Go component and contract modules...\n'
while IFS= read -r module; do
    printf '  %s\n' "$module"
    GOMODCACHE="$cache_root/go-mod" go mod download "$module"
done < <(go run ./cmd/compatcheck "${compat_args[@]}" -output modules)

printf '\nChecking released shared modules independently...\n'
PRAETOR_HEALTH_CACHE="$cache_root/shared-health" \
    ./scripts/check-workspace-health.sh --modules --remote

printf '\nRemote release preflight passed.\n'
