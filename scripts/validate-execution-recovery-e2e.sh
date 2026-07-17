#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAMESPACE="${PRAETOR_VALIDATION_NAMESPACE:-praetor-secrets}"
RELEASE="${PRAETOR_HELM_RELEASE:-praetor}"
API_PORT="${PRAETOR_RECOVERY_API_PORT:-18082}"
API="http://127.0.0.1:$API_PORT/api/v1"
PASSWORD="${PRAETOR_VALIDATION_LDAP_PASSWORD:-praetor123}"
PROJECT_URL="${PRAETOR_RECOVERY_PROJECT_URL:-https://github.com/Niftel/praetor.git}"
PROJECT_REF="${PRAETOR_RECOVERY_PROJECT_REF:-}"
EXECUTOR_ROOT="${PRAETOR_EXECUTOR_ROOT:-$ROOT/../executor}"
TIMEOUT="${PRAETOR_RECOVERY_TIMEOUT_SECONDS:-240}"
STAMP="$(date -u +%Y%m%d%H%M%S)-$$"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/praetor-recovery-e2e.XXXXXX")"
PORT_FORWARD_PID=""

die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for command in curl go jq kubectl; do need "$command"; done

cleanup() {
  [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

dbq() {
  kubectl exec -n "$NAMESPACE" "$DB_POD" -- psql -v ON_ERROR_STOP=1 -U postgres -d praetor -Atc "$1"
}
login() {
  curl -fsS -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg username "$1" --arg password "$2" '{username:$username,password:$password}')" \
    "$API/auth/login"
}
get() { curl -fsS -H "Authorization: Bearer $1" "$API/$2"; }
post() {
  curl -fsS -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
    -d "${3:-{}}" "$API/$2"
}
post_status() {
  local token="$1" path="$2" body="${3:-{}}"
  RESPONSE_FILE="$WORK/response.json"
  RESPONSE_STATUS="$(curl -sS -o "$RESPONSE_FILE" -w '%{http_code}' \
    -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
    -d "$body" "$API/$path")"
  RESPONSE="$(cat "$RESPONSE_FILE")"
}
wait_db() {
  local query="$1" expected="$2" description="$3" deadline=$((SECONDS + TIMEOUT)) value=""
  while (( SECONDS < deadline )); do
    value="$(dbq "$query" 2>/dev/null || true)"
    [[ "$value" == "$expected" ]] && return
    sleep 2
  done
  die "timed out waiting for $description (last value: ${value:-<empty>})"
}
wait_match() {
  local query="$1" pattern="$2" description="$3" deadline=$((SECONDS + TIMEOUT)) value=""
  while (( SECONDS < deadline )); do
    value="$(dbq "$query" 2>/dev/null || true)"
    [[ "$value" =~ $pattern ]] && { WAIT_VALUE="$value"; return; }
    sleep 2
  done
  die "timed out waiting for $description (last value: ${value:-<empty>})"
}
wait_rollout() {
  kubectl rollout status "$1" -n "$NAMESPACE" --timeout="${TIMEOUT}s" >/dev/null
}
executor_pod() {
  kubectl get pods -n "$NAMESPACE" \
    -l "app.kubernetes.io/component=executor,app.kubernetes.io/instance=$RELEASE" \
    -o jsonpath='{.items[0].metadata.name}'
}
notification_count() {
  local workflow_job_id="$1" event="$2"
  kubectl logs -n "$NAMESPACE" deployment/praetor-validation-notification-sink --since=30m 2>/dev/null |
    grep -F "\"job_id\":$workflow_job_id" | grep -F "\"event\":\"$event\"" | wc -l | tr -d ' '
}
wait_notification_count() {
  local workflow_job_id="$1" event="$2" expected="$3" deadline=$((SECONDS + 60)) count=0
  while (( SECONDS < deadline )); do
    count="$(notification_count "$workflow_job_id" "$event")"
    [[ "$count" == "$expected" ]] && return
    (( count > expected )) && die "notification $event for workflow $workflow_job_id was delivered $count times"
    sleep 1
  done
  die "notification $event for workflow $workflow_job_id was delivered $count times, expected $expected"
}

