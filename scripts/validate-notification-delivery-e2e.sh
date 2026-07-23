#!/usr/bin/env bash
set -Eeuo pipefail

# Proves durable notification enqueue, retry, restart recovery, deduplication,
# RBAC, and redacted history against the disposable product-validation cluster.

NAMESPACE="${PRAETOR_VALIDATION_NAMESPACE:-praetor-secrets}"
RELEASE="${PRAETOR_HELM_RELEASE:-praetor}"
API_PORT="${PRAETOR_NOTIFICATION_E2E_API_PORT:-18086}"
API="http://127.0.0.1:$API_PORT/api/v1"
PASSWORD="${PRAETOR_VALIDATION_LDAP_PASSWORD:-praetor123}"
EVIDENCE_FILE="${PRAETOR_NOTIFICATION_EVIDENCE_FILE:-}"
PREFIX="Notification Delivery E2E $(date +%s)"
SECRET_CANARY="notification-history-secret-canary"
PORT_FORWARD_PID=""
WORK="$(mktemp -d "${TMPDIR:-/tmp}/praetor-notification-e2e.XXXXXX")"
PHASE="bootstrap"
POLICY_IDS=()
TARGET_IDS=()
WORKFLOW_JOB_IDS=()
INVENTORY_ID=""
JOB_TEMPLATE_ID=""
OTHER_ORG_ID=""

