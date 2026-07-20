#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMMAND="${1:-}"
CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}"
NAMESPACE="${PRAETOR_STAGING_NAMESPACE:-praetor-staging}"
RELEASE="${PRAETOR_STAGING_RELEASE:-praetor-staging}"
API_PORT="${PRAETOR_DIAGNOSTICS_PREFLIGHT_PORT:-18093}"
API="http://127.0.0.1:$API_PORT/api/v1"
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-$HOME/.local/share/praetor/staging}"
EVIDENCE_ROOT="${PRAETOR_DIAGNOSTICS_EVIDENCE_ROOT:-$DATA_ROOT/diagnostics/evidence}"
INPUT_ROOT="${PRAETOR_DIAGNOSTICS_INPUT_ROOT:-$DATA_ROOT/pilot/evidence}"
RECOVERY_EVIDENCE="${PRAETOR_DIAGNOSTICS_RECOVERY_EVIDENCE:-$DATA_ROOT/acceptance/evidence/execution-recovery.json}"
UI_EVIDENCE="${PRAETOR_DIAGNOSTICS_UI_EVIDENCE:-$EVIDENCE_ROOT/ui-responsive.json}"
REPORT="${PRAETOR_DIAGNOSTICS_REPORT:-$EVIDENCE_ROOT/execution-diagnostics-acceptance.json}"
LOCK="$ROOT/deployments/staging/release-lock.yaml"

usage() { echo "usage: $0 <plan|preflight|verify>" >&2; exit 2; }
die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for tool in jq shasum; do need "$tool"; done
[[ "$COMMAND" =~ ^(plan|preflight|verify)$ ]] || usage
umask 077

plan() {
  cat <<EOF
Execution diagnostics staging acceptance
  journeys: successful, failed-task, unreachable-host, rejected-approval,
            runner-bootstrap failure, control-plane interruption, relaunch lineage
  security: auditor read-only and cross-team access fail closed; secret-canary scan
  budgets:  diagnostic API <= 750 ms, render <= 1500 ms, bounded event pages <= 100
  release:  every component is pinned to an immutable sha256 digest
  UI:       desktop and 390x844 mobile evidence must both pass
  report:   $REPORT
EOF
}

preflight() {
  local log pid token run_id status body
  for tool in curl kubectl; do need "$tool"; done
  kubectl --context "$CONTEXT" -n "$NAMESPACE" wait --for=condition=available "deployment/$RELEASE-api" --timeout=30s >/dev/null || die "staging API is unavailable"
  log="$(mktemp "${TMPDIR:-/tmp}/praetor-diagnostics-preflight.XXXXXX")"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" port-forward "svc/$RELEASE-api" "$API_PORT:8080" >"$log" 2>&1 & pid=$!
  cleanup_preflight() { kill "$pid" 2>/dev/null || true; rm -f "$log"; }
  trap cleanup_preflight RETURN
  for _ in $(seq 1 30); do curl -fsS "$API/ping" >/dev/null 2>&1 && break; sleep 1; done
  kill -0 "$pid" 2>/dev/null || { sed -n '1,40p' "$log" >&2; die "staging API tunnel stopped"; }
  token="$(curl -fsS -H 'Content-Type: application/json' -d "$(jq -nc --arg username "${PRAETOR_DIAGNOSTICS_USERNAME:-demo-operator}" --arg password "${PRAETOR_DIAGNOSTICS_PASSWORD:-praetor123}" '{username:$username,password:$password}')" "$API/auth/login" | jq -er .token)"
  run_id="${PRAETOR_DIAGNOSTICS_PROBE_RUN_ID:-$(curl -fsS -H "Authorization: Bearer $token" "$API/jobs/" | jq -er 'map(select(.current_run_id != null))[0].current_run_id')}"
  body="$(mktemp "${TMPDIR:-/tmp}/praetor-diagnostics-response.XXXXXX")"
  status="$(curl -sS -o "$body" -w '%{http_code}' -H "Authorization: Bearer $token" "$API/jobs/runs/$run_id/diagnostics?limit=1")"
  if [[ "$status" == 404 ]]; then rm -f "$body"; die "deployed API does not expose execution diagnostics; promote a release containing /jobs/runs/{runID}/diagnostics before running staging journeys"; fi
  [[ "$status" == 200 ]] || { response="$(sed -n '1,8p' "$body")"; rm -f "$body"; die "diagnostics preflight returned HTTP $status: $response"; }
  jq -e '.summary and (.events | type == "array")' "$body" >/dev/null || { rm -f "$body"; die "diagnostics preflight returned an invalid contract"; }
  rm -f "$body"
  echo "healthy: staging diagnostics route and redacted response contract are available"
  cleanup_preflight
  trap - RETURN
}