echo "==> Verifying the deterministic validation stack"
kubectl get --raw=/readyz >/dev/null || die "Kubernetes API is unavailable"
DB_POD="$(kubectl get pods -n "$NAMESPACE" \
  -l "app.kubernetes.io/component=postgresql,app.kubernetes.io/instance=$RELEASE" \
  -o jsonpath='{.items[0].metadata.name}')"
SINK_POD="$(kubectl get pods -n "$NAMESPACE" -l app=praetor-validation-notification-sink -o jsonpath='{.items[0].metadata.name}')"
[[ -n "$DB_POD" && -n "$SINK_POD" ]] || die "create the product-validation fixture first"

echo "==> Staging a test-only execution pack from the checked-out host runner"
EXECUTOR_POD="$(executor_pod)"
ARCH="$(kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')"
[[ "$ARCH" == amd64 || "$ARCH" == arm64 ]] || die "unsupported executor architecture '$ARCH'"
CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -o "$WORK/praetor-host-runner" ./cmd/host-runner
kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- sh -c \
  'mkdir -p /opt/praetor/packs/ansible-runtime/bin /opt/praetor/packs/ansible-runtime/plugins/callback'
kubectl cp "$WORK/praetor-host-runner" "$NAMESPACE/$EXECUTOR_POD:/opt/praetor/packs/ansible-runtime/bin/praetor-host-runner"
[[ -f "$EXECUTOR_ROOT/deploy/plugins/callback/praetor_checkpoint.py" ]] || die "executor checkout is missing the checkpoint callback"
kubectl cp "$EXECUTOR_ROOT/deploy/plugins/callback/praetor_checkpoint.py" \
  "$NAMESPACE/$EXECUTOR_POD:/opt/praetor/packs/ansible-runtime/plugins/callback/praetor_checkpoint.py"
kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- sh -c \
  'chmod 0755 /opt/praetor/packs/ansible-runtime/bin/praetor-host-runner; ln -sf /usr/local/bin/ansible-playbook /opt/praetor/packs/ansible-runtime/bin/ansible-playbook; ln -sf /usr/local/bin/python3 /opt/praetor/packs/ansible-runtime/bin/python3; rm -f /var/lib/praetor/recovery-side-effects.log /var/lib/praetor/recovery-completions.log'
kubectl set env -n "$NAMESPACE" "statefulset/$RELEASE-executor" \
  PRAETOR_CALLBACK_PLUGINS=/opt/praetor/packs/ansible-runtime/plugins/callback >/dev/null
wait_rollout "statefulset/$RELEASE-executor"
EXECUTOR_POD="$(executor_pod)"

echo "==> Tightening only the validation recovery windows"
kubectl set env -n "$NAMESPACE" "deployment/$RELEASE-scheduler" \
  RECONCILE_HEARTBEAT_GRACE=10s RECONCILE_START_GRACE=15s RECONCILE_LOCAL_GRACE=15s >/dev/null
wait_rollout "deployment/$RELEASE-scheduler"

kubectl port-forward -n "$NAMESPACE" "svc/$RELEASE-api" "$API_PORT:8080" >"$WORK/port-forward.log" 2>&1 &
PORT_FORWARD_PID=$!
for _ in $(seq 1 30); do
  curl -fsS "$API/ping" >/dev/null 2>&1 && break
  kill -0 "$PORT_FORWARD_PID" 2>/dev/null || { cat "$WORK/port-forward.log" >&2; die "API tunnel stopped"; }
  sleep 1
done
curl -fsS "$API/ping" >/dev/null || die "API is unavailable"

