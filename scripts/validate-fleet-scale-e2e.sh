#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EVIDENCE_FILE="${PRAETOR_FLEET_EVIDENCE_FILE:-}"
[[ -n "${TEST_DATABASE_URL:-}" ]] || {
  echo "error: TEST_DATABASE_URL must point to an isolated migrated database" >&2
  exit 1
}
for command in go jq npm; do
  command -v "$command" >/dev/null || {
    echo "error: $command is required" >&2
    exit 1
  }
done

work="$(mktemp -d "${TMPDIR:-/tmp}/praetor-fleet-e2e.XXXXXX")"
trap 'rm -rf "$work"' EXIT

(
  cd "$ROOT"
  go test -json -count=1 ./services/api/handlers \
    -run '^(TestBulk|TestDelegatedWorkflowLaunch)'
) >"$work/api.json"

api_skipped="$(jq -s '[.[] | select(.Action == "skip" and ((.Test // "") | test("^(TestBulk|TestDelegatedWorkflowLaunch)")))] | length' "$work/api.json")"
api_passed="$(jq -s '[.[] | select(.Action == "pass" and ((.Test // "") | test("^(TestBulk|TestDelegatedWorkflowLaunch)")))] | length' "$work/api.json")"
[[ "$api_skipped" == 0 ]] || {
  echo "error: $api_skipped fleet API tests were skipped" >&2
  exit 1
}
[[ "$api_passed" -ge 14 ]] || {
  echo "error: only $api_passed fleet API tests passed, expected at least 14" >&2
  exit 1
}

(
  cd "$ROOT/web"
  npm test -- --run \
    services/api.bulk.test.ts \
    components/ui/BulkSelection.test.tsx \
    pages/FleetScaleJourney.test.tsx \
    pages/frameworkAdoption.test.ts \
    --reporter=json \
    --outputFile="$work/ui.json" >/dev/null
)
ui_passed="$(jq -er '.numPassedTests' "$work/ui.json")"
ui_failed="$(jq -er '.numFailedTests' "$work/ui.json")"
ui_pending="$(jq -er '.numPendingTests' "$work/ui.json")"
[[ "$ui_passed" -ge 16 && "$ui_failed" == 0 && "$ui_pending" == 0 ]] || {
  echo "error: fleet UI contracts passed=$ui_passed failed=$ui_failed pending=$ui_pending" >&2
  exit 1
}

evidence="$(jq -n \
  --argjson api_tests "$api_passed" \
  --argjson ui_tests "$ui_passed" \
  '{
    schema_version:1,
    journey:"fleet-scale",
    result:"pass",
    test_counts:{database_api:$api_tests,browser_dom:$ui_tests},
    scope_guards:[
      "organization-boundary",
      "inventory-rbac",
      "host-scope",
      "delegated-client",
      "request-bounds",
      "payload-bounds",
      "idempotent-replay",
      "concurrent-replay",
      "preview-confirm-delete",
      "stale-preview",
      "audit-attribution",
      "partial-result-retry"
    ],
    checks:[
      "database-backed-api",
      "mixed-outcomes",
      "fail-closed-identifiers",
      "service-principal-boundary",
      "browser-selection-execution-results-retry",
      "ui-selection-limit",
      "ui-per-item-results",
      "ui-failed-item-retry"
    ]
  }')"

if [[ -n "$EVIDENCE_FILE" ]]; then
  mkdir -p "$(dirname "$EVIDENCE_FILE")"
  umask 077
  printf '%s\n' "$evidence" >"$EVIDENCE_FILE"
fi
printf '%s\n' "$evidence"
