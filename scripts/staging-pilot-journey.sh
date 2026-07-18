#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMMAND="${1:-}"
CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}"
NAMESPACE="${PRAETOR_STAGING_NAMESPACE:-praetor-staging}"
RELEASE="${PRAETOR_STAGING_RELEASE:-praetor-staging}"
SECRETS_DB_POD="${PRAETOR_STAGING_SECRETS_DB_POD:-${RELEASE}-secrets-postgres-0}"
API_PORT="${PRAETOR_PILOT_JOURNEY_PORT:-18084}"
API="http://127.0.0.1:$API_PORT/api/v1"
PASSWORD="${PRAETOR_STAGING_ACCEPTANCE_PASSWORD:-praetor123}"
PROJECT_REF="${PRAETOR_PILOT_PROJECT_REF:-main}"
PREFIX="${PRAETOR_PILOT_JOURNEY_PREFIX:-Pilot Managed Host}"
PROJECT_NAME="$PREFIX Project"
JOB_NAME="$PREFIX Job"
WORKFLOW_NAME="$PREFIX Workflow"
INVENTORY_NAME="Pilot Engineering Inventory"
HOST_NAME="pilot-managed-host"
CREDENTIAL_NAME="Pilot SSH Credential"
NOTIFICATION_NAME="$PREFIX Notifications"
PLAYBOOK="playbooks/pilot-managed-host.yml"
PACK_NAME="ansible-runtime"
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-$HOME/.local/share/praetor/staging}"
EVIDENCE_ROOT="$DATA_ROOT/pilot/evidence"
EVIDENCE_FILE="$EVIDENCE_ROOT/managed-host-journey.json"
PACK_FILE="$ROOT/build/runtime/ansible-runtime-linux-arm64.tar.gz"
PACK_REMOTE="/tmp/build/runtime/ansible-runtime-linux-arm64.tar.gz"
PILOT_TARGET="${PRAETOR_PILOT_TARGET:-praetor-pilot-target}"
PORT_FORWARD_PID=""
PORT_FORWARD_LOG=""
TOKEN=""
STATUS=""
RESPONSE=""

usage() { echo "usage: $0 <plan|seed|status|run>" >&2; exit 2; }
die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for tool in curl docker git jq kubectl shasum; do need "$tool"; done
[[ "$COMMAND" =~ ^(plan|seed|status|run)$ ]] || usage
umask 077