echo "==> Authenticating the initiating, approving, outsider, auditor, and administrative actors"
ADMIN_TOKEN="$(jq -er .token <<<"$(login admin admin)")"
OPERATOR_LOGIN="$(login demo-operator "$PASSWORD")"; OPERATOR_TOKEN="$(jq -er .token <<<"$OPERATOR_LOGIN")"; OPERATOR_ID="$(jq -er .user.id <<<"$OPERATOR_LOGIN")"
APPROVER_TOKEN="$(jq -er .token <<<"$(login mwebb "$PASSWORD")")"
OUTSIDER_TOKEN="$(jq -er .token <<<"$(login fwalsh "$PASSWORD")")"
AUDITOR_TOKEN="$(jq -er .token <<<"$(login demo-auditor "$PASSWORD")")"
ORG_ID="$(get "$ADMIN_TOKEN" organizations/ | jq -er '(if type == "object" then .items else . end)[] | select(.name == "Engineering") | .id')"
TEAM_ID="$(get "$ADMIN_TOKEN" teams | jq -er '(if type == "object" then .items else . end)[] | select(.name == "backend-team") | .id')"
NOTIFICATION_ID="$(get "$ADMIN_TOKEN" 'notification-templates?organization_id='"$ORG_ID" | jq -er '(if type == "object" and has("items") then .items else . end)[] | select(.name == "Praetor Validation Notifications") | .id')"

echo "==> Creating the approval-gated recovery workflow"
PROJECT_ID="$(post "$ADMIN_TOKEN" projects "$(jq -nc --argjson org "$ORG_ID" --arg name "Recovery E2E $STAMP" --arg url "$PROJECT_URL" --arg ref "$PROJECT_REF" '{organization_id:$org,name:$name,scm_type:"git",scm_url:$url,scm_branch:$ref}')" | jq -er .id)"
CREDENTIAL_ID="$(post "$ADMIN_TOKEN" credentials "$(jq -nc --argjson org "$ORG_ID" --arg name "Recovery E2E $STAMP" '{organization_id:$org,credential_type_id:1,name:$name,inputs:{username:"recovery-validation",password:"synthetic-recovery-only"}}')" | jq -er .id)"
JOB_TEMPLATE_ID="$(post "$ADMIN_TOKEN" job-templates "$(jq -nc --argjson org "$ORG_ID" --argjson project "$PROJECT_ID" --argjson credential "$CREDENTIAL_ID" --arg name "Recovery E2E $STAMP" '{organization_id:$org,project_id:$project,credential_id:$credential,name:$name,playbook:"playbooks/validate-execution-recovery.yml",job_type:"run",forks:1}')" | jq -er .id)"
WORKFLOW_ID="$(post "$ADMIN_TOKEN" workflow-templates "$(jq -nc --argjson org "$ORG_ID" --argjson jt "$JOB_TEMPLATE_ID" --arg name "Recovery E2E $STAMP" '{organization_id:$org,name:$name,allow_simultaneous:false,nodes:[{node_key:"approval",node_type:"approval",name:"Recovery approval"},{node_key:"execute",node_type:"job",job_template_id:$jt,name:"Recovery execution"}],edges:[{parent_key:"approval",child_key:"execute",edge_type:"success"}]}')" | jq -er .id)"

grant_team_role() {
  local role_name="$1" role_id
  role_id="$(get "$ADMIN_TOKEN" 'role-definitions?content_type=workflow_template' | jq -er --arg name "$role_name" '.[] | select(.name == $name) | .id' | head -n1)"
  post "$ADMIN_TOKEN" access "$(jq -nc --argjson object "$WORKFLOW_ID" --argjson role "$role_id" --argjson team "$TEAM_ID" '{content_type:"workflow_template",object_id:$object,role_definition_id:$role,team_id:$team}')" >/dev/null
}
grant_team_role "Workflow Template Execute"
grant_team_role "Workflow Template Approve"
for event in approval approved success error; do
  post_status "$ADMIN_TOKEN" "workflow-templates/$WORKFLOW_ID/notifications" \
    "$(jq -nc --argjson id "$NOTIFICATION_ID" --arg event "$event" '{notification_template_id:$id,event:$event}')"
  [[ "$RESPONSE_STATUS" == 204 ]] || die "attach $event notification returned $RESPONSE_STATUS: $RESPONSE"
done

