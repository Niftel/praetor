#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}"
NAMESPACE="${PRAETOR_STAGING_NAMESPACE:-praetor-staging}"
RELEASE="${PRAETOR_STAGING_RELEASE:-praetor-staging}"
API_PORT="${PRAETOR_DIAGNOSTICS_BUDGET_PORT:-18094}"
API="http://127.0.0.1:$API_PORT/api/v1"
PASSWORD="${PRAETOR_DIAGNOSTICS_PASSWORD:-praetor123}"
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-$HOME/.local/share/praetor/staging}"
EVIDENCE_ROOT="${PRAETOR_DIAGNOSTICS_EVIDENCE_ROOT:-$DATA_ROOT/diagnostics/evidence}"
SUCCESS_EVIDENCE="${PRAETOR_DIAGNOSTICS_SUCCESS_EVIDENCE:-$DATA_ROOT/pilot/evidence/managed-host-journey.json}"
OUTPUT="${PRAETOR_DIAGNOSTICS_BUDGET_EVIDENCE:-$EVIDENCE_ROOT/diagnostic-budgets.json}"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/praetor-diagnostic-budgets.XXXXXX")"
PORT_FORWARD_PID=""
DB_POD=""
JOB_ID=""
RUN_ID=""

die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for tool in curl jq kubectl sort; do need "$tool"; done
[[ -x "$ROOT/web/node_modules/.bin/vite-node" ]] || die "web dependencies are missing; run npm ci in web"
umask 077

db() { kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$DB_POD" -- psql -v ON_ERROR_STOP=1 -U postgres -d praetor -qAt -F '|' -c "$1"; }
cleanup() {
  [[ -z "$JOB_ID" || -z "$DB_POD" ]] || db "DELETE FROM unified_jobs WHERE id=$JOB_ID" >/dev/null 2>&1 || true
  [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

login() {
  local username="$1"
  curl -fsS -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg username "$username" --arg password "$PASSWORD" '{username:$username,password:$password}')" \
    "$API/auth/login" | jq -er .token
}
status_as() {
  local token="$1" method="$2" path="$3"
  curl -sS -o /dev/null -w '%{http_code}' -X "$method" -H "Authorization: Bearer $token" "$API/$path"
}
p95_ms() {
  sort -n "$1" | awk '{v[NR]=$1} END {if (!NR) exit 1; i=int((NR*95+99)/100); printf "%.3f", v[i]*1000}'
}

install -d -m 0700 "$EVIDENCE_ROOT"
jq -e '.result == "pass" and .runs.second.run_id' "$SUCCESS_EVIDENCE" >/dev/null || die "successful managed-host evidence is missing"
SOURCE_RUN="$(jq -er .runs.second.run_id "$SUCCESS_EVIDENCE")"
[[ "$SOURCE_RUN" =~ ^[0-9a-f-]{36}$ ]] || die "source run ID is invalid"

DB_POD="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" get pods -l "app.kubernetes.io/component=postgresql,app.kubernetes.io/instance=$RELEASE" -o jsonpath='{.items[0].metadata.name}')"
[[ -n "$DB_POD" ]] || die "staging PostgreSQL pod is missing"
SOURCE_JOB="$(db "SELECT unified_job_id FROM execution_runs WHERE id='$SOURCE_RUN'")"
[[ "$SOURCE_JOB" =~ ^[0-9]+$ ]] || die "source governed job is missing"
db "DELETE FROM unified_jobs WHERE job_args->>'synthetic_fixture'='execution-diagnostics'" >/dev/null
JOB_ID="$(db "INSERT INTO unified_jobs (unified_job_template_id,name,status,started_at,finished_at,job_args) SELECT unified_job_template_id,'Synthetic diagnostics budget fixture','successful',now(),now(),jsonb_build_object('synthetic_fixture','execution-diagnostics') FROM unified_jobs WHERE id=$SOURCE_JOB RETURNING id")"
RUN_ID="$(db "INSERT INTO execution_runs (unified_job_id,state,started_at,last_event_seq,persisted_event_seq) VALUES ($JOB_ID,'running',now(),125,125) RETURNING id")"
[[ "$JOB_ID" =~ ^[0-9]+$ && "$RUN_ID" =~ ^[0-9a-f-]{36}$ ]] || die "failed to create bounded synthetic run"

db "INSERT INTO job_events (unified_job_id,execution_run_id,seq,event_type,host_id,task_name,play_name,event_data,created_at) SELECT $JOB_ID,'$RUN_ID',i,CASE WHEN i=1 THEN 'JOB_STARTED' WHEN i=17 THEN 'HOST_FAILED' WHEN i=18 THEN 'JOB_FAILED' WHEN i=19 THEN 'JOB_CANCELED' WHEN i%23=0 THEN 'HOST_CHANGED' ELSE 'HOST_OK' END,CASE WHEN i IN (1,18,19) THEN NULL ELSE (SELECT host_id FROM job_events WHERE execution_run_id='$SOURCE_RUN' AND host_id IS NOT NULL LIMIT 1) END,CASE WHEN i=17 THEN 'Failed task fixture' WHEN i=18 THEN 'Runner bootstrap fixture' WHEN i=19 THEN 'Rejected approval fixture' WHEN i=1 THEN NULL ELSE 'Synthetic task '||i END,'Synthetic diagnostics scale',jsonb_strip_nulls(jsonb_build_object('outcome',CASE WHEN i IN (17,18,19) THEN 'failed' WHEN i%23=0 THEN 'changed' ELSE 'ok' END,'changed',i%23=0,'duration_ms',i,'failure_code',CASE WHEN i=17 THEN 'task_failed' WHEN i=18 THEN 'execution_failed' ELSE NULL END)),now() + i * interval '1 millisecond' FROM generate_series(1,125) AS i" >/dev/null

TUNNEL_LOG="$WORK/port-forward.log"
kubectl --context "$CONTEXT" -n "$NAMESPACE" port-forward "svc/$RELEASE-api" "$API_PORT:8080" >"$TUNNEL_LOG" 2>&1 & PORT_FORWARD_PID=$!
for _ in $(seq 1 30); do curl -fsS "$API/ping" >/dev/null 2>&1 && break; sleep 1; done
kill -0 "$PORT_FORWARD_PID" 2>/dev/null || { sed -n '1,40p' "$TUNNEL_LOG" >&2; die "staging API tunnel stopped"; }

OPERATOR_TOKEN="$(login demo-operator)"
AUDITOR_TOKEN="$(login demo-auditor)"
OUTSIDER_TOKEN="$(login fwalsh)"

# Connect while the run is active, then force a client-side disconnect after the
# first bounded page has been acknowledged.
curl -sS --max-time 2 -H "Authorization: Bearer $OPERATOR_TOKEN" -H 'Accept: text/event-stream' \
  "$API/jobs/runs/$RUN_ID/diagnostics/stream?cursor=0" >"$WORK/stream-first.txt" || true
awk '/^id: [0-9]+$/ {print $2}' "$WORK/stream-first.txt" >"$WORK/ids-first"
FIRST_CURSOR="$(tail -n1 "$WORK/ids-first")"
[[ "$FIRST_CURSOR" =~ ^[0-9]+$ && "$FIRST_CURSOR" -eq 125 ]] || die "induced disconnect did not acknowledge the first 125 events"

db "INSERT INTO job_events (unified_job_id,execution_run_id,seq,event_type,host_id,task_name,play_name,event_data,created_at) SELECT $JOB_ID,'$RUN_ID',i,CASE WHEN i=251 THEN 'JOB_COMPLETED' WHEN i%19=0 THEN 'HOST_CHANGED' ELSE 'HOST_OK' END,(SELECT host_id FROM job_events WHERE execution_run_id='$SOURCE_RUN' AND host_id IS NOT NULL LIMIT 1),CASE WHEN i=251 THEN NULL ELSE 'Synthetic task '||i END,'Synthetic diagnostics scale',jsonb_build_object('outcome',CASE WHEN i%19=0 THEN 'changed' ELSE 'ok' END,'changed',i%19=0,'duration_ms',i),now() + i * interval '1 millisecond' FROM generate_series(126,251) AS i; UPDATE execution_runs SET state='successful',finished_at=now(),last_event_seq=251,persisted_event_seq=251 WHERE id='$RUN_ID'" >/dev/null

curl -fsS -H "Authorization: Bearer $OPERATOR_TOKEN" -H 'Accept: text/event-stream' -H "Last-Event-ID: $FIRST_CURSOR" \
  "$API/jobs/runs/$RUN_ID/diagnostics/stream" >"$WORK/stream-second.txt"
awk '/^id: [0-9]+$/ {print $2}' "$WORK/stream-second.txt" >"$WORK/ids-second"
cat "$WORK/ids-first" "$WORK/ids-second" >"$WORK/ids-all"
TOTAL_IDS="$(wc -l <"$WORK/ids-all" | tr -d ' ')"
UNIQUE_IDS="$(sort -n -u "$WORK/ids-all" | wc -l | tr -d ' ')"
GAPS="$(sort -n -u "$WORK/ids-all" | awk 'NR==1 {previous=$1; if ($1 != 1) gaps += $1-1; next} {if ($1 != previous+1) gaps += $1-previous-1; previous=$1} END {if (previous != 251) gaps += 251-previous; print gaps+0}')"
DUPLICATES=$((TOTAL_IDS - UNIQUE_IDS))
[[ "$TOTAL_IDS" == 251 && "$GAPS" == 0 && "$DUPLICATES" == 0 ]] || die "diagnostic reconnect lost or duplicated events"

for _ in $(seq 1 20); do
  curl -fsS -o "$WORK/page.json" -w '%{time_total}\n' -H "Authorization: Bearer $OPERATOR_TOKEN" \
    "$API/jobs/runs/$RUN_ID/diagnostics?limit=100" >>"$WORK/api-times"
done
API_P95_MS="$(p95_ms "$WORK/api-times")"

cursor=0
: >"$WORK/pages.jsonl"
while :; do
  curl -fsS -H "Authorization: Bearer $OPERATOR_TOKEN" "$API/jobs/runs/$RUN_ID/diagnostics?limit=100&cursor=$cursor" >"$WORK/page.json"
  jq -c . "$WORK/page.json" >>"$WORK/pages.jsonl"
  next="$(jq -r '.next_cursor // empty' "$WORK/page.json")"
  [[ -n "$next" ]] || break
  cursor="$next"
done
jq -s '[.[].events[]]' "$WORK/pages.jsonl" >"$WORK/events.json"
EVENT_COUNT="$(jq length "$WORK/events.json")"
PAGE_SIZE_MAX="$(jq -s '[.[].events | length] | max' "$WORK/pages.jsonl")"
[[ "$EVENT_COUNT" == 251 && "$PAGE_SIZE_MAX" -le 100 ]] || die "bounded diagnostic pagination failed"
jq -e 'any(.[]; .event_type == "HOST_FAILED" and .task_name == "Failed task fixture" and .outcome == "failed" and .failure_code == "task_failed")' "$WORK/events.json" >/dev/null || die "failed-task diagnostic fixture was not projected"
jq -e 'any(.[]; .event_type == "JOB_FAILED" and .task_name == "Runner bootstrap fixture" and .outcome == "failed" and .failure_code == "execution_failed")' "$WORK/events.json" >/dev/null || die "runner-bootstrap diagnostic fixture was not projected"
jq -e 'any(.[]; .event_type == "JOB_CANCELED" and .task_name == "Rejected approval fixture" and .outcome == "failed")' "$WORK/events.json" >/dev/null || die "rejected-approval diagnostic fixture was not projected"

"$ROOT/web/node_modules/.bin/vite-node" "$ROOT/web/scripts/measure-diagnostics.ts" "$WORK/events.json" >"$WORK/render.json"
RENDER_P95_MS="$(jq -er .render_p95_ms "$WORK/render.json")"

AUDITOR_READ="$(status_as "$AUDITOR_TOKEN" GET "jobs/runs/$RUN_ID/diagnostics?limit=1")"
AUDITOR_MUTATION="$(status_as "$AUDITOR_TOKEN" POST "jobs/$JOB_ID/cancel")"
CROSS_TEAM_READ="$(status_as "$OUTSIDER_TOKEN" GET "jobs/runs/$RUN_ID/diagnostics?limit=1")"
[[ "$AUDITOR_READ" == 200 && "$AUDITOR_MUTATION" == 403 && "$CROSS_TEAM_READ" == 403 ]] || die "diagnostics RBAC acceptance failed (auditor=$AUDITOR_READ/$AUDITOR_MUTATION cross-team=$CROSS_TEAM_READ)"

jq -n --arg recorded_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg source_revision "$(git -C "$ROOT" rev-parse HEAD)" \
  --argjson event_count "$EVENT_COUNT" --argjson page_size_max "$PAGE_SIZE_MAX" --argjson api_p95_ms "$API_P95_MS" --argjson render_p95_ms "$RENDER_P95_MS" \
  --argjson gaps "$GAPS" --argjson duplicates "$DUPLICATES" --arg source_evidence_sha256 "$(shasum -a 256 "$SUCCESS_EVIDENCE" | awk '{print "sha256:"$1}')" \
  '{schema_version:1,journey:"execution-diagnostics-large-run",result:"pass",recorded_at:$recorded_at,source_revision:$source_revision,journeys:{failed_task:"pass",rejected_approval:"pass",runner_bootstrap_failure:"pass"},large_run:{event_count:$event_count,page_size_max:$page_size_max},stream_resume:{gaps:$gaps,duplicates:$duplicates,disconnect_cursor:125,terminal_cursor:251},rbac:{auditor_read:true,auditor_mutations_denied:true,cross_team_denied:true},budgets:{api_p95_ms:$api_p95_ms,render_p95_ms:$render_p95_ms,api_limit_ms:750,render_limit_ms:1500},source_evidence_sha256:$source_evidence_sha256,checks:["bounded-synthetic-fixture","api-page-budget","ui-projection-budget","induced-disconnect-resume","auditor-read-only","cross-team-fail-closed","temporary-fixture-cleanup"]}' >"$OUTPUT"
chmod 0600 "$OUTPUT"
if grep -Eiq '(BEGIN [A-Z ]+ PRIVATE KEY|authorization:[[:space:]]*bearer|"(password|token|private_key|secret)"[[:space:]]*:|PRAETOR_SECRET_CANARY|172\.[0-9]+\.)' "$OUTPUT"; then
  die "sensitive material appeared in diagnostic budget evidence"
fi
jq -e '.result == "pass" and .large_run.event_count == 251 and .stream_resume.gaps == 0 and .stream_resume.duplicates == 0 and .budgets.api_p95_ms <= .budgets.api_limit_ms and .budgets.render_p95_ms <= .budgets.render_limit_ms' "$OUTPUT" >/dev/null || die "diagnostic budgets failed"
echo "execution diagnostics budget evidence passed: $OUTPUT"