cleanup() {
  [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null || true
  [[ -z "$PORT_FORWARD_LOG" ]] || rm -f "$PORT_FORWARD_LOG"
}
trap cleanup EXIT

start_tunnel() {
  [[ -z "$PORT_FORWARD_PID" ]] || return
  PORT_FORWARD_LOG="$(mktemp "${TMPDIR:-/tmp}/praetor-pilot-journey.XXXXXX")"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" port-forward "svc/$RELEASE-api" "$API_PORT:8080" >"$PORT_FORWARD_LOG" 2>&1 &
  PORT_FORWARD_PID=$!
  for _ in $(seq 1 30); do
    curl -fsS "$API/ping" >/dev/null 2>&1 && return
    kill -0 "$PORT_FORWARD_PID" 2>/dev/null || { cat "$PORT_FORWARD_LOG" >&2; die "API tunnel stopped"; }
    sleep 1
  done
  die "staging API did not become reachable"
}

login() {
  local username="$1" body headers status retry_after attempt
  body="$(mktemp "${TMPDIR:-/tmp}/praetor-pilot-login-body.XXXXXX")"
  headers="$(mktemp "${TMPDIR:-/tmp}/praetor-pilot-login-headers.XXXXXX")"
  for attempt in $(seq 1 6); do
    status="$(curl -sS -D "$headers" -o "$body" -w '%{http_code}' -H 'Content-Type: application/json' \
      -d "$(jq -nc --arg username "$username" --arg password "$PASSWORD" '{username:$username,password:$password}')" \
      "$API/auth/login")"
    if [[ "$status" == 200 ]]; then
      cat "$body"; rm -f "$body" "$headers"; return
    fi
    if [[ "$status" != 429 || "$attempt" == 6 ]]; then
      RESPONSE="$(cat "$body")"; rm -f "$body" "$headers"
      die "login for $username returned HTTP $status: $RESPONSE"
    fi
    retry_after="$(awk 'BEGIN {IGNORECASE=1} /^Retry-After:/ {gsub("\\r", "", $2); print $2; exit}' "$headers")"
    [[ "$retry_after" =~ ^[0-9]+$ ]] || retry_after=$((attempt * 2))
    (( retry_after > 15 )) && retry_after=15
    echo "login for $username was rate limited; retrying in ${retry_after}s" >&2
    sleep "$retry_after"
  done
}
get_as() { curl -fsS -H "Authorization: Bearer $1" "$API/$2"; }
get() { get_as "$TOKEN" "$1"; }
post_status() {
  local token="$1" path="$2" body="${3:-{}}" output
  output="$(mktemp "${TMPDIR:-/tmp}/praetor-pilot-response.XXXXXX")"
  STATUS="$(curl -sS -o "$output" -w '%{http_code}' -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "$body" "$API/$path")"
  RESPONSE="$(cat "$output")"; rm -f "$output"
}
post() { post_status "$TOKEN" "$1" "$2"; [[ "$STATUS" =~ ^20[014]$ ]] || die "POST $1 returned HTTP $STATUS: $RESPONSE"; printf '%s' "$RESPONSE"; }
items() { jq 'if type == "object" and has("items") then .items else . end'; }
find_id() { get "$1" | items | jq -r --arg name "$2" '.[] | select(.name == $name) | .id' | head -n1; }
ensure() {
  local id
  id="$(find_id "$2" "$3")"
  [[ -n "$id" ]] || id="$(post "$1" "$4" | jq -er .id)"
  printf '%s' "$id"
}
grant_team_role() {
  local content_type="$1" object_id="$2" role_name="$3" team_id="$4" role_id existing
  role_id="$(get "role-definitions?content_type=$content_type" | jq -r --arg name "$role_name" '.[] | select(.name == $name) | .id' | head -n1)"
  [[ -n "$role_id" ]] || die "role definition is missing: $role_name"
  existing="$(get "access?content_type=$content_type&object_id=$object_id")"
  if ! jq -e --argjson role "$role_id" --argjson team "$team_id" '.[]? | select(.role_definition_id == $role and ((.team_id == $team) or (.teams | any(.id == $team))))' <<<"$existing" >/dev/null; then
    post access "$(jq -nc --arg type "$content_type" --argjson object "$object_id" --argjson role "$role_id" --argjson team "$team_id" '{content_type:$type,object_id:$object,role_definition_id:$role,team_id:$team}')" >/dev/null
  fi
}

executor_pod() {
  kubectl --context "$CONTEXT" -n "$NAMESPACE" get pods -l "app.kubernetes.io/instance=$RELEASE,app.kubernetes.io/component=executor" -o jsonpath='{.items[0].metadata.name}'
}

stage_execution_pack() {
  local pod local_digest remote_digest
  [[ -s "$PACK_FILE" ]] || die "released ARM64 pack is missing: $PACK_FILE"
  pod="$(executor_pod)"; [[ -n "$pod" ]] || die "staging executor pod is missing"
  local_digest="$(shasum -a 256 "$PACK_FILE" | awk '{print $1}')"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$pod" -- mkdir -p /tmp/build/runtime
  remote_digest="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$pod" -- sha256sum "$PACK_REMOTE" 2>/dev/null | awk '{print $1}' || true)"
  if [[ "$remote_digest" != "$local_digest" ]]; then
    kubectl --context "$CONTEXT" -n "$NAMESPACE" cp "$PACK_FILE" "$pod:$PACK_REMOTE"
    remote_digest="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$pod" -- sha256sum "$PACK_REMOTE" | awk '{print $1}')"
  fi
  [[ "$remote_digest" == "$local_digest" ]] || die "staged execution-pack digest does not match the released local artifact"
}

verify_execution_pack() {
  local pod local_digest remote_digest
  [[ -s "$PACK_FILE" ]] || die "released ARM64 pack is missing: $PACK_FILE"
  pod="$(executor_pod)"; [[ -n "$pod" ]] || die "staging executor pod is missing"
  local_digest="$(shasum -a 256 "$PACK_FILE" | awk '{print $1}')"
  remote_digest="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$pod" -- sha256sum "$PACK_REMOTE" 2>/dev/null | awk '{print $1}' || true)"
  [[ "$remote_digest" == "$local_digest" ]] || die "released execution-pack tarball is missing or stale on the staging executor; run seed"
}

