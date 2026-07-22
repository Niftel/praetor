#!/usr/bin/env bash
set -Eeuo pipefail

# Proves the complete dynamic-inventory operator boundary against the disposable
# product-validation cluster. Provider data and credentials are synthetic; the
# execution, sealing, RBAC, scheduling and reconciliation paths are production code.

NAMESPACE="${PRAETOR_VALIDATION_NAMESPACE:-praetor-secrets}"
RELEASE="${PRAETOR_HELM_RELEASE:-praetor}"
API_PORT="${PRAETOR_DYNAMIC_INVENTORY_API_PORT:-18083}"
API="http://127.0.0.1:$API_PORT/api/v1"
PASSWORD="${PRAETOR_VALIDATION_LDAP_PASSWORD:-praetor123}"
PREFIX="Dynamic Inventory E2E"
SECRET="praetor-dynamic-inventory-fixture"
EVIDENCE_FILE="${PRAETOR_DYNAMIC_INVENTORY_EVIDENCE_FILE:-}"
DB_OBSERVER_POD="${PRAETOR_DYNAMIC_INVENTORY_DB_OBSERVER_POD:-}"
PORT_FORWARD_PID=""
WORK="$(mktemp -d "${TMPDIR:-/tmp}/praetor-dynamic-inventory.XXXXXX")"
PHASE="bootstrap"
INVENTORY_ID=""; SOURCE_ID=""; CREDENTIAL_ID=""; SCHEDULE_ID=""