launch_and_approve() {
  local label="$1"
  post_status "$OPERATOR_TOKEN" "workflow-templates/$WORKFLOW_ID/launch" \
    "$(jq -nc --argjson team "$TEAM_ID" --arg label "$label" '{approval_team_id:$team,extra_vars:{validation_run_label:$label,validation_pause_seconds:30}}')"
  [[ "$RESPONSE_STATUS" == 201 ]] || die "workflow launch returned $RESPONSE_STATUS: $RESPONSE"
  CURRENT_WORKFLOW_JOB_ID="$(jq -er .workflow_job_id <<<"$RESPONSE")"
  local approval_id="" approvals
  for _ in $(seq 1 60); do
    approvals="$(get "$APPROVER_TOKEN" workflow-approvals)"
    approval_id="$(jq -r --argjson job "$CURRENT_WORKFLOW_JOB_ID" '.[] | select(.workflow_job_id == $job) | .id' <<<"$approvals" | head -n1)"
    [[ -n "$approval_id" ]] && break
    sleep 1
  done
  [[ -n "$approval_id" ]] || die "backend-team did not receive workflow $CURRENT_WORKFLOW_JOB_ID approval"
  jq -e --argjson job "$CURRENT_WORKFLOW_JOB_ID" --argjson team "$TEAM_ID" '.[] | select(.workflow_job_id == $job and .approval_team_id == $team and .requested_by == "demo-operator")' <<<"$approvals" >/dev/null || die "approval recipient or requester attribution is wrong"
  [[ "$(get "$OUTSIDER_TOKEN" workflow-approvals | jq --argjson job "$CURRENT_WORKFLOW_JOB_ID" '[.[] | select(.workflow_job_id == $job)] | length')" == 0 ]] || die "outsider received the approval"
  post_status "$APPROVER_TOKEN" "workflow-job-nodes/$approval_id/approve"
  [[ "$RESPONSE_STATUS" == 204 ]] || die "approval returned $RESPONSE_STATUS: $RESPONSE"
  CURRENT_APPROVAL_ID="$approval_id"
  wait_match "select coalesce(wjn.unified_job_id::text,'')||'|'||coalesce(uj.current_run_id::text,'') from workflow_job_nodes wjn left join unified_jobs uj on uj.id=wjn.unified_job_id where wjn.workflow_job_id=$CURRENT_WORKFLOW_JOB_ID and wjn.node_key='execute'" '^[0-9]+\|[0-9a-f-]{36}$' "workflow execution run"
  CURRENT_JOB_ID="${WAIT_VALUE%%|*}"; CURRENT_RUN_ID="${WAIT_VALUE#*|}"
}

assert_workflow_actor() {
  local workflow_job_id="$1"
  [[ "$(dbq "select launched_by_user_id from workflow_jobs where id=$workflow_job_id")" == "$OPERATOR_ID" ]] || die "workflow $workflow_job_id lost initiating actor attribution"
}
assert_terminal_once() {
  local run_id="$1" expected="$2" events
  events="$(dbq "select count(*) from job_events where execution_run_id='$run_id' and event_type in ('JOB_COMPLETED','JOB_FAILED','JOB_CANCELED')")"
  [[ "$events" == 1 ]] || die "run $run_id has $events terminal events"
  [[ "$(dbq "select state from execution_runs where id='$run_id'")" == "$expected" ]] || die "run $run_id did not end as $expected"
}

echo "==> Recoverable fault: interrupt network and supporting services, then kill the executor"
RECOVERY_LABEL="recoverable-$STAMP"
launch_and_approve "$RECOVERY_LABEL"
RECOVERABLE_WORKFLOW_JOB_ID="$CURRENT_WORKFLOW_JOB_ID"; RECOVERABLE_APPROVAL_ID="$CURRENT_APPROVAL_ID"; RECOVERABLE_JOB_ID="$CURRENT_JOB_ID"; RECOVERABLE_RUN_ID="$CURRENT_RUN_ID"
EXECUTOR_POD="$(executor_pod)"
for _ in $(seq 1 60); do
  kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- sh -c \
    "grep -Fx '$RECOVERY_LABEL' /var/lib/praetor/recovery-side-effects.log >/dev/null 2>&1 && grep -F 'Hold the run open' /var/lib/praetor/jobs/$RECOVERABLE_RUN_ID/checkpoint.json >/dev/null 2>&1" && break
  sleep 1