lookup() {
  ORG_ID="$(find_id organizations Engineering)"; [[ -n "$ORG_ID" ]] || die "Engineering organization is missing"
  TEAM_ID="$(find_id teams backend-team)"; [[ -n "$TEAM_ID" ]] || die "backend-team is missing"
  INVENTORY_ID="$(find_id inventories "$INVENTORY_NAME")"; [[ -n "$INVENTORY_ID" ]] || die "$INVENTORY_NAME is missing"
  HOST_ID="$(find_id "inventories/$INVENTORY_ID/hosts/" "$HOST_NAME")"; [[ -n "$HOST_ID" ]] || die "$HOST_NAME is missing"
  CREDENTIAL_ID="$(find_id credentials "$CREDENTIAL_NAME")"; [[ -n "$CREDENTIAL_ID" ]] || die "$CREDENTIAL_NAME is missing"
  PROJECT_ID="$(find_id projects "$PROJECT_NAME")"; [[ -n "$PROJECT_ID" ]] || die "$PROJECT_NAME is missing"
  JOB_ID="$(find_id job-templates "$JOB_NAME")"; [[ -n "$JOB_ID" ]] || die "$JOB_NAME is missing"
  WORKFLOW_ID="$(find_id workflow-templates "$WORKFLOW_NAME")"; [[ -n "$WORKFLOW_ID" ]] || die "$WORKFLOW_NAME is missing"
  NOTIFICATION_ID="$(find_id "notification-templates?organization_id=$ORG_ID" "$NOTIFICATION_NAME")"; [[ -n "$NOTIFICATION_ID" ]] || die "$NOTIFICATION_NAME is missing"
  PACK_ID="$(find_id execution-packs "$PACK_NAME")"; [[ -n "$PACK_ID" ]] || die "$PACK_NAME execution pack is missing"
}

plan() {
  cat <<EOF
Pilot managed-host journey plan
  operator path:    demo-operator launch -> mwebb approval (backend-team)
  target scope:     $INVENTORY_NAME / $HOST_NAME with exact host limit
  content:          $PROJECT_REF:$PLAYBOOK using $PACK_NAME
  secret path:      $CREDENTIAL_NAME resolved once through a run-scoped claim
  assertions:       first run changed, second run unchanged, facts, notification, audit
  evidence:         sanitized mode-0600 JSON at $EVIDENCE_FILE
EOF
}

