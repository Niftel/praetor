#!/usr/bin/env bash
set -Eeuo pipefail

# Exercises the deployed fleet endpoints against the product-validation or
# persistent staging cluster. It creates only uniquely named synthetic hosts,
# deletes them through the preview/confirm contract, and records counts/IDs
# only—never credentials, tokens, request bodies, or infrastructure details.

NAMESPACE="${PRAETOR_VALIDATION_NAMESPACE:-praetor-secrets}"
RELEASE="${PRAETOR_HELM_RELEASE:-praetor}"
CONTEXT="${PRAETOR_VALIDATION_CONTEXT:-}"
API_PORT="${PRAETOR_FLEET_API_PORT:-18087}"
API="${PRAETOR_FLEET_API_URL:-http://127.0.0.1:$API_PORT/api/v1}"
USERNAME="${PRAETOR_FLEET_USERNAME:-admin}"
PASSWORD="${PRAETOR_FLEET_PASSWORD:-admin}"
EVIDENCE_FILE="${PRAETOR_FLEET_LIVE_EVIDENCE_FILE:-}"
BOOTSTRAP_FIXTURE="${PRAETOR_FLEET_BOOTSTRAP_FIXTURE:-false}"
PREFIX="${PRAETOR_FLEET_PREFIX:-Praetor Fleet E2E $(date -u +%Y%m%d%H%M%S)-$$}"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/praetor-fleet-live.XXXXXX")"
PORT_FORWARD_PID=""
PHASE="bootstrap"
TOKEN=""
CREATED_HOST_IDS=()
CREATED_ORGANIZATION_ID=""
CREATED_INVENTORY_ID=""
CREATED_PROJECT_ID=""
CREATED_TEMPLATE_ID=""

die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for command in curl jq; do need "$command"; done

KUBECTL=(kubectl)
[[ -z "$CONTEXT" ]] || KUBECTL+=(--context "$CONTEXT")

request() {
  local method="$1" path="$2" body="${3:-}" key="${4:-}" output="$WORK/response.json"
  local args=(-sS -o "$output" -w '%{http_code}' -X "$method")
  [[ -z "$TOKEN" ]] || args+=(-H "Authorization: Bearer $TOKEN")
  [[ -z "$body" ]] || args+=(-H 'Content-Type: application/json' -d "$body")
  [[ -z "$key" ]] || args+=(-H "Idempotency-Key: $key")
  STATUS="$(curl "${args[@]}" "$API/$path")"
  RESPONSE="$(cat "$output")"
}

