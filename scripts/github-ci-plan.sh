#!/usr/bin/env bash
set -euo pipefail

# Resolve the changed path set for a GitHub event, then delegate every scoping
# decision to plan-ci.sh. Keeping event handling here prevents workflow YAML
# files from growing their own subtly different diff logic.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

event="${EVENT_NAME:-workflow_dispatch}"
case "$event" in
  pull_request)
    : "${BASE_SHA:?BASE_SHA is required for pull_request}"
    : "${HEAD_SHA:?HEAD_SHA is required for pull_request}"
    git diff --name-only "$BASE_SHA" "$HEAD_SHA" | ./scripts/plan-ci.sh
    ;;
  push)
    : "${HEAD_SHA:?HEAD_SHA is required for push}"
    if [[ "${REF_TYPE:-}" == tag || -z "${BASE_SHA:-}" || "${BASE_SHA:-}" =~ ^0+$ ]]; then
      ./scripts/plan-ci.sh --all
    else
      git diff --name-only "$BASE_SHA" "$HEAD_SHA" | ./scripts/plan-ci.sh
    fi
    ;;
  schedule|workflow_dispatch)
    ./scripts/plan-ci.sh --all
    ;;
  *)
    echo "error: unsupported GitHub event '$event'" >&2
    exit 2
    ;;
esac