done
kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- sh -c \
  "grep -F 'Hold the run open' /var/lib/praetor/jobs/$RECOVERABLE_RUN_ID/checkpoint.json" >/dev/null || die "checkpoint was not durable before interruption"
kubectl scale -n "$NAMESPACE" deployment/praetor-ingestion --replicas=0 >/dev/null
kubectl rollout restart -n "$NAMESPACE" deployment/praetor-scheduler deployment/praetor-consumer >/dev/null
kubectl delete pod -n "$NAMESPACE" "$EXECUTOR_POD" --wait=false >/dev/null
sleep 5
kubectl scale -n "$NAMESPACE" deployment/praetor-ingestion --replicas=1 >/dev/null
wait_rollout deployment/praetor-ingestion
wait_rollout deployment/praetor-scheduler
wait_rollout deployment/praetor-consumer
wait_rollout "statefulset/$RELEASE-executor"
wait_db "select status from unified_jobs where id=$RECOVERABLE_JOB_ID" successful "recovered job completion"
wait_db "select status from workflow_jobs where id=$RECOVERABLE_WORKFLOW_JOB_ID" successful "recovered workflow completion"
EXECUTOR_POD="$(executor_pod)"
[[ "$(kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- sh -c "grep -Fxc '$RECOVERY_LABEL' /var/lib/praetor/recovery-side-effects.log")" == 1 ]] || die "recoverable run repeated its protected side effect"
[[ "$(kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- sh -c "grep -Fxc '$RECOVERY_LABEL' /var/lib/praetor/recovery-completions.log")" == 1 ]] || die "recoverable run did not complete exactly once"
assert_workflow_actor "$RECOVERABLE_WORKFLOW_JOB_ID"
assert_terminal_once "$RECOVERABLE_RUN_ID" successful
[[ "$(dbq "select count(*) from job_events where execution_run_id='$RECOVERABLE_RUN_ID' and event_type='RESUMED_FROM_CHECKPOINT'")" == 1 ]] || die "recoverable run has no single checkpoint-resume event"

echo "==> Unrecoverable fault: remove the authoritative WAL and prove clear failure"
LOSS_LABEL="unrecoverable-$STAMP"
launch_and_approve "$LOSS_LABEL"
LOST_WORKFLOW_JOB_ID="$CURRENT_WORKFLOW_JOB_ID"; LOST_APPROVAL_ID="$CURRENT_APPROVAL_ID"; LOST_JOB_ID="$CURRENT_JOB_ID"; LOST_RUN_ID="$CURRENT_RUN_ID"
EXECUTOR_POD="$(executor_pod)"
for _ in $(seq 1 60); do
  kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- test -f "/var/lib/praetor/jobs/$LOST_RUN_ID/checkpoint.json" && break
  sleep 1
done
kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- rm -rf "/var/lib/praetor/jobs/$LOST_RUN_ID"
kubectl delete pod -n "$NAMESPACE" "$EXECUTOR_POD" --wait=false >/dev/null
wait_rollout "statefulset/$RELEASE-executor"
# This is controlled fault injection: age the missing-WAL run beyond the same
# heartbeat/grace boundaries the scheduler uses, avoiding a multi-minute test.
dbq "update execution_runs set state='reconciling', last_heartbeat_at=now()-interval '1 hour', reconcile_after=now()-interval '1 second' where id='$LOST_RUN_ID'" >/dev/null
wait_db "select status from unified_jobs where id=$LOST_JOB_ID" error "unrecoverable job failure"
wait_db "select status from workflow_jobs where id=$LOST_WORKFLOW_JOB_ID" failed "unrecoverable workflow failure"
assert_workflow_actor "$LOST_WORKFLOW_JOB_ID"
[[ "$(dbq "select state from execution_runs where id='$LOST_RUN_ID'")" == lost ]] || die "unrecoverable run was not marked lost"