die() { echo "error: $*" >&2; record_failure; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for command in curl jq kubectl; do need "$command"; done

record_failure() {
  [[ -z "$EVIDENCE_FILE" ]] || {
    umask 077
    mkdir -p "$(dirname "$EVIDENCE_FILE")"
    jq -n --arg phase "$PHASE" \
      '{schema_version:1,journey:"notification-delivery",result:"fail",phase:$phase}' >"$EVIDENCE_FILE"
  }
}

request() {
  local token="$1" method="$2" path="$3" body="${4:-}" output="$WORK/response.json"
  local args=(-sS -o "$output" -w '%{http_code}' -X "$method" -H "Authorization: Bearer $token")
  [[ -z "$body" ]] || args+=(-H 'Content-Type: application/json' -d "$body")
  STATUS="$(curl "${args[@]}" "$API/$path")"
  RESPONSE="$(cat "$output")"
}

cleanup() {
  local approvals=""
  set +e
  kubectl scale -n "$NAMESPACE" deployment/praetor-validation-notification-sink --replicas=1 >/dev/null 2>&1
  kubectl scale -n "$NAMESPACE" "deployment/$RELEASE-consumer" --replicas=1 >/dev/null 2>&1
  if [[ -n "${ADMIN_TOKEN:-}" && -n "$PORT_FORWARD_PID" ]] && kill -0 "$PORT_FORWARD_PID" 2>/dev/null; then
    if [[ -n "${APPROVER_TOKEN:-}" ]]; then
      request "$APPROVER_TOKEN" GET workflow-approvals >/dev/null 2>&1
      if [[ "$STATUS" == 200 ]]; then
        approvals="$RESPONSE"
        for workflow_job_id in "${WORKFLOW_JOB_IDS[@]}"; do
          while read -r approval_id; do
            [[ -z "$approval_id" ]] || request "$APPROVER_TOKEN" POST "workflow-job-nodes/$approval_id/approve" >/dev/null 2>&1
          done < <(jq -r --argjson job "$workflow_job_id" '.[] | select(.workflow_job_id == $job) | .id' <<<"$approvals")
        done
      fi
    fi
    for policy_id in "${POLICY_IDS[@]}"; do request "$ADMIN_TOKEN" DELETE "notification-policies/$policy_id" >/dev/null 2>&1; done
    [[ -z "$INVENTORY_ID" ]] || request "$ADMIN_TOKEN" DELETE "inventories/$INVENTORY_ID" >/dev/null 2>&1
    [[ -z "$JOB_TEMPLATE_ID" ]] || request "$ADMIN_TOKEN" DELETE "job-templates/$JOB_TEMPLATE_ID" >/dev/null 2>&1
    for target_id in "${TARGET_IDS[@]}"; do request "$ADMIN_TOKEN" DELETE "notification-templates/$target_id" >/dev/null 2>&1; done
    [[ -z "$OTHER_ORG_ID" ]] || request "$ADMIN_TOKEN" DELETE "organizations/$OTHER_ORG_ID/" >/dev/null 2>&1
  fi
  [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null
  rm -rf "$WORK"
}
trap record_failure ERR
trap cleanup EXIT

kubectl port-forward -n "$NAMESPACE" "svc/$RELEASE-api" "$API_PORT:8080" >"$WORK/port-forward.log" 2>&1 &
PORT_FORWARD_PID=$!
for _ in $(seq 1 30); do
  curl -fsS "$API/ping" >/dev/null 2>&1 && break
  kill -0 "$PORT_FORWARD_PID" 2>/dev/null || { cat "$WORK/port-forward.log" >&2; die "API tunnel stopped"; }
  sleep 1
done
curl -fsS "$API/ping" >/dev/null || die "API did not become reachable"

login() {
  local username="$1" password="$2"
  curl -fsS -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg username "$username" --arg password "$password" '{username:$username,password:$password}')" \
    "$API/auth/login" | jq -er .token
}
get() {
  local token="$1" path="$2"
  request "$token" GET "$path"
  [[ "$STATUS" == 200 ]] || die "GET /api/v1/$path returned $STATUS: $RESPONSE"
  printf '%s' "$RESPONSE"
}
items() { jq -c 'if type == "object" and has("items") then .items else . end'; }
find_id() {
  get "$ADMIN_TOKEN" "$1" | items | jq -r --arg name "$2" '.[] | select(.name == $name) | .id' | head -n1
}
wait_rollout() {
  kubectl rollout status -n "$NAMESPACE" "$1" --timeout=180s >/dev/null ||
    die "$1 did not become ready"
}
history() { get "$1" "notification-deliveries?organization_id=$2&limit=100"; }
history_match() {
  local token="$1" org_id="$2" target_name="$3" subject_id="$4" event="$5"
  history "$token" "$org_id" | jq -c \
    --arg target "$target_name" --argjson subject "$subject_id" --arg event "$event" \
    '.results[] | select(.target_name == $target and .subject_id == $subject and .event == $event)' | head -n1
}
wait_history_state() {
  local target_name="$1" subject_id="$2" event="$3" state="$4" row=""
  for _ in $(seq 1 60); do
    if ! row="$(history_match "$ADMIN_TOKEN" "$ORG_ID" "$target_name" "$subject_id" "$event")"; then
      die "notification history API failed while waiting for $target_name"
    fi
    if [[ -n "$row" && "$(jq -r .status <<<"$row")" == "$state" ]]; then
      printf '%s' "$row"
      return
    fi
    sleep 1
  done
  die "$target_name $event delivery for subject $subject_id did not reach $state"
}
wait_job() {
  local job_id="$1" expected="$2" state=""
  for _ in $(seq 1 180); do
    state="$(get "$ADMIN_TOKEN" jobs | jq -r --argjson id "$job_id" '.[] | select(.id == $id) | .status' | head -n1)"
    [[ "$state" =~ ^(successful|failed|error|canceled)$ ]] && break
    sleep 1
  done
  [[ "$state" == "$expected" ]] || die "job $job_id finished '$state', expected '$expected'"
}
create_target() {
  local name="$1" url="$2"
  request "$ADMIN_TOKEN" POST notification-templates \
    "$(jq -nc --argjson org "$ORG_ID" --arg name "$name" --arg url "$url" \
      '{organization_id:$org,name:$name,notification_type:"webhook",config:{url:$url}}')"
  [[ "$STATUS" == 201 ]] || die "create notification target returned $STATUS: $RESPONSE"
  jq -er .id <<<"$RESPONSE"
}
create_policy() {
  local target_id="$1" resource_type="$2" resource_id="$3" event="$4" team_id="${5:-}"
  local body
  body="$(jq -nc --argjson target "$target_id" --arg type "$resource_type" \
    --argjson resource "$resource_id" --arg event "$event" --arg team "$team_id" \
    '{notification_template_id:$target,resource_type:$type,resource_id:$resource,event:$event}
     + if $team == "" then {} else {team_id:($team|tonumber)} end')"
  request "$ADMIN_TOKEN" POST notification-policies "$body"
  [[ "$STATUS" == 201 ]] || die "create $resource_type $event policy returned $STATUS: $RESPONSE"
  jq -er .id <<<"$RESPONSE"
}
delete_policy() {
  local policy_id="$1"
  request "$ADMIN_TOKEN" DELETE "notification-policies/$policy_id"
  [[ "$STATUS" == 204 ]] || die "delete notification policy returned $STATUS"
}
launch_approval_workflow() {
  request "$OPERATOR_TOKEN" POST "workflow-templates/$WORKFLOW_ID/launch" \
    "$(jq -nc --argjson team "$TEAM_ID" '{approval_team_id:$team}')"
  [[ "$STATUS" == 201 ]] || die "workflow launch returned $STATUS: $RESPONSE"
  printf '%s' "$(jq -er .workflow_job_id <<<"$RESPONSE")"
}
approval_id() {
  local workflow_job_id="$1" id=""
  for _ in $(seq 1 60); do
    id="$(get "$APPROVER_TOKEN" workflow-approvals |
      jq -r --argjson job "$workflow_job_id" '.[] | select(.workflow_job_id == $job) | .id' | head -n1)"
    [[ -n "$id" ]] && { printf '%s' "$id"; return; }
    sleep 1
  done
  die "approval for workflow $workflow_job_id did not appear"
}
approve_workflow() {
  local workflow_job_id="$1" id status=""
  id="$(approval_id "$workflow_job_id")"
  request "$APPROVER_TOKEN" POST "workflow-job-nodes/$id/approve"
  [[ "$STATUS" == 204 ]] || die "approval returned $STATUS: $RESPONSE"
  for _ in $(seq 1 180); do
    status="$(get "$ADMIN_TOKEN" "workflow-jobs/$workflow_job_id" | jq -r .status)"
    [[ "$status" =~ ^(successful|failed|error|canceled)$ ]] && break
    sleep 1
  done
  [[ "$status" == successful ]] || die "workflow $workflow_job_id finished '$status', expected 'successful'"
}

