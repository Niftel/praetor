#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${PRAETOR_VALIDATION_NAMESPACE:-praetor-secrets}"
RELEASE="${PRAETOR_HELM_RELEASE:-praetor}"
API_PORT="${PRAETOR_VALIDATION_API_PORT:-18081}"
API="http://127.0.0.1:$API_PORT/api/v1"
PASSWORD="${PRAETOR_VALIDATION_LDAP_PASSWORD:-praetor123}"
PORT_FORWARD_PID=""
PORT_FORWARD_LOG=""

die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for command in curl jq kubectl; do need "$command"; done

cleanup() {
  [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null || true
  [[ -z "$PORT_FORWARD_LOG" ]] || rm -f "$PORT_FORWARD_LOG"
}
trap cleanup EXIT

PORT_FORWARD_LOG="$(mktemp "${TMPDIR:-/tmp}/praetor-ldap-journey.XXXXXX")"
kubectl port-forward -n "$NAMESPACE" "svc/$RELEASE-api" "$API_PORT:8080" >"$PORT_FORWARD_LOG" 2>&1 &
PORT_FORWARD_PID=$!
for _ in $(seq 1 30); do
  curl -fsS "$API/ping" >/dev/null 2>&1 && break
  kill -0 "$PORT_FORWARD_PID" 2>/dev/null || { cat "$PORT_FORWARD_LOG" >&2; die "API tunnel stopped"; }
  sleep 1
done
curl -fsS "$API/ping" >/dev/null 2>&1 || die "API did not become reachable"

login() {
  curl -fsS -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg username "$1" --arg password "$PASSWORD" '{username:$username,password:$password}')" \
    "$API/auth/login"
}
get() { curl -fsS -H "Authorization: Bearer $1" "$API/$2"; }
get_status() {
  local token="$1" path="$2" output
  output="$(mktemp "${TMPDIR:-/tmp}/praetor-ldap-response.XXXXXX")"
  REQUEST_STATUS="$(curl -sS -o "$output" -w '%{http_code}' -H "Authorization: Bearer $token" "$API/$path")"
  RESPONSE="$(cat "$output")"; rm -f "$output"
}
post_status() {
  local token="$1" path="$2" body="${3:-}" output
  output="$(mktemp "${TMPDIR:-/tmp}/praetor-ldap-response.XXXXXX")"
  REQUEST_STATUS="$(curl -sS -o "$output" -w '%{http_code}' -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "$body" "$API/$path")"
  RESPONSE="$(cat "$output")"; rm -f "$output"
}
assert_user() {
  local json="$1" username="$2" auditor="$3"
  jq -e --arg username "$username" --argjson auditor "$auditor" \
    '.user.username == $username and .user.is_active == true and .user.is_superuser == false and .user.is_system_auditor == $auditor' \
    <<<"$json" >/dev/null || die "unexpected LDAP identity for $username"
}
assert_mapping() {
  local token="$1" user_id="$2" expected_team="$3"
  get "$token" "users/$user_id/organizations" | jq -e '[.[] | select(.name == "Engineering")] | length == 1' >/dev/null || die "Engineering membership is missing"
  get "$token" "users/$user_id/teams" | jq -e --arg team "$expected_team" '[.[] | select(.name == $team)] | length == 1' >/dev/null || die "$expected_team membership is missing"
}

operator_json="$(login demo-operator)"; assert_user "$operator_json" demo-operator false
approver_json="$(login mwebb)"; assert_user "$approver_json" mwebb false
outsider_json="$(login fwalsh)"; assert_user "$outsider_json" fwalsh false
auditor_json="$(login demo-auditor)"; assert_user "$auditor_json" demo-auditor true

operator_token="$(jq -er .token <<<"$operator_json")"; operator_id="$(jq -er .user.id <<<"$operator_json")"
approver_token="$(jq -er .token <<<"$approver_json")"; approver_id="$(jq -er .user.id <<<"$approver_json")"
outsider_token="$(jq -er .token <<<"$outsider_json")"; outsider_id="$(jq -er .user.id <<<"$outsider_json")"
auditor_token="$(jq -er .token <<<"$auditor_json")"
assert_mapping "$operator_token" "$operator_id" backend-team
assert_mapping "$approver_token" "$approver_id" backend-team
assert_mapping "$outsider_token" "$outsider_id" frontend-team