echo "==> Safe relaunch after loss"
RELAUNCH_LABEL="relaunch-$STAMP"
launch_and_approve "$RELAUNCH_LABEL"
RELAUNCH_WORKFLOW_JOB_ID="$CURRENT_WORKFLOW_JOB_ID"; RELAUNCH_APPROVAL_ID="$CURRENT_APPROVAL_ID"; RELAUNCH_JOB_ID="$CURRENT_JOB_ID"; RELAUNCH_RUN_ID="$CURRENT_RUN_ID"
[[ "$RELAUNCH_WORKFLOW_JOB_ID" != "$LOST_WORKFLOW_JOB_ID" && "$RELAUNCH_RUN_ID" != "$LOST_RUN_ID" ]] || die "relaunch reused the failed run"
wait_db "select status from unified_jobs where id=$RELAUNCH_JOB_ID" successful "relaunched job completion"
wait_db "select status from workflow_jobs where id=$RELAUNCH_WORKFLOW_JOB_ID" successful "relaunched workflow completion"
assert_workflow_actor "$RELAUNCH_WORKFLOW_JOB_ID"
assert_terminal_once "$RELAUNCH_RUN_ID" successful

echo "==> Verifying notification deduplication and audit evidence"
for workflow_job_id in "$RECOVERABLE_WORKFLOW_JOB_ID" "$LOST_WORKFLOW_JOB_ID" "$RELAUNCH_WORKFLOW_JOB_ID"; do
  wait_notification_count "$workflow_job_id" approval 1
  wait_notification_count "$workflow_job_id" approved 1
done
wait_notification_count "$RECOVERABLE_WORKFLOW_JOB_ID" success 1
wait_notification_count "$LOST_WORKFLOW_JOB_ID" error 1
wait_notification_count "$RELAUNCH_WORKFLOW_JOB_ID" success 1

AUDIT="$(get "$AUDITOR_TOKEN" 'activity-stream?limit=500')"
for pair in \
  "$RECOVERABLE_WORKFLOW_JOB_ID:$RECOVERABLE_APPROVAL_ID" \
  "$LOST_WORKFLOW_JOB_ID:$LOST_APPROVAL_ID" \
  "$RELAUNCH_WORKFLOW_JOB_ID:$RELAUNCH_APPROVAL_ID"; do
  workflow_job_id="${pair%%:*}"; approval_id="${pair#*:}"
  jq -e --arg path "/api/v1/workflow-templates/$WORKFLOW_ID/launch" '.[] | select(.username == "demo-operator" and .path == $path and .status_code == 201)' <<<"$AUDIT" >/dev/null || die "operator launch audit evidence is missing"
  jq -e --arg path "/api/v1/workflow-job-nodes/$approval_id/approve" '.[] | select(.username == "mwebb" and .path == $path and .status_code == 204)' <<<"$AUDIT" >/dev/null || die "approval audit evidence is missing for workflow $workflow_job_id"
done

RESOLUTION_COUNTS="$(kubectl exec -n "$NAMESPACE" deployment/praetor-validation-secrets-postgres -- sh -c \
  'PGPASSWORD="$POSTGRES_PASSWORD" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "select run_id||'\''|'\''||resolution_count from run_bindings where run_id in ('\'''"$RECOVERABLE_RUN_ID"'\'','\'''"$LOST_RUN_ID"'\'','\'''"$RELAUNCH_RUN_ID"'\'') order by run_id"')"
while IFS='|' read -r run_id count; do
  [[ -z "$run_id" || "$count" == 1 ]] || die "run $run_id resolved its credential $count times"
done <<<"$RESOLUTION_COUNTS"

jq -n \
  --argjson recoverable_workflow_job_id "$RECOVERABLE_WORKFLOW_JOB_ID" --arg recoverable_run_id "$RECOVERABLE_RUN_ID" \
  --argjson lost_workflow_job_id "$LOST_WORKFLOW_JOB_ID" --arg lost_run_id "$LOST_RUN_ID" \
  --argjson relaunch_workflow_job_id "$RELAUNCH_WORKFLOW_JOB_ID" --arg relaunch_run_id "$RELAUNCH_RUN_ID" \
  '{result:"pass",recoverable:{workflow_job_id:$recoverable_workflow_job_id,run_id:$recoverable_run_id},unrecoverable:{workflow_job_id:$lost_workflow_job_id,run_id:$lost_run_id},relaunch:{workflow_job_id:$relaunch_workflow_job_id,run_id:$relaunch_run_id}}'