PHASE="identity-and-fixture"
ADMIN_TOKEN="$(login "${PRAETOR_VALIDATION_ADMIN_USERNAME:-admin}" "${PRAETOR_VALIDATION_ADMIN_PASSWORD:-admin}")"
OPERATOR_TOKEN="$(login demo-operator "$PASSWORD")"
APPROVER_TOKEN="$(login mwebb "$PASSWORD")"
OUTSIDER_TOKEN="$(login fwalsh "$PASSWORD")"
ORG_ID="$(find_id organizations/ Engineering)"; [[ -n "$ORG_ID" ]] || die "Engineering organization is missing"
TEAM_ID="$(find_id teams/ backend-team)"; [[ -n "$TEAM_ID" ]] || die "backend-team is missing"
PROJECT_ID="$(find_id projects 'Praetor Validation Project')"; [[ -n "$PROJECT_ID" ]] || die "fixture project is missing"
WORKFLOW_ID="$(find_id workflow-templates 'Praetor Validation LDAP Workflow')"; [[ -n "$WORKFLOW_ID" ]] || die "fixture workflow is missing"

# This journey uses a no-inventory template so execution remains local to the
# executor. Inventory-backed templates intentionally require an operator-owned
# SSH Machine credential for their selected runner host.
request "$ADMIN_TOKEN" POST job-templates/ \
  "$(jq -nc --argjson org "$ORG_ID" --argjson project "$PROJECT_ID" --arg name "$PREFIX Job Template" \
    '{organization_id:$org,project_id:$project,name:$name,playbook:"playbooks/ping.yml",job_type:"run",forks:1}')"
[[ "$STATUS" == 201 ]] || die "create local job template returned $STATUS: $RESPONSE"
JOB_TEMPLATE_ID="$(jq -er .id <<<"$RESPONSE")"
UJT_ID="$(jq -er .unified_job_template_id <<<"$RESPONSE")"