seed() {
  "$ROOT/scripts/staging-pilot-access.sh" seed >/dev/null
  stage_execution_pack
  kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status deployment/praetor-staging-acceptance-sink --timeout=30s >/dev/null || die "acceptance notification sink is unavailable"
  start_tunnel
  TOKEN="$(login demo-operator | jq -er .token)"
  ORG_ID="$(find_id organizations Engineering)"; TEAM_ID="$(find_id teams backend-team)"
  INVENTORY_ID="$(find_id inventories "$INVENTORY_NAME")"; CREDENTIAL_ID="$(find_id credentials "$CREDENTIAL_NAME")"
  PACK_ID="$(find_id execution-packs "$PACK_NAME")"; [[ -n "$PACK_ID" ]] || die "$PACK_NAME execution pack is missing"
  PROJECT_ID="$(ensure projects projects "$PROJECT_NAME" "$(jq -nc --argjson org "$ORG_ID" --arg name "$PROJECT_NAME" --arg branch "$PROJECT_REF" '{organization_id:$org,name:$name,scm_type:"git",scm_url:"https://github.com/Niftel/praetor.git",scm_branch:$branch}')")"
  project_branch="$(get projects | items | jq -r --argjson id "$PROJECT_ID" '.[] | select(.id == $id) | .scm_branch // ""')"
  [[ "$project_branch" == "$PROJECT_REF" ]] || die "$PROJECT_NAME uses branch '$project_branch', expected '$PROJECT_REF'"
  grant_team_role project "$PROJECT_ID" "Project Use" "$TEAM_ID"
  JOB_ID="$(ensure job-templates job-templates "$JOB_NAME" "$(jq -nc --argjson org "$ORG_ID" --argjson inv "$INVENTORY_ID" --argjson project "$PROJECT_ID" --argjson credential "$CREDENTIAL_ID" --argjson pack "$PACK_ID" --arg name "$JOB_NAME" --arg playbook "$PLAYBOOK" --arg limit "$HOST_NAME" '{organization_id:$org,inventory_id:$inv,project_id:$project,credential_id:$credential,execution_pack_id:$pack,name:$name,playbook:$playbook,job_type:"run",forks:1,limit:$limit,use_fact_cache:true}')")"
  WORKFLOW_ID="$(ensure workflow-templates workflow-templates "$WORKFLOW_NAME" "$(jq -nc --argjson org "$ORG_ID" --argjson job "$JOB_ID" --arg name "$WORKFLOW_NAME" '{organization_id:$org,name:$name,nodes:[{node_key:"approval",node_type:"approval",name:"Backend team approval"},{node_key:"execute",node_type:"job",job_template_id:$job,name:"Apply pilot marker"}],edges:[{parent_key:"approval",child_key:"execute",edge_type:"success"}]}')")"
  grant_team_role workflow_template "$WORKFLOW_ID" "Workflow Template Execute" "$TEAM_ID"
  grant_team_role workflow_template "$WORKFLOW_ID" "Workflow Template Approve" "$TEAM_ID"
  NOTIFICATION_ID="$(ensure notification-templates "notification-templates?organization_id=$ORG_ID" "$NOTIFICATION_NAME" "$(jq -nc --argjson org "$ORG_ID" --arg name "$NOTIFICATION_NAME" '{organization_id:$org,name:$name,notification_type:"webhook",config:{url:"http://praetor-staging-acceptance-sink:8080/echo"}}')")"
  attachments="$(get "workflow-templates/$WORKFLOW_ID/notifications")"
  if ! jq -e --argjson id "$NOTIFICATION_ID" '.[] | select(.notification_template_id == $id and .event == "approval")' <<<"$attachments" >/dev/null; then
    post "workflow-templates/$WORKFLOW_ID/notifications" "$(jq -nc --argjson id "$NOTIFICATION_ID" '{notification_template_id:$id,event:"approval"}')" >/dev/null
  fi
  echo "seeded pilot project $PROJECT_ID, job $JOB_ID, workflow $WORKFLOW_ID, pack $PACK_ID"
}

status_check() {
  "$ROOT/scripts/pilot-host.sh" status >/dev/null
  "$ROOT/scripts/staging-pilot-access.sh" status >/dev/null
  verify_execution_pack
  kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status deployment/praetor-staging-acceptance-sink --timeout=30s >/dev/null || die "acceptance notification sink is unavailable"
  start_tunnel
  TOKEN="$(login demo-operator | jq -er .token)"
  lookup
  template="$(get "job-templates/$JOB_ID")"
  jq -e --argjson inv "$INVENTORY_ID" --argjson cred "$CREDENTIAL_ID" --argjson pack "$PACK_ID" --arg playbook "$PLAYBOOK" --arg limit "$HOST_NAME" \
    '.inventory_id == $inv and .credential_id == $cred and .execution_pack_id == $pack and .playbook == $playbook and .limit == $limit and .use_fact_cache == true' <<<"$template" >/dev/null || die "pilot job template is not pinned to the expected inventory, credential, pack, playbook, and host limit"
  [[ "$(get "credentials/$CREDENTIAL_ID" | jq -r '.inputs.ssh_private_key')" == '$encrypted$' ]] || die "pilot credential is not sealed"
  echo "healthy: pilot workflow is pinned to one inventory, host, sealed credential, playbook, and execution pack"
}