cleanup() {
  set +e
  if [[ -n "$TOKEN" ]]; then
    for host_id in "${CREATED_HOST_IDS[@]}"; do
      request DELETE "inventories/$INVENTORY_ID/hosts/$host_id" >/dev/null 2>&1
    done
    [[ -z "$CREATED_TEMPLATE_ID" ]] || request DELETE "job-templates/$CREATED_TEMPLATE_ID" >/dev/null 2>&1
    [[ -z "$CREATED_PROJECT_ID" ]] || request DELETE "projects/$CREATED_PROJECT_ID" >/dev/null 2>&1
    [[ -z "$CREATED_INVENTORY_ID" ]] || request DELETE "inventories/$CREATED_INVENTORY_ID" >/dev/null 2>&1
    [[ -z "$CREATED_ORGANIZATION_ID" ]] || request DELETE "organizations/$CREATED_ORGANIZATION_ID/" >/dev/null 2>&1
  fi
  [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT

if [[ -z "${PRAETOR_FLEET_API_URL:-}" ]]; then
  need kubectl
  "${KUBECTL[@]}" port-forward -n "$NAMESPACE" "svc/$RELEASE-api" "$API_PORT:8080" >"$WORK/port-forward.log" 2>&1 &
  PORT_FORWARD_PID=$!
  for _ in $(seq 1 30); do
    curl -fsS "$API/ping" >/dev/null 2>&1 && break
    kill -0 "$PORT_FORWARD_PID" 2>/dev/null || { cat "$WORK/port-forward.log" >&2; die "API tunnel stopped"; }
    sleep 1
  done
fi
curl -fsS "$API/ping" >/dev/null || die "deployed API did not become reachable"

PHASE="authenticate"
request POST auth/login "$(jq -nc --arg username "$USERNAME" --arg password "$PASSWORD" '{username:$username,password:$password}')"
[[ "$STATUS" == 200 ]] || die "fleet identity could not authenticate: HTTP $STATUS"
TOKEN="$(jq -er .token <<<"$RESPONSE")"

PHASE="discover-fixture"
request GET inventories
[[ "$STATUS" == 200 ]] || die "inventory discovery returned HTTP $STATUS"
INVENTORY_ID="$(jq -r '[if type == "object" and has("items") then .items[] else .[] end | select(.name == "Praetor Validation Inventory")][0].id // empty' <<<"$RESPONSE")"
request GET job-templates
[[ "$STATUS" == 200 ]] || die "job-template discovery returned HTTP $STATUS"
UNIFIED_TEMPLATE_ID="$(jq -r '[if type == "object" and has("items") then .items[] else .[] end | select(.name == "Praetor Validation Job")][0].unified_job_template_id // empty' <<<"$RESPONSE")"
if [[ -z "$INVENTORY_ID" || -z "$UNIFIED_TEMPLATE_ID" ]]; then
  [[ "$BOOTSTRAP_FIXTURE" == true ]] || die "Praetor Validation Inventory and Job fixture resources are required"
  PHASE="bootstrap-api-fixture"
  request POST organizations "$(jq -nc --arg name "$PREFIX Organization" '{name:$name,description:"Temporary fleet validation boundary"}')"
  [[ "$STATUS" == 201 ]] || die "temporary organization creation returned HTTP $STATUS"
  CREATED_ORGANIZATION_ID="$(jq -er .id <<<"$RESPONSE")"
  request POST inventories "$(jq -nc --argjson org "$CREATED_ORGANIZATION_ID" --arg name "$PREFIX Inventory" '{organization_id:$org,name:$name,kind:"static"}')"
  [[ "$STATUS" == 201 ]] || die "temporary inventory creation returned HTTP $STATUS"
  CREATED_INVENTORY_ID="$(jq -er .id <<<"$RESPONSE")"
  INVENTORY_ID="$CREATED_INVENTORY_ID"
  request POST projects "$(jq -nc --argjson org "$CREATED_ORGANIZATION_ID" --arg name "$PREFIX Project" '{organization_id:$org,name:$name,scm_type:"git",scm_url:"https://github.com/Niftel/praetor.git"}')"
  [[ "$STATUS" == 201 ]] || die "temporary project creation returned HTTP $STATUS"
  CREATED_PROJECT_ID="$(jq -er .id <<<"$RESPONSE")"
  request POST job-templates "$(jq -nc --argjson org "$CREATED_ORGANIZATION_ID" --argjson inventory "$INVENTORY_ID" --argjson project "$CREATED_PROJECT_ID" --arg name "$PREFIX Job" '{
    organization_id:$org,inventory_id:$inventory,project_id:$project,name:$name,
    playbook:"playbooks/ping.yml",job_type:"run",forks:1
  }')"
  [[ "$STATUS" == 201 ]] || die "temporary job-template creation returned HTTP $STATUS"
  CREATED_TEMPLATE_ID="$(jq -er .id <<<"$RESPONSE")"
  UNIFIED_TEMPLATE_ID="$(jq -er .unified_job_template_id <<<"$RESPONSE")"
fi

PHASE="bulk-create"
host_a="$PREFIX web"
host_b="$PREFIX db"
create_body="$(jq -nc --argjson inventory "$INVENTORY_ID" --arg a "$host_a" --arg b "$host_b" '{
  items:[
    {identifier:"web",inventory_id:$inventory,name:$a,variables:{ansible_connection:"local"}},
    {identifier:"db",inventory_id:$inventory,name:$b,variables:{ansible_connection:"local"}},
    {identifier:"duplicate",inventory_id:$inventory,name:$a}
  ]
}')"
create_key="fleet-create-$(date +%s)-$$"
request POST bulk/hosts/create "$create_body" "$create_key"
[[ "$STATUS" == 207 ]] || die "mixed host creation returned HTTP $STATUS: $RESPONSE"
jq -e '
  .complete == true and
  [.results[].status] == ["created","created","rejected"] and
  .results[2].code == "duplicate"
