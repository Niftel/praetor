#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EVIDENCE_FILE="${PRAETOR_DELEGATED_EVIDENCE_FILE:-}"
[[ -n "${TEST_DATABASE_URL:-}" ]] || { echo "error: TEST_DATABASE_URL must point to an isolated migrated database" >&2; exit 1; }
for command in go jq; do command -v "$command" >/dev/null || { echo "error: $command is required" >&2; exit 1; }; done

work="$(mktemp -d "${TMPDIR:-/tmp}/praetor-delegated-e2e.XXXXXX")"
trap 'rm -rf "$work"' EXIT
(
  cd "$ROOT"
  go test -json -count=1 ./services/api/handlers -run '^TestDelegatedWorkflowLaunch'
) >"$work/test.json"

skipped="$(jq -s '[.[] | select(.Action == "skip" and (.Test // "" | startswith("TestDelegatedWorkflowLaunch")))] | length' "$work/test.json")"
passed="$(jq -s '[.[] | select(.Action == "pass" and (.Test // "" | startswith("TestDelegatedWorkflowLaunch")))] | length' "$work/test.json")"
[[ "$skipped" == 0 ]] || { echo "error: $skipped delegated API tests were skipped" >&2; exit 1; }
[[ "$passed" -ge 4 ]] || { echo "error: only $passed delegated API tests passed, expected at least 4" >&2; exit 1; }

EVIDENCE="$(jq -n --argjson passed "$passed" '{schema_version:1,journey:"delegated-api",result:"pass",test_count:$passed,scope_guards:["inventory","host","limit","organization","approval-team","extra-vars","idempotency","principal-boundary"]}')"
if [[ -n "$EVIDENCE_FILE" ]]; then
  umask 077
  printf '%s\n' "$EVIDENCE" >"$EVIDENCE_FILE"
fi
printf '%s\n' "$EVIDENCE"