SUCCESS_NAME="$PREFIX Success"
TRANSIENT_NAME="$PREFIX Transient"
PERMANENT_NAME="$PREFIX Permanent"
SUCCESS_TARGET="$(create_target "$SUCCESS_NAME" "http://praetor-validation-notification-sink:8080/echo?token=$SECRET_CANARY")"
TRANSIENT_TARGET="$(create_target "$TRANSIENT_NAME" "http://praetor-validation-notification-sink:8080/echo?token=$SECRET_CANARY")"
PERMANENT_TARGET="$(create_target "$PERMANENT_NAME" "http://praetor-validation-notification-sink:8080/permanent?token=$SECRET_CANARY")"
TARGET_IDS+=("$SUCCESS_TARGET" "$TRANSIENT_TARGET" "$PERMANENT_TARGET")

PHASE="workflow-pending-and-transient-retry"
TRANSIENT_POLICY="$(create_policy "$TRANSIENT_TARGET" workflow_template "$WORKFLOW_ID" approval "$TEAM_ID")"
POLICY_IDS+=("$TRANSIENT_POLICY")
kubectl scale -n "$NAMESPACE" "deployment/$RELEASE-consumer" --replicas=0 >/dev/null
WORKFLOW_JOB_ID="$(launch_approval_workflow)"
WORKFLOW_JOB_IDS+=("$WORKFLOW_JOB_ID")
PENDING="$(wait_history_state "$TRANSIENT_NAME" "$WORKFLOW_JOB_ID" approval pending)"
[[ "$(jq -r .attempt_count <<<"$PENDING")" == 0 ]] || die "pending workflow delivery already has an attempt"

# Restart the producer after enqueue, then start a fresh worker while the target
# is unavailable. The first attempt must become retrying rather than disappear.
kubectl rollout restart -n "$NAMESPACE" "deployment/$RELEASE-scheduler" >/dev/null
wait_rollout "deployment/$RELEASE-scheduler"
kubectl scale -n "$NAMESPACE" deployment/praetor-validation-notification-sink --replicas=0 >/dev/null
kubectl scale -n "$NAMESPACE" "deployment/$RELEASE-consumer" --replicas=1 >/dev/null
wait_rollout "deployment/$RELEASE-consumer"
RETRYING="$(wait_history_state "$TRANSIENT_NAME" "$WORKFLOW_JOB_ID" approval retrying)"
[[ "$(jq -r .attempt_count <<<"$RETRYING")" == 1 ]] || die "transient delivery did not record one failed attempt"
[[ "$(jq -r '.attempts | length' <<<"$RETRYING")" == 1 ]] || die "transient attempt history is incomplete"
[[ "$(jq -r '.attempts[0].outcome' <<<"$RETRYING")" == transient_failure ]] || die "first transient attempt was misclassified"

kubectl scale -n "$NAMESPACE" "deployment/$RELEASE-consumer" --replicas=0 >/dev/null
kubectl scale -n "$NAMESPACE" deployment/praetor-validation-notification-sink --replicas=1 >/dev/null
wait_rollout deployment/praetor-validation-notification-sink
kubectl scale -n "$NAMESPACE" "deployment/$RELEASE-consumer" --replicas=1 >/dev/null
wait_rollout "deployment/$RELEASE-consumer"
DELIVERED_RETRY="$(wait_history_state "$TRANSIENT_NAME" "$WORKFLOW_JOB_ID" approval delivered)"
jq -e '
  .attempt_count == 2 and
  .subject_kind == "workflow approval" and
  (.attempts | length) == 2 and
  .attempts[0].outcome == "transient_failure" and
  .attempts[1].outcome == "delivered"
' <<<"$DELIVERED_RETRY" >/dev/null || die "transient retry sequence is incorrect"
[[ "$(history "$ADMIN_TOKEN" "$ORG_ID" | jq --arg target "$TRANSIENT_NAME" --argjson subject "$WORKFLOW_JOB_ID" \
  '[.results[] | select(.target_name == $target and .subject_id == $subject and .event == "approval")] | length')" == 1 ]] ||
  die "duplicate workflow occurrence produced more than one logical delivery"