die() { echo "error: $*" >&2; record_failure; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for command in curl jq kubectl; do need "$command"; done
record_failure() {
  if [[ -n "$EVIDENCE_FILE" && -s "$EVIDENCE_FILE" ]] &&
    jq -e '.journey == "dynamic-inventory" and .result == "fail"' "$EVIDENCE_FILE" >/dev/null 2>&1; then
    return
  fi
  echo "error: dynamic inventory journey failed during phase '$PHASE'" >&2
  [[ -z "$EVIDENCE_FILE" ]] || {
    umask 077
    mkdir -p "$(dirname "$EVIDENCE_FILE")"
    jq -n --arg phase "$PHASE" \
      '{schema_version:1,journey:"dynamic-inventory",result:"fail",phase:$phase}' >"$EVIDENCE_FILE"
  }
}
cleanup() { [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null || true; rm -rf "$WORK"; }
trap record_failure ERR

kubectl port-forward -n "$NAMESPACE" "svc/$RELEASE-api" "$API_PORT:8080" >"$WORK/port-forward.log" 2>&1 &
PORT_FORWARD_PID=$!
for _ in $(seq 1 30); do
  curl -fsS "$API/ping" >/dev/null 2>&1 && break
  kill -0 "$PORT_FORWARD_PID" 2>/dev/null || { cat "$WORK/port-forward.log" >&2; die "API tunnel stopped"; }
  sleep 1
done
curl -fsS "$API/ping" >/dev/null || die "API did not become reachable"

login() {
  local username="$1" password="$2" output="$WORK/login-response.json" status response
  status="$(curl -sS -o "$output" -w '%{http_code}' -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg username "$username" --arg password "$password" '{username:$username,password:$password}')" "$API/auth/login")"
  response="$(cat "$output")"
  [[ "$status" == 200 ]] || die "login for $username returned $status"
  jq -er .token <<<"$response"
}
ADMIN_TOKEN="$(login "${PRAETOR_VALIDATION_ADMIN_USERNAME:-admin}" "${PRAETOR_VALIDATION_ADMIN_PASSWORD:-admin}")"
OPERATOR_TOKEN="$(login demo-operator "$PASSWORD")"
OUTSIDER_TOKEN="$(login fwalsh "$PASSWORD")"

request() {
  local token="$1" method="$2" path="$3" body="${4:-}" output="$WORK/response.json"
  local args=(-sS -o "$output" -w '%{http_code}' -X "$method" -H "Authorization: Bearer $token")
  [[ -z "$body" ]] || args+=(-H 'Content-Type: application/json' -d "$body")
  STATUS="$(curl "${args[@]}" "$API/$path")"; RESPONSE="$(cat "$output")"
}
resource_cleanup() {
  local strict="${1:-false}" spec status failed=0
  [[ -n "${ADMIN_TOKEN:-}" ]] || return 0
  for spec in \
    "${SCHEDULE_ID:+schedules/$SCHEDULE_ID}" \
    "${SOURCE_ID:+inventories/$INVENTORY_ID/sources/$SOURCE_ID}" \
    "${CREDENTIAL_ID:+credentials/$CREDENTIAL_ID}" \
    "${INVENTORY_ID:+inventories/$INVENTORY_ID}"; do
    [[ -z "$spec" ]] && continue
    request "$ADMIN_TOKEN" DELETE "$spec"
    if [[ "$STATUS" != 204 && "$STATUS" != 404 ]]; then
      echo "error: cleanup $spec returned $STATUS" >&2; failed=1
    fi
  done
  SCHEDULE_ID=""; SOURCE_ID=""; CREDENTIAL_ID=""; INVENTORY_ID=""
  [[ "$strict" != true || "$failed" == 0 ]]
}
trap 'resource_cleanup false; cleanup' EXIT
get() {
  local token="$1" path="$2" output="$WORK/get-response.json" status
  status="$(curl -sS -o "$output" -w '%{http_code}' -H "Authorization: Bearer $token" "$API/$path")"
  [[ "$status" == 200 ]] || die "GET /api/v1/$path returned $status"
  cat "$output"
}
items() { jq -c 'if type == "object" and has("items") then .items else . end'; }
find_id() { get "$ADMIN_TOKEN" "$1" | items | jq -r --arg name "$2" '.[] | select(.name == $name) | .id' | head -n1; }

ensure_post() {
  local list="$1" path="$2" name="$3" body="$4" id
  id="$(find_id "$list" "$name")"
  if [[ -z "$id" ]]; then request "$ADMIN_TOKEN" POST "$path" "$body"; [[ "$STATUS" == 201 ]] || die "create $name returned $STATUS: $RESPONSE"; id="$(jq -er .id <<<"$RESPONSE")"; fi
  printf '%s' "$id"
}
grant_team_role() {
  local content_type="$1" object_id="$2" role_name="$3" team_id="$4" role_id
  role_id="$(get "$ADMIN_TOKEN" "role-definitions?content_type=$content_type" | jq -er --arg name "$role_name" '.[] | select(.name == $name) | .id' | head -n1)"
  request "$ADMIN_TOKEN" POST access "$(jq -nc --arg type "$content_type" --argjson object "$object_id" --argjson role "$role_id" --argjson team "$team_id" '{content_type:$type,object_id:$object,role_definition_id:$role,team_id:$team}')"
  [[ "$STATUS" == 201 || "$STATUS" == 204 || "$STATUS" == 409 ]] || die "grant $role_name returned $STATUS: $RESPONSE"
}
job_state() {
  local job_id="$1"
  [[ "$job_id" =~ ^[0-9]+$ ]] || die "invalid unified job id"
  if [[ -n "$DB_OBSERVER_POD" ]]; then
    kubectl exec -n "$NAMESPACE" "$DB_OBSERVER_POD" -- \
      psql -U postgres -d praetor -Atc "SELECT status FROM unified_jobs WHERE id=$job_id"
    return
  fi
  get "$ADMIN_TOKEN" jobs | jq -r --argjson id "$job_id" '.[] | select(.id == $id) | .status' | head -n1
}
wait_job() {
  local job_id="$1" expected="$2" state=""
  for _ in $(seq 1 180); do
    # Inventory-source jobs have no job template and are therefore deliberately
    # absent from a regular user's /jobs collection. Observe their state through
    # the supported administrator collection while all mutations remain scoped
    # to the operator token.
    state="$(job_state "$job_id")"
    [[ "$state" =~ ^(successful|failed|error|canceled)$ ]] && break
    sleep 1
  done
  [[ "$state" == "$expected" ]] || die "job $job_id finished '$state', expected '$expected'"
}
history() { get "$1" "inventories/$2/sources/$3/history?limit=20"; }
wait_history_total() {
  local token="$1" inventory="$2" source="$3" minimum="$4" result
  for _ in $(seq 1 180); do
    result="$(history "$token" "$inventory" "$source")"
    if jq -e --argjson minimum "$minimum" \
      '.total >= $minimum and (.results[0].status | IN("successful", "failed", "error", "canceled"))' <<<"$result" >/dev/null; then
      printf '%s' "$result"
      return
    fi
    sleep 1
  done
  die "inventory source history did not reach $minimum terminal entries"
}
set_provider_payload() {
  local payload="$1" expected="$2"
  kubectl create configmap praetor-validation-inventory-provider -n "$NAMESPACE" \
    --from-literal="inventory.json=$payload" \
    --from-literal="default.conf=$(kubectl get configmap praetor-validation-inventory-provider -n "$NAMESPACE" -o jsonpath='{.data.default\.conf}')" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  # A projected ConfigMap may take up to a kubelet sync period to refresh in an
  # existing pod. Restart the tiny synthetic provider so each reconciliation
  # phase observes exactly the payload that phase just published.
  kubectl rollout restart -n "$NAMESPACE" deployment/praetor-validation-inventory-provider >/dev/null
  kubectl rollout status -n "$NAMESPACE" deployment/praetor-validation-inventory-provider --timeout=60s >/dev/null
  for _ in $(seq 1 30); do
    kubectl exec -n "$NAMESPACE" deployment/praetor-validation-inventory-provider -- \
      wget -qO- --header="Authorization: Bearer $SECRET" http://127.0.0.1:8080/inventory 2>/dev/null | grep -Fq "$expected" && return
    sleep 1
  done
  die "synthetic provider did not publish $expected"
}

PHASE="resource-discovery"
ORG_ID="$(find_id organizations Engineering)"; [[ -n "$ORG_ID" ]] || die "Engineering organization is missing"
TEAM_ID="$(find_id teams backend-team)"; [[ -n "$TEAM_ID" ]] || die "backend-team is missing"
INVENTORY_ID="$(find_id inventories "$PREFIX Inventory")"
if [[ -z "$INVENTORY_ID" ]]; then
  request "$ADMIN_TOKEN" POST inventories "$(jq -nc --argjson org "$ORG_ID" --arg name "$PREFIX Inventory" '{organization_id:$org,name:$name,kind:"dynamic"}')"
  [[ "$STATUS" == 201 ]] || die "create inventory returned $STATUS: $RESPONSE"; INVENTORY_ID="$(jq -er .id <<<"$RESPONSE")"
fi

PHASE="credential-and-source"
CREDENTIAL_TYPE_ID="$(find_id credential-types Machine)"; [[ -n "$CREDENTIAL_TYPE_ID" ]] || die "built-in Machine credential type is missing"
CREDENTIAL_ID="$(ensure_post credentials credentials "$PREFIX Credential" "$(jq -nc --argjson org "$ORG_ID" --argjson type "$CREDENTIAL_TYPE_ID" --arg name "$PREFIX Credential" --arg secret "$SECRET" '{organization_id:$org,credential_type_id:$type,name:$name,inputs:{username:"dynamic-inventory",password:$secret}}')")"
[[ "$(get "$ADMIN_TOKEN" "credentials/$CREDENTIAL_ID" | jq -r .inputs.password)" == '$encrypted$' ]] || die "provider credential is not sealed"

SOURCE_SCRIPT='#!/usr/bin/env python3
import os, time, urllib.request
with open(os.environ["ANSIBLE_PASSWORD_FILE"], encoding="utf-8") as credential_file:
    token = credential_file.read()
if token == "praetor-timeout-fixture":
    time.sleep(70)
request = urllib.request.Request("http://praetor-validation-inventory-provider:8080/inventory")
request.add_header("Authorization", "Bearer " + token)
print(urllib.request.urlopen(request, timeout=10).read().decode())'
SOURCE_ID="$(get "$ADMIN_TOKEN" "inventories/$INVENTORY_ID/sources" | jq -r --arg name "$PREFIX Source" '.[] | select(.name == $name) | .id' | head -n1)"
if [[ -z "$SOURCE_ID" ]]; then
  request "$ADMIN_TOKEN" POST "inventories/$INVENTORY_ID/sources" "$(jq -nc --arg name "$PREFIX Source" --arg source "$SOURCE_SCRIPT" --argjson credential "$CREDENTIAL_ID" '{name:$name,source_type:"custom",source_kind:"script",source:$source,credential_id:$credential,reconciliation_policy:"disable_missing"}')"
  [[ "$STATUS" == 201 ]] || die "create source returned $STATUS: $RESPONSE"; SOURCE_ID="$(jq -er .id <<<"$RESPONSE")"
fi
grant_team_role inventory "$INVENTORY_ID" "Inventory Use" "$TEAM_ID"
grant_team_role inventory "$INVENTORY_ID" "Inventory Update" "$TEAM_ID"
grant_team_role credential "$CREDENTIAL_ID" "Credential Use" "$TEAM_ID"

INITIAL='{"_meta":{"hostvars":{"dynamic-alpha":{"fixture_revision":1},"dynamic-beta":{"fixture_revision":1}}},"validation":{"hosts":["dynamic-alpha","dynamic-beta"]}}'
set_provider_payload "$INITIAL" dynamic-beta

PHASE="rbac"
# Another team cannot discover or operate the source.
get "$OUTSIDER_TOKEN" inventories | items | jq -e --argjson id "$INVENTORY_ID" 'all(.[]; .id != $id)' >/dev/null || die "unauthorized team can list dynamic inventory"
for operation in preview sync history; do
  path="inventories/$INVENTORY_ID/sources/$SOURCE_ID/$operation"; method=POST; [[ "$operation" == history ]] && method=GET
  request "$OUTSIDER_TOKEN" "$method" "$path"
  [[ "$STATUS" == 403 ]] || die "unauthorized $operation returned $STATUS, expected 403"
done

PHASE="preview"
# Preview succeeds without mutating inventory or creating synchronization history.
request "$OPERATOR_TOKEN" POST "inventories/$INVENTORY_ID/sources/$SOURCE_ID/preview"
[[ "$STATUS" == 201 ]] || die "preview returned $STATUS: $RESPONSE"; PREVIEW_JOB="$(jq -er .job_id <<<"$RESPONSE")"; wait_job "$PREVIEW_JOB" successful
[[ "$(history "$OPERATOR_TOKEN" "$INVENTORY_ID" "$SOURCE_ID" | jq -r .total)" == 0 ]] || die "preview mutated synchronization history"

PHASE="initial-sync"
# Initial synchronization creates two source-owned hosts.
request "$OPERATOR_TOKEN" POST "inventories/$INVENTORY_ID/sources/$SOURCE_ID/sync"
[[ "$STATUS" == 201 ]] || die "initial sync returned $STATUS: $RESPONSE"; INITIAL_JOB="$(jq -er .job_id <<<"$RESPONSE")"; wait_job "$INITIAL_JOB" successful
INITIAL_HISTORY="$(wait_history_total "$OPERATOR_TOKEN" "$INVENTORY_ID" "$SOURCE_ID" 1)"
if ! jq -e '.results[0] | .status == "successful" and .hosts_added == 2 and .hosts_disabled == 0' <<<"$INITIAL_HISTORY" >/dev/null; then
  INITIAL_DELTA="$(jq -c '.results[0] | {status,phase,hosts_added,hosts_updated,hosts_disabled,hosts_unchanged,groups_added,groups_updated,groups_unchanged,diagnostic_code}' <<<"$INITIAL_HISTORY")"
  die "initial sync delta is incorrect: $INITIAL_DELTA"
fi

PHASE="changed-reconciliation"
# Provider change updates alpha, adds gamma and safely disables missing beta.
CHANGED='{"_meta":{"hostvars":{"dynamic-alpha":{"fixture_revision":2},"dynamic-gamma":{"fixture_revision":2}}},"validation":{"hosts":["dynamic-alpha","dynamic-gamma"]}}'
set_provider_payload "$CHANGED" dynamic-gamma
request "$OPERATOR_TOKEN" POST "inventories/$INVENTORY_ID/sources/$SOURCE_ID/sync"
[[ "$STATUS" == 201 ]] || die "changed sync returned $STATUS: $RESPONSE"; CHANGED_JOB="$(jq -er .job_id <<<"$RESPONSE")"; wait_job "$CHANGED_JOB" successful
CHANGED_HISTORY="$(wait_history_total "$OPERATOR_TOKEN" "$INVENTORY_ID" "$SOURCE_ID" 2)"
jq -e '.results[0] | .status == "successful" and .hosts_added == 1 and .hosts_updated == 1 and .hosts_disabled == 1' <<<"$CHANGED_HISTORY" >/dev/null || die "changed sync delta is incorrect"
HOSTS="$(get "$OPERATOR_TOKEN" "inventories/$INVENTORY_ID/hosts" | items)"
jq -e '[.[] | select(.name == "dynamic-beta" and .enabled == false)] | length == 1' <<<"$HOSTS" >/dev/null || die "missing host was not safely disabled"

PHASE="invalid-credential"
request "$ADMIN_TOKEN" PUT "credentials/$CREDENTIAL_ID" "$(jq -nc --argjson org "$ORG_ID" --argjson type "$CREDENTIAL_TYPE_ID" --arg name "$PREFIX Credential" '{organization_id:$org,credential_type_id:$type,name:$name,inputs:{username:"dynamic-inventory",password:"invalid-fixture-token"}}')"
[[ "$STATUS" == 200 ]] || die "invalidate credential returned $STATUS: $RESPONSE"
request "$OPERATOR_TOKEN" POST "inventories/$INVENTORY_ID/sources/$SOURCE_ID/sync"
[[ "$STATUS" == 201 ]] || die "invalid-credential sync returned $STATUS: $RESPONSE"; INVALID_JOB="$(jq -er .job_id <<<"$RESPONSE")"; wait_job "$INVALID_JOB" failed
INVALID_HISTORY="$(wait_history_total "$OPERATOR_TOKEN" "$INVENTORY_ID" "$SOURCE_ID" 3)"
jq -e '.results[0] | .status == "failed" and .diagnostic_code == "provider_acquisition_failed" and (.diagnostic_details == {})' <<<"$INVALID_HISTORY" >/dev/null || die "invalid credential did not fail safely"
! grep -Fq 'invalid-fixture-token' <<<"$INVALID_HISTORY" || die "invalid credential leaked into history"
request "$ADMIN_TOKEN" PUT "credentials/$CREDENTIAL_ID" "$(jq -nc --argjson org "$ORG_ID" --argjson type "$CREDENTIAL_TYPE_ID" --arg name "$PREFIX Credential" --arg secret "$SECRET" '{organization_id:$org,credential_type_id:$type,name:$name,inputs:{username:"dynamic-inventory",password:$secret}}')"
[[ "$STATUS" == 200 ]] || die "restore credential returned $STATUS: $RESPONSE"
request "$OPERATOR_TOKEN" POST "inventories/$INVENTORY_ID/sources/$SOURCE_ID/sync"
[[ "$STATUS" == 201 ]] || die "credential recovery sync returned $STATUS: $RESPONSE"; CREDENTIAL_RECOVERY_JOB="$(jq -er .job_id <<<"$RESPONSE")"; wait_job "$CREDENTIAL_RECOVERY_JOB" successful
wait_history_total "$OPERATOR_TOKEN" "$INVENTORY_ID" "$SOURCE_ID" 4 >/dev/null

PHASE="provider-timeout"
request "$ADMIN_TOKEN" PUT "credentials/$CREDENTIAL_ID" "$(jq -nc --argjson org "$ORG_ID" --argjson type "$CREDENTIAL_TYPE_ID" --arg name "$PREFIX Credential" '{organization_id:$org,credential_type_id:$type,name:$name,inputs:{username:"dynamic-inventory",password:"praetor-timeout-fixture"}}')"
[[ "$STATUS" == 200 ]] || die "set timeout credential returned $STATUS: $RESPONSE"
request "$OPERATOR_TOKEN" POST "inventories/$INVENTORY_ID/sources/$SOURCE_ID/sync"
[[ "$STATUS" == 201 ]] || die "timeout sync returned $STATUS: $RESPONSE"; TIMEOUT_JOB="$(jq -er .job_id <<<"$RESPONSE")"; wait_job "$TIMEOUT_JOB" failed
TIMEOUT_HISTORY="$(wait_history_total "$OPERATOR_TOKEN" "$INVENTORY_ID" "$SOURCE_ID" 5)"
jq -e '.results[0] | .status == "failed" and .diagnostic_code == "provider_timeout" and (.diagnostic_details == {})' <<<"$TIMEOUT_HISTORY" >/dev/null || die "provider timeout did not fail safely"
request "$ADMIN_TOKEN" PUT "credentials/$CREDENTIAL_ID" "$(jq -nc --argjson org "$ORG_ID" --argjson type "$CREDENTIAL_TYPE_ID" --arg name "$PREFIX Credential" --arg secret "$SECRET" '{organization_id:$org,credential_type_id:$type,name:$name,inputs:{username:"dynamic-inventory",password:$secret}}')"
[[ "$STATUS" == 200 ]] || die "restore credential after timeout returned $STATUS: $RESPONSE"

PHASE="malformed-provider-output"
# Malformed output fails in validation, records a bounded diagnostic, and recovers.
set_provider_payload '{"_meta":' '"_meta"'
request "$OPERATOR_TOKEN" POST "inventories/$INVENTORY_ID/sources/$SOURCE_ID/sync"
[[ "$STATUS" == 201 ]] || die "fault sync returned $STATUS: $RESPONSE"; FAULT_JOB="$(jq -er .job_id <<<"$RESPONSE")"; wait_job "$FAULT_JOB" failed
FAULT_HISTORY="$(wait_history_total "$OPERATOR_TOKEN" "$INVENTORY_ID" "$SOURCE_ID" 6)"
jq -e '.results[0] | .status == "failed" and (.diagnostic_code | length > 0) and (.diagnostic_message | length > 0) and (.diagnostic_details == {})' <<<"$FAULT_HISTORY" >/dev/null || die "safe failure diagnostic is missing"
! grep -Fq "$SECRET" <<<"$FAULT_HISTORY" || die "credential leaked into history"

set_provider_payload "$CHANGED" dynamic-gamma
PHASE="recovery"
request "$OPERATOR_TOKEN" POST "inventories/$INVENTORY_ID/sources/$SOURCE_ID/sync"
[[ "$STATUS" == 201 ]] || die "recovery sync returned $STATUS: $RESPONSE"; RECOVERY_JOB="$(jq -er .job_id <<<"$RESPONSE")"; wait_job "$RECOVERY_JOB" successful
RECOVERY_HISTORY="$(wait_history_total "$OPERATOR_TOKEN" "$INVENTORY_ID" "$SOURCE_ID" 7)"
jq -e '.results[0] | .status == "successful" and .hosts_unchanged == 2' <<<"$RECOVERY_HISTORY" >/dev/null || die "recovery sync did not converge"

PHASE="scheduling"
# Scheduling uses the same source-scoped update permission and is hidden from outsiders.
START="$(date -u -v+2M +%Y%m%dT%H%M%SZ 2>/dev/null || date -u -d '+2 minutes' +%Y%m%dT%H%M%SZ)"
RRULE="$(printf 'DTSTART:%s\nRRULE:FREQ=DAILY;COUNT=1' "$START")"
request "$OPERATOR_TOKEN" POST schedules "$(jq -nc --arg name "$PREFIX Schedule" --argjson source "$SOURCE_ID" --arg rule "$RRULE" '{name:$name,inventory_source_id:$source,rrule:$rule}')"
[[ "$STATUS" == 201 ]] || die "source schedule returned $STATUS: $RESPONSE"; SCHEDULE_ID="$(jq -er .id <<<"$RESPONSE")"
get "$OUTSIDER_TOKEN" schedules | jq -e --argjson id "$SCHEDULE_ID" 'all(.[]; .id != $id)' >/dev/null || die "unauthorized team can list source schedule"

PHASE="evidence"
EVIDENCE="$(jq -n --argjson inventory_id "$INVENTORY_ID" --argjson source_id "$SOURCE_ID" --argjson schedule_id "$SCHEDULE_ID" --argjson initial_job "$INITIAL_JOB" --argjson changed_job "$CHANGED_JOB" --argjson invalid_job "$INVALID_JOB" --argjson timeout_job "$TIMEOUT_JOB" --argjson fault_job "$FAULT_JOB" --argjson recovery_job "$RECOVERY_JOB" '{schema_version:1,journey:"dynamic-inventory",result:"pass",inventory_id:$inventory_id,source_id:$source_id,schedule_id:$schedule_id,jobs:{initial:$initial_job,changed:$changed_job,invalid_credential:$invalid_job,timeout:$timeout_job,fault:$fault_job,recovery:$recovery_job},checks:["sealed-credential","preview-no-mutation","initial-sync","changed-reconciliation","invalid-credential","provider-timeout","safe-failure","recovery","source-schedule","cross-team-denial","secret-redaction"]}')"
if [[ -n "$EVIDENCE_FILE" ]]; then umask 077; printf '%s\n' "$EVIDENCE" >"$EVIDENCE_FILE"; fi
printf '%s\n' "$EVIDENCE"

PHASE="cleanup"
resource_cleanup true || die "dynamic inventory fixture cleanup failed"
PHASE="complete"
trap - ERR