launch_and_approve() {
  local operator_token="$1" approver_token="$2" outsider_token="$3" team_id="$4" workflow_id="$5" workflow_job_id approval_id approvals run terminal
  post_status "$operator_token" "workflow-templates/$workflow_id/launch" "$(jq -nc --argjson team "$team_id" --arg limit "$HOST_NAME" '{approval_team_id:$team,limit:$limit}')"
  [[ "$STATUS" == 201 ]] || die "pilot workflow launch returned HTTP $STATUS: $RESPONSE"
  workflow_job_id="$(jq -er .workflow_job_id <<<"$RESPONSE")"
  approval_id=""
  for _ in $(seq 1 60); do
    approvals="$(get_as "$approver_token" workflow-approvals)"
    approval_id="$(jq -r --argjson job "$workflow_job_id" '.[] | select(.workflow_job_id == $job) | .id' <<<"$approvals" | head -n1)"
    [[ -n "$approval_id" ]] && break
    sleep 1
  done
  [[ -n "$approval_id" ]] || die "backend-team approver did not receive workflow $workflow_job_id"
  [[ "$(get_as "$operator_token" workflow-approvals | jq --argjson job "$workflow_job_id" '[.[] | select(.workflow_job_id == $job)] | length')" == 0 ]] || die "requester can see their own approval"
  [[ "$(get_as "$outsider_token" workflow-approvals | jq --argjson job "$workflow_job_id" '[.[] | select(.workflow_job_id == $job)] | length')" == 0 ]] || die "frontend-team can see the pilot approval"
  post_status "$operator_token" "workflow-job-nodes/$approval_id/approve"; [[ "$STATUS" == 403 ]] || die "requester self-approval returned HTTP $STATUS"
  post_status "$outsider_token" "workflow-job-nodes/$approval_id/approve"; [[ "$STATUS" == 403 ]] || die "frontend-team approval returned HTTP $STATUS"
  post_status "$approver_token" "workflow-job-nodes/$approval_id/approve"; [[ "$STATUS" == 204 ]] || die "backend-team approval returned HTTP $STATUS: $RESPONSE"
  terminal=""
  for _ in $(seq 1 180); do
    run="$(get_as "$operator_token" "workflow-jobs/$workflow_job_id")"
    terminal="$(jq -r .status <<<"$run")"
    [[ "$terminal" =~ ^(successful|failed|error|canceled)$ ]] && break
    sleep 1
  done
  [[ "$terminal" == successful ]] || die "pilot workflow $workflow_job_id finished with '$terminal'"
  jq -nc --argjson workflow_job_id "$workflow_job_id" --argjson approval_id "$approval_id" --arg run_id "$(jq -r '.nodes[] | select(.node_key == "execute") | .run_id' <<<"$run")" '{workflow_job_id:$workflow_job_id,approval_id:$approval_id,run_id:$run_id}'
}