approve_workflow "$WORKFLOW_JOB_ID"
delete_policy "$TRANSIENT_POLICY"

PHASE="permanent-failure"
PERMANENT_POLICY="$(create_policy "$PERMANENT_TARGET" workflow_template "$WORKFLOW_ID" approval "$TEAM_ID")"
POLICY_IDS+=("$PERMANENT_POLICY")
PERMANENT_WORKFLOW_JOB_ID="$(launch_approval_workflow)"
WORKFLOW_JOB_IDS+=("$PERMANENT_WORKFLOW_JOB_ID")
FAILED_PERMANENT="$(wait_history_state "$PERMANENT_NAME" "$PERMANENT_WORKFLOW_JOB_ID" approval failed)"
jq -e '
  .attempt_count == 1 and
  (.attempts | length) == 1 and
  .attempts[0].outcome == "permanent_failure" and
  (.failure_code | length) > 0 and
  (.failure_reason | length) > 0
' <<<"$FAILED_PERMANENT" >/dev/null || die "permanent delivery did not stop after one actionable failure"
approve_workflow "$PERMANENT_WORKFLOW_JOB_ID"

PHASE="job-delivery"
JOB_POLICY="$(create_policy "$SUCCESS_TARGET" job_template "$JOB_TEMPLATE_ID" success)"
POLICY_IDS+=("$JOB_POLICY")
request "$ADMIN_TOKEN" POST jobs "$(jq -nc --argjson template "$UJT_ID" --arg name "$PREFIX Job" \
  '{unified_job_template_id:$template,name:$name}')"
[[ "$STATUS" == 201 ]] || die "job launch returned $STATUS: $RESPONSE"
JOB_ID="$(jq -er .id <<<"$RESPONSE")"
wait_job "$JOB_ID" successful
JOB_DELIVERY="$(wait_history_state "$SUCCESS_NAME" "$JOB_ID" success delivered)"
[[ "$(jq -r '.attempt_count' <<<"$JOB_DELIVERY")" == 1 ]] || die "job delivery was not exactly once"
[[ "$(jq -r '.subject_kind' <<<"$JOB_DELIVERY")" == job ]] || die "job delivery identity is incorrect"

PHASE="inventory-delivery"
request "$ADMIN_TOKEN" POST inventories "$(jq -nc --argjson org "$ORG_ID" --arg name "$PREFIX Inventory" \
  '{organization_id:$org,name:$name,kind:"dynamic"}')"
[[ "$STATUS" == 201 ]] || die "create inventory returned $STATUS: $RESPONSE"
INVENTORY_ID="$(jq -er .id <<<"$RESPONSE")"
SOURCE='#!/usr/bin/env python3
print("{\"_meta\":{\"hostvars\":{\"notification-e2e\":{\"fixture\":true}}},\"all\":{\"hosts\":[\"notification-e2e\"]}}")'
request "$ADMIN_TOKEN" POST "inventories/$INVENTORY_ID/sources" \
  "$(jq -nc --arg name "$PREFIX Source" --arg source "$SOURCE" \
    '{name:$name,source_type:"custom",source_kind:"script",source:$source,reconciliation_policy:"disable_missing"}')"
[[ "$STATUS" == 201 ]] || die "create inventory source returned $STATUS: $RESPONSE"
SOURCE_ID="$(jq -er .id <<<"$RESPONSE")"
INVENTORY_POLICY="$(create_policy "$SUCCESS_TARGET" inventory_source "$SOURCE_ID" success)"
POLICY_IDS+=("$INVENTORY_POLICY")
request "$ADMIN_TOKEN" POST "inventories/$INVENTORY_ID/sources/$SOURCE_ID/sync"
[[ "$STATUS" == 201 ]] || die "inventory sync returned $STATUS: $RESPONSE"
INVENTORY_JOB_ID="$(jq -er .job_id <<<"$RESPONSE")"
wait_job "$INVENTORY_JOB_ID" successful
INVENTORY_DELIVERY="$(wait_history_state "$SUCCESS_NAME" "$INVENTORY_JOB_ID" success delivered)"
[[ "$(jq -r '.subject_kind' <<<"$INVENTORY_DELIVERY")" == "inventory sync" ]] ||
  die "inventory delivery identity is incorrect"