sha256() { shasum -a 256 "$1" | awk '{print "sha256:"$1}'; }
require_json() { [[ -s "$1" ]] || die "required evidence is missing: $1"; jq -e . "$1" >/dev/null || die "invalid JSON evidence: $1"; }
scan_evidence() {
  local file="$1"
  if grep -Eiq '(BEGIN [A-Z ]+ PRIVATE KEY|authorization:[[:space:]]*bearer|"(password|token|private_key|secret)"[[:space:]]*:|PRAETOR_SECRET_CANARY)' "$file"; then
    die "secret canary or credential-shaped data appeared in evidence: $file"
  fi
}

verify() {
  local success="$INPUT_ROOT/managed-host-journey.json" faults="$INPUT_ROOT/managed-host-faults.json"
  local metrics="$EVIDENCE_ROOT/diagnostic-budgets.json" file component_count
  install -d -m 0700 "$EVIDENCE_ROOT"
  for file in "$success" "$faults" "$RECOVERY_EVIDENCE" "$metrics" "$UI_EVIDENCE"; do require_json "$file"; scan_evidence "$file"; done

  jq -e '.result == "pass" and (.checks | index("first-run-changed")) and (.checks | index("second-run-idempotent"))' "$success" >/dev/null || die "successful-run evidence is incomplete"
  jq -e '.result == "pass" and (.checks | index("actionable-unreachable-diagnostics")) and (.checks | index("control-plane-restart-recovered"))' "$faults" >/dev/null || die "fault evidence is incomplete"
  jq -e '.result == "pass" and .recoverable.run_id and .unrecoverable.run_id and .relaunch.run_id and (.unrecoverable.run_id != .relaunch.run_id)' "$RECOVERY_EVIDENCE" >/dev/null || die "interruption and relaunch evidence is incomplete"
  jq -e '.result == "pass" and .journeys.failed_task == "pass" and .journeys.rejected_approval == "pass" and .journeys.runner_bootstrap_failure == "pass" and .stream_resume.gaps == 0 and .stream_resume.duplicates == 0 and .rbac.auditor_mutations_denied == true and .rbac.cross_team_denied == true and .large_run.event_count >= 100 and .large_run.page_size_max <= 100 and .budgets.api_p95_ms <= 750 and .budgets.render_p95_ms <= 1500' "$metrics" >/dev/null || die "diagnostic journey, RBAC, stream, or performance budgets failed"
  jq -e '.result == "pass" and .desktop == "pass" and .mobile_390x844 == "pass"' "$UI_EVIDENCE" >/dev/null || die "desktop and mobile UI acceptance is incomplete"

  component_count="$(awk '/^  [a-z-]+:$/ {count++} END {print count+0}' "$LOCK")"
  [[ "$component_count" == 8 ]] || die "release lock must contain exactly eight components"
  awk '/digest:/ {print $2}' "$LOCK" | jq -Rsc 'split("\n")[:-1] | length == 8 and all(.[]; test("^sha256:[0-9a-f]{64}$"))' | grep -qx true || die "release lock contains a mutable or invalid digest"

  jq -n --arg recorded_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg source_revision "$(git -C "$ROOT" rev-parse HEAD)" \
    --arg release_lock "$(sha256 "$LOCK")" --arg success "$(sha256 "$success")" --arg faults "$(sha256 "$faults")" \
    --arg recovery "$(sha256 "$RECOVERY_EVIDENCE")" --arg metrics "$(sha256 "$metrics")" --arg ui "$(sha256 "$UI_EVIDENCE")" \
    --argjson event_count "$(jq '.large_run.event_count' "$metrics")" \
    '{schema_version:1,profile:"execution-diagnostics-staging",result:"pass",recorded_at:$recorded_at,source_revision:$source_revision,release_lock_sha256:$release_lock,counts:{components:8,diagnostic_events:$event_count},evidence_sha256:{successful:$success,faults:$faults,recovery:$recovery,metrics:$metrics,ui:$ui},checks:["required-journeys","stream-resume-no-gaps-or-duplicates","auditor-and-cross-team-fail-closed","large-run-budgets","secret-canary-clean","desktop-and-mobile","immutable-component-digests"]}' >"$REPORT"
  chmod 0600 "$REPORT"
  scan_evidence "$REPORT"
  echo "execution diagnostics staging acceptance passed: $REPORT"
}

case "$COMMAND" in plan) plan ;; preflight) preflight ;; verify) verify ;; esac