run_journey() {
  status_check >/dev/null
  install -d -m 0700 "$EVIDENCE_ROOT"
  docker exec "$PILOT_TARGET" rm -f /home/praetor/.praetor-pilot-marker
  operator_token="$TOKEN"
  approver_token="$(login mwebb | jq -er .token)"
  outsider_token="$(login fwalsh | jq -er .token)"
  auditor_token="$(login demo-auditor | jq -er .token)"
  start_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  first="$(launch_and_approve "$operator_token" "$approver_token" "$outsider_token" "$TEAM_ID" "$WORKFLOW_ID")"
  second="$(launch_and_approve "$operator_token" "$approver_token" "$outsider_token" "$TEAM_ID" "$WORKFLOW_ID")"
  first_run="$(jq -r .run_id <<<"$first")"; second_run="$(jq -r .run_id <<<"$second")"
  [[ -n "$first_run" && "$first_run" != null && -n "$second_run" && "$second_run" != null ]] || die "execution run IDs are missing"
  first_log="$(get_as "$operator_token" "jobs/runs/$first_run/logs")"
  second_log="$(get_as "$operator_token" "jobs/runs/$second_run/logs")"
  grep -Fq 'TASK [Install pilot marker]' <<<"$first_log" || die "first run did not execute the marker task"
  grep -Eq 'changed=1([[:space:]]|$)' <<<"$first_log" || die "first run did not report exactly one change"
  grep -Fq 'TASK [Install pilot marker]' <<<"$second_log" || die "second run did not execute the marker task"
  grep -Eq 'changed=0([[:space:]]|$)' <<<"$second_log" || die "second run was not idempotent"
  facts="$(get_as "$operator_token" "hosts/$HOST_ID/facts")"
  jq -e '.ansible_distribution == "Rocky" and (.ansible_distribution_major_version | tostring) == "9"' <<<"$facts" >/dev/null || die "Rocky Linux pilot facts were not ingested"
  first_job="$(jq -r .workflow_job_id <<<"$first")"; second_job="$(jq -r .workflow_job_id <<<"$second")"
  deliveries="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" logs deployment/praetor-staging-acceptance-sink --since-time="$start_time" | jq -Rsc --argjson first "$first_job" --argjson second "$second_job" '[split("\n")[] | fromjson? | select(.event == "approval" and (.job_id == $first or .job_id == $second))] | group_by(.job_id) | map(length)')"
  [[ "$deliveries" == '[1,1]' ]] || die "approval notifications were not delivered exactly once per run: $deliveries"
  audit="$(get_as "$auditor_token" 'activity-stream?limit=500')"
  for row in "$first" "$second"; do
    job="$(jq -r .workflow_job_id <<<"$row")"; approval="$(jq -r .approval_id <<<"$row")"
    jq -e --arg path "/api/v1/workflow-templates/$WORKFLOW_ID/launch" '.[] | select(.username == "demo-operator" and .method == "POST" and .path == $path and .status_code == 201)' <<<"$audit" >/dev/null || die "launch attribution is missing for workflow $job"
    jq -e --arg path "/api/v1/workflow-job-nodes/$approval/approve" '.[] | select(.username == "mwebb" and .method == "POST" and .path == $path and .status_code == 204)' <<<"$audit" >/dev/null || die "approval attribution is missing for workflow $job"
  done
  kubectl --context "$CONTEXT" -n "$NAMESPACE" get pod "$SECRETS_DB_POD" >/dev/null || die "staging Secrets database pod is missing: $SECRETS_DB_POD"
  secrets_pod="$SECRETS_DB_POD"
  for run_id in "$first_run" "$second_run"; do
    binding="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$secrets_pod" -- psql -U postgres -d praetor_secrets -At -F '|' -c "select state,resolution_count from run_bindings where run_id='$run_id'")"
    [[ "$binding" == canceled\|1 ]] || die "credential claim for $run_id was not resolved once and closed: $binding"
  done
  platform_version="$(awk '/^platformVersion:/ {print $2}' "$ROOT/deployments/staging/release-lock.yaml")"
  source_revision="$(git -C "$ROOT" rev-parse HEAD)"
  pack_digest="$(shasum -a 256 "$ROOT/build/runtime/ansible-runtime-linux-arm64.tar.gz" | awk '{print "sha256:"$1}')"
  target_image="$(docker inspect praetor-pilot-target --format '{{.Image}}')"
  jq -n --arg recorded_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg platform_version "$platform_version" --arg source_revision "$source_revision" --arg project_ref "$PROJECT_REF" --arg pack_digest "$pack_digest" --arg target_image "$target_image" \
    --argjson organization_id "$ORG_ID" --argjson team_id "$TEAM_ID" --argjson inventory_id "$INVENTORY_ID" --argjson host_id "$HOST_ID" --argjson credential_id "$CREDENTIAL_ID" --argjson project_id "$PROJECT_ID" --argjson job_template_id "$JOB_ID" --argjson workflow_id "$WORKFLOW_ID" --argjson execution_pack_id "$PACK_ID" --argjson first "$first" --argjson second "$second" \
    '{schema_version:1,journey:"managed-host-pilot",result:"pass",recorded_at:$recorded_at,revisions:{platform:$platform_version,source:$source_revision,project_ref:$project_ref,execution_pack:$pack_digest,target_image:$target_image},resources:{organization_id:$organization_id,team_id:$team_id,inventory_id:$inventory_id,host_id:$host_id,credential_id:$credential_id,project_id:$project_id,job_template_id:$job_template_id,workflow_id:$workflow_id,execution_pack_id:$execution_pack_id},runs:{first:$first,second:$second},checks:["exact-host-limit","requester-self-approval-denied","team-approval-isolated","credential-resolved-once","first-run-changed","second-run-idempotent","facts-ingested","notification-exact-once","audit-attributed"]}' >"$EVIDENCE_FILE"
  chmod 0600 "$EVIDENCE_FILE"
  if grep -Eiq '(private.?key|bearer |password|token|BEGIN [A-Z ]+ KEY|172\.29\.)' "$EVIDENCE_FILE"; then die "sensitive material appeared in pilot evidence"; fi
  jq -e '.result == "pass" and (.checks | length == 9) and .runs.first.run_id != .runs.second.run_id' "$EVIDENCE_FILE" >/dev/null || die "pilot evidence is incomplete"
  echo "pilot managed-host journey passed; sanitized evidence: $EVIDENCE_FILE"
}

case "$COMMAND" in
  plan) plan ;;
  seed) seed ;;
  status) status_check ;;
  run) run_journey ;;
esac