PHASE="history-rbac"
OPERATOR_HISTORY="$(history "$OPERATOR_TOKEN" "$ORG_ID")"
jq -e --argjson id "$(jq -r .id <<<"$DELIVERED_RETRY")" '.results | any(.id == $id)' \
  <<<"$OPERATOR_HISTORY" >/dev/null || die "assigned-team operator cannot inspect approval history"
jq -e --argjson job "$JOB_ID" '.results | all(.subject_id != $job)' \
  <<<"$OPERATOR_HISTORY" >/dev/null || die "ordinary operator can inspect organization-scoped job history"
OUTSIDER_HISTORY="$(history "$OUTSIDER_TOKEN" "$ORG_ID")"
jq -e --argjson first "$WORKFLOW_JOB_ID" --argjson second "$PERMANENT_WORKFLOW_JOB_ID" \
  '.results | all(.subject_id != $first and .subject_id != $second)' \
  <<<"$OUTSIDER_HISTORY" >/dev/null || die "wrong-team user can inspect approval history"

request "$ADMIN_TOKEN" POST organizations "$(jq -nc --arg name "$PREFIX Other Org" '{name:$name}')"
[[ "$STATUS" == 201 ]] || die "create cross-organization fixture returned $STATUS: $RESPONSE"
OTHER_ORG_ID="$(jq -er .id <<<"$RESPONSE")"
request "$OPERATOR_TOKEN" GET "notification-deliveries?organization_id=$OTHER_ORG_ID&limit=25"
[[ "$STATUS" == 403 ]] || die "cross-organization history returned $STATUS, expected 403"

PHASE="redaction-and-evidence"
ALL_HISTORY="$(history "$ADMIN_TOKEN" "$ORG_ID")"
for sensitive in "$SECRET_CANARY" '"config"' '"idempotency_key"' '"job_args"' '"credential"'; do
  ! grep -Fq "$sensitive" <<<"$ALL_HISTORY" || die "history exposed sensitive marker $sensitive"
done

EVIDENCE="$(jq -n \
  --argjson workflow_job_id "$WORKFLOW_JOB_ID" \
  --argjson permanent_workflow_job_id "$PERMANENT_WORKFLOW_JOB_ID" \
  --argjson job_id "$JOB_ID" \
  --argjson inventory_job_id "$INVENTORY_JOB_ID" \
  --argjson retry_attempts "$(jq -r .attempt_count <<<"$DELIVERED_RETRY")" \
  --argjson permanent_attempts "$(jq -r .attempt_count <<<"$FAILED_PERMANENT")" \
  '{
    schema_version:1,
    journey:"notification-delivery",
    result:"pass",
    subjects:{
      workflow:$workflow_job_id,
      permanent_workflow:$permanent_workflow_job_id,
      job:$job_id,
      inventory_sync:$inventory_job_id
    },
    attempts:{transient:$retry_attempts,permanent:$permanent_attempts},
    checks:[
      "job-delivery",
      "inventory-sync-delivery",
      "workflow-approval-delivery",
      "logical-delivery-deduplication",
      "bounded-transient-retry",
      "permanent-failure-stops",
      "producer-restart",
      "worker-restart-resume",
      "assigned-team-history",
      "wrong-team-history-denial",
      "cross-organization-history-denial",
      "notification-history-secret-redaction",
      "sanitized-machine-readable-evidence"
    ]
  }')"
if [[ -n "$EVIDENCE_FILE" ]]; then
  umask 077
  mkdir -p "$(dirname "$EVIDENCE_FILE")"
  printf '%s\n' "$EVIDENCE" >"$EVIDENCE_FILE"
fi
printf '%s\n' "$EVIDENCE"

PHASE="complete"
trap - ERR
