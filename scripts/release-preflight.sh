#!/usr/bin/env bash
# Validate that a manifest is promotable and, optionally, that all referenced
# release artifacts exist. No release state is changed by this command.

set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cache_root=${PRAETOR_RELEASE_CACHE:-${TMPDIR:-/tmp}/praetor-release-preflight}
remote=false

case ${1:-} in
    "") ;;
    --remote) remote=true ;;
    *) printf 'usage: %s [--remote]\n' "$0" >&2; exit 2 ;;
esac

cd "$root_dir"
export GOWORK=off
export GOCACHE="$cache_root/go-build"

printf 'Checking stable-release invariants...\n'
go run ./cmd/compatcheck -release

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
done < <(go run ./cmd/compatcheck -release -output images)

printf '\nChecking published Go contract modules...\n'
while IFS= read -r module; do
    printf '  %s\n' "$module"
    GOMODCACHE="$cache_root/go-mod" go mod download "$module"
done < <(go run ./cmd/compatcheck -release -output contracts)

printf '\nRemote release preflight passed.\n'