team_id="$(get "$operator_token" teams | jq -er '(if type == "object" then .items else . end)[] | select(.name == "backend-team") | .id')"
workflow_id="$(get "$operator_token" workflow-templates | jq -er '(if type == "object" then .items else . end)[] | select(.name == "Praetor Validation LDAP Workflow") | .id')"
inventory_id="$(get "$operator_token" inventories | jq -er '(if type == "object" then .items else . end)[] | select(.name == "Praetor Validation Inventory") | .id')" || die "operator cannot use fixture inventory"
get "$operator_token" "inventories/$inventory_id/hosts/" | jq -e '(if type == "object" then .items else . end)[] | select(.name == "Praetor Validation Host" and .enabled == true)' >/dev/null || die "operator cannot access fixture host"
get "$outsider_token" inventories | jq -e --argjson inventory "$inventory_id" '[(if type == "object" then .items else . end)[] | select(.id == $inventory)] | length == 0' >/dev/null || die "another team can list the fixture inventory"
get_status "$outsider_token" "inventories/$inventory_id"; status="$REQUEST_STATUS"
[[ "$status" == 403 ]] || die "another team inventory access returned $status, expected 403"
get_status "$outsider_token" "inventories/$inventory_id/hosts/"; status="$REQUEST_STATUS"
[[ "$status" == 403 ]] || die "another team host access returned $status, expected 403"

post_status "$operator_token" "workflow-templates/$workflow_id/launch" "$(jq -nc --argjson team "$team_id" '{approval_team_id:$team}')"
status="$REQUEST_STATUS"
[[ "$status" == 201 ]] || die "authorized workflow launch returned $status: $RESPONSE"
workflow_job_id="$(jq -er .workflow_job_id <<<"$RESPONSE")"

approval_id=""
for _ in $(seq 1 60); do
  approvals="$(get "$approver_token" workflow-approvals)"
  approval_id="$(jq -r --argjson job "$workflow_job_id" '.[] | select(.workflow_job_id == $job) | .id' <<<"$approvals" | head -n1)"
  [[ -n "$approval_id" ]] && break
  sleep 1
done
[[ -n "$approval_id" ]] || die "assigned team did not receive the approval"
jq -e --argjson job "$workflow_job_id" --argjson team "$team_id" '.[] | select(.workflow_job_id == $job and .approval_team_id == $team and .requested_by == "demo-operator")' <<<"$approvals" >/dev/null || die "approval attribution is incorrect"
[[ "$(get "$operator_token" workflow-approvals | jq 'length')" == 0 ]] || die "requester can see their own approval"
[[ "$(get "$outsider_token" workflow-approvals | jq 'length')" == 0 ]] || die "another team can see the approval"

post_status "$outsider_token" "workflow-job-nodes/$approval_id/approve"; status="$REQUEST_STATUS"
[[ "$status" == 403 ]] || die "another team approval returned $status, expected 403"
post_status "$operator_token" "workflow-job-nodes/$approval_id/approve"; status="$REQUEST_STATUS"
[[ "$status" == 403 ]] || die "requester self-approval returned $status, expected 403"
post_status "$approver_token" "workflow-job-nodes/$approval_id/approve"; status="$REQUEST_STATUS"
[[ "$status" == 204 ]] || die "assigned-team approval returned $status: $RESPONSE"

terminal=""
for _ in $(seq 1 180); do
  run="$(get "$operator_token" "workflow-jobs/$workflow_job_id")"
  terminal="$(jq -r .status <<<"$run")"
  [[ "$terminal" =~ ^(successful|failed|error|canceled)$ ]] && break
  sleep 1
done
[[ "$terminal" == successful ]] || die "workflow finished with status '$terminal'"

audit="$(get "$auditor_token" 'activity-stream?limit=100')"
jq -e --arg path "/api/v1/workflow-templates/$workflow_id/launch" '.[] | select(.username == "demo-operator" and .method == "POST" and .path == $path and .status_code == 201)' <<<"$audit" >/dev/null || die "launch actor is missing from audit evidence"
jq -e --arg path "/api/v1/workflow-job-nodes/$approval_id/approve" '.[] | select(.username == "mwebb" and .method == "POST" and .path == $path and .status_code == 204)' <<<"$audit" >/dev/null || die "approval actor is missing from audit evidence"

jq -n --argjson workflow_job_id "$workflow_job_id" --arg status "$terminal" --arg requester demo-operator --arg approver mwebb --arg approval_team backend-team \
  '{result:"pass",workflow_job_id:$workflow_job_id,status:$status,requester:$requester,approver:$approver,approval_team:$approval_team}'
