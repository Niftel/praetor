#!/usr/bin/env bash
# Coordinate an idempotent, tag-driven Praetor platform release. The manifest
# remains the source of truth; this script never chooses or rewrites versions.

set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
dry_run=false
timeout_seconds=${PRAETOR_RELEASE_TIMEOUT_SECONDS:-1800}

case ${1:-} in
    --dry-run) dry_run=true; shift ;;
esac

requested_version=${1:-}
if [[ -z $requested_version || $# -ne 1 ]]; then
    printf 'usage: %s [--dry-run] <platform-version>\n' "$0" >&2
    exit 2
fi

cd "$root_dir"
export GOWORK=off
export GOCACHE=${PRAETOR_RELEASE_CACHE:-${TMPDIR:-/tmp}/praetor-release-promote}/go-build

compat_args=()
if [[ $dry_run == false ]]; then
    compat_args=(-release)
fi
summary=$(go run ./cmd/compatcheck "${compat_args[@]}" -output summary)
manifest_version=$(sed -E 's/^Praetor ([^ ]+).*/\1/' <<<"$summary")
if [[ $requested_version != "$manifest_version" ]]; then
    printf 'requested platform version %s does not match manifest %s\n' "$requested_version" "$manifest_version" >&2
    exit 1
fi

releases=()
while IFS= read -r release; do
    releases+=("$release")
done < <(go run ./cmd/compatcheck -output repositories)
printf 'Platform release plan for v%s:\n' "$manifest_version"
printf '  %s\n' "${releases[@]}"

if [[ $dry_run == true ]]; then
    printf 'Dry run complete; no tags, workflows, or releases were changed.\n'
    exit 0
fi

if [[ -z ${PRAETOR_RELEASE_TOKEN:-} ]]; then
    printf 'PRAETOR_RELEASE_TOKEN is required\n' >&2
    exit 1
fi
export GH_TOKEN=$PRAETOR_RELEASE_TOKEN

for release in "${releases[@]}"; do
    repository=${release%@*}
    version=${release#*@}
    if gh api "repos/$repository/git/ref/tags/$version" >/dev/null 2>&1; then
        printf 'EXISTS  %s@%s\n' "$repository" "$version"
        continue
    fi
    default_branch=$(gh api "repos/$repository" --jq .default_branch)
    sha=$(gh api "repos/$repository/commits/$default_branch" --jq .sha)
    gh api --method POST "repos/$repository/git/refs" -f ref="refs/tags/$version" -f sha="$sha" >/dev/null
    printf 'CREATED %s@%s (%s)\n' "$repository" "$version" "$sha"
done

printf 'Waiting for all declared tags, images, and modules to become available...\n'
deadline=$((SECONDS + timeout_seconds))
until ./scripts/release-preflight.sh --remote; do
    if ((SECONDS >= deadline)); then
        printf 'release artifacts did not converge within %s seconds\n' "$timeout_seconds" >&2
        exit 1
    fi
    printf 'Artifacts are still publishing; retrying in 20 seconds.\n'
    sleep 20
done

platform_tag="v$manifest_version"
if gh release view "$platform_tag" --repo Niftel/praetor >/dev/null 2>&1; then
    printf 'EXISTS  platform release %s\n' "$platform_tag"
else
    gh release create "$platform_tag" --repo Niftel/praetor --verify-tag --title "Praetor $manifest_version" --generate-notes
    printf 'CREATED platform release %s\n' "$platform_tag"
fi