' <<<"$RESPONSE" >/dev/null || die "host creation did not preserve ordered mixed outcomes"
while IFS= read -r host_id; do
  CREATED_HOST_IDS[${#CREATED_HOST_IDS[@]}]="$host_id"
done < <(jq -r '.results[] | select(.status == "created") | .host_id' <<<"$RESPONSE")
[[ "${#CREATED_HOST_IDS[@]}" == 2 ]] || die "host creation did not return two created host IDs"
create_response="$RESPONSE"
request POST bulk/hosts/create "$create_body" "$create_key"
[[ "$STATUS" == 207 && "$RESPONSE" == "$create_response" ]] || die "host creation replay changed its response"

PHASE="bulk-launch"
launch_body="$(jq -nc --argjson template "$UNIFIED_TEMPLATE_ID" '{
  items:[
    {identifier:"authorized",unified_job_template_id:$template,name:"Fleet validation",limit:"Praetor Fleet E2E*"},
    {identifier:"unknown",unified_job_template_id:9223372036854775000,name:"Hidden"}
  ]
}')"
launch_key="fleet-launch-$(date +%s)-$$"
request POST bulk/jobs/launch "$launch_body" "$launch_key"
[[ "$STATUS" == 207 ]] || die "mixed job launch returned HTTP $STATUS: $RESPONSE"
jq -e '
  .complete == true and
  [.results[].status] == ["accepted","rejected"] and
  .results[1].code == "not_found_or_forbidden" and
  (.results[1].job_id // 0) == 0
' <<<"$RESPONSE" >/dev/null || die "job launch did not fail closed with ordered mixed outcomes"
JOB_ID="$(jq -er '.results[0].job_id' <<<"$RESPONSE")"
launch_response="$RESPONSE"
request POST bulk/jobs/launch "$launch_body" "$launch_key"
[[ "$STATUS" == 207 && "$RESPONSE" == "$launch_response" ]] || die "job launch replay changed its response"

oversized_items="$(jq -nc '[range(0;26) | {identifier:("bounded-"+tostring),unified_job_template_id:9223372036854775000,name:"bounded"}] | {items:.}')"
request POST bulk/jobs/launch "$oversized_items" "fleet-bounded-$(date +%s)-$$"
[[ "$STATUS" == 400 ]] || die "oversized launch was not rejected before scheduling: HTTP $STATUS"

PHASE="preview-confirm-delete"
preview_body="$(jq -nc --argjson a "${CREATED_HOST_IDS[0]}" --argjson b "${CREATED_HOST_IDS[1]}" '{
  items:[
    {identifier:"web",host_id:$a},
    {identifier:"db",host_id:$b},
    {identifier:"unknown",host_id:9223372036854775000}
  ]
}')"
request POST bulk/hosts/delete/preview "$preview_body"
[[ "$STATUS" == 201 ]] || die "host deletion preview returned HTTP $STATUS: $RESPONSE"
jq -e '
  [.results[].status] == ["ready","ready","rejected"] and
  .results[2].code == "not_found_or_forbidden" and
  (.results[2].host_id // 0) == 0
' <<<"$RESPONSE" >/dev/null || die "deletion preview did not fail closed with ordered mixed outcomes"
confirmation_token="$(jq -er .confirmation_token <<<"$RESPONSE")"
delete_key="fleet-delete-$(date +%s)-$$"
request POST bulk/hosts/delete "$(jq -nc --arg value "$confirmation_token" '{confirmation_token:$value}')" "$delete_key"
[[ "$STATUS" == 207 ]] || die "confirmed host deletion returned HTTP $STATUS: $RESPONSE"
jq -e '[.results[].status] == ["deleted","deleted","rejected"]' <<<"$RESPONSE" >/dev/null || die "confirmed deletion changed preview ordering"
delete_response="$RESPONSE"
request POST bulk/hosts/delete "$(jq -nc --arg value "$confirmation_token" '{confirmation_token:$value}')" "$delete_key"
[[ "$STATUS" == 207 && "$RESPONSE" == "$delete_response" ]] || die "host deletion replay changed its response"
CREATED_HOST_IDS=()

PHASE="audit-attribution"
request GET "activity-stream?limit=1000"
[[ "$STATUS" == 200 ]] || die "activity evidence returned HTTP $STATUS"
jq -e --arg username "$USERNAME" --argjson job "$JOB_ID" '
  any(.[]; .username == $username and .principal_kind == "human" and
    .action == "launch" and .resource_type == "unified_job" and .resource_id == $job)
' <<<"$RESPONSE" >/dev/null || die "launch activity is missing actor/resource attribution"
for host_id in "$(jq -r '.results[0].host_id' <<<"$delete_response")" "$(jq -r '.results[1].host_id' <<<"$delete_response")"; do
  jq -e --arg username "$USERNAME" --argjson host "$host_id" '
    any(.[]; .username == $username and .principal_kind == "human" and
      .action == "delete" and .resource_type == "host" and .resource_id == $host)
  ' <<<"$RESPONSE" >/dev/null || die "host deletion activity is missing actor/resource attribution"
done

evidence="$(jq -n \
  --arg recorded_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson created_hosts 2 \
  --argjson accepted_jobs 1 \
  --argjson deleted_hosts 2 \
  '{
    schema_version:1,
    journey:"fleet-scale-live",
    result:"pass",
    recorded_at:$recorded_at,
    counts:{created_hosts:$created_hosts,accepted_jobs:$accepted_jobs,deleted_hosts:$deleted_hosts},
    checks:[
      "deployed-api",
      "mixed-outcomes",
      "fail-closed-identifiers",
      "idempotent-replay",
      "request-bounds",
      "preview-confirm-delete",
      "audit-attribution"
    ]
  }')"
if [[ -n "$EVIDENCE_FILE" ]]; then
  mkdir -p "$(dirname "$EVIDENCE_FILE")"
  umask 077
  printf '%s\n' "$evidence" >"$EVIDENCE_FILE"
fi
printf '%s\n' "$evidence"
