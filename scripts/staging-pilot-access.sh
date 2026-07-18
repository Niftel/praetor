#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMMAND="${1:-}"
CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}"
NAMESPACE="${PRAETOR_STAGING_NAMESPACE:-praetor-staging}"
RELEASE="${PRAETOR_STAGING_RELEASE:-praetor-staging}"
API_PORT="${PRAETOR_PILOT_ACCESS_PORT:-18083}"
API="http://127.0.0.1:$API_PORT/api/v1"
PASSWORD="${PRAETOR_STAGING_ACCEPTANCE_PASSWORD:-praetor123}"
PILOT_ROOT="${PRAETOR_PILOT_DATA_ROOT:-$HOME/.local/share/praetor/pilot-host}"
PRIVATE_KEY="$PILOT_ROOT/ssh/id_ed25519"
INVENTORY_NAME="Pilot Engineering Inventory"
HOST_NAME="Pilot Managed Host"
CREDENTIAL_NAME="Pilot SSH Credential"
TARGET_ADDRESS="${PRAETOR_PILOT_ADDRESS:-172.29.50.10}"
PORT_FORWARD_PID=""
PORT_FORWARD_LOG=""
SECRET_REQUEST=""
TOKEN=""

usage() { echo "usage: $0 <plan|seed|status>" >&2; exit 2; }
die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for tool in curl docker jq kubectl; do need "$tool"; done
[[ "$COMMAND" =~ ^(plan|seed|status)$ ]] || usage
umask 077

cleanup() {
  [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null || true
  [[ -z "$PORT_FORWARD_LOG" ]] || rm -f "$PORT_FORWARD_LOG"
  [[ -z "$SECRET_REQUEST" ]] || rm -f "$SECRET_REQUEST"
}
trap cleanup EXIT

start_tunnel() {
  PORT_FORWARD_LOG="$(mktemp "${TMPDIR:-/tmp}/praetor-pilot-access.XXXXXX")"
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
  curl -fsS -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg username "$1" --arg password "$PASSWORD" '{username:$username,password:$password}')" \
    "$API/auth/login"
}
get_as() { curl -fsS -H "Authorization: Bearer $1" "$API/$2"; }
get() { get_as "$TOKEN" "$1"; }
post() { curl -fsS -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$2" "$API/$1"; }
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
  if ! jq -e --argjson role "$role_id" --argjson team "$team_id" '.[]? | select(.role_definition_id == $role and .team_id == $team)' <<<"$existing" >/dev/null; then
    post access "$(jq -nc --arg type "$content_type" --argjson object "$object_id" --argjson role "$role_id" --argjson team "$team_id" '{content_type:$type,object_id:$object,role_definition_id:$role,team_id:$team}')" >/dev/null
  fi
}

lookup() {
  ORG_ID="$(find_id organizations Engineering)"; [[ -n "$ORG_ID" ]] || die "Engineering organization is missing"
  TEAM_ID="$(find_id teams backend-team)"; [[ -n "$TEAM_ID" ]] || die "backend-team is missing"
  INVENTORY_ID="$(find_id inventories "$INVENTORY_NAME")"; [[ -n "$INVENTORY_ID" ]] || die "$INVENTORY_NAME is missing; run seed"
  CREDENTIAL_ID="$(find_id credentials "$CREDENTIAL_NAME")"; [[ -n "$CREDENTIAL_ID" ]] || die "$CREDENTIAL_NAME is missing; run seed"
}

plan() {
  cat <<EOF
Pilot credential and inventory plan
  organization/team: Engineering / backend-team
  inventory/host:    $INVENTORY_NAME / $HOST_NAME ($TARGET_ADDRESS)
  credential:        $CREDENTIAL_NAME; private key submitted only to the API -> Secrets Service write path
  Praetor storage:   masked inputs plus opaque Secrets UUID/version only
  deny boundary:     frontend-team, auditors, users, and service principals receive no use assignment
  verification:      sealed API, DB plaintext scan, role matrix, audit event, and idempotent counts
EOF
}

seed() {
  "$ROOT/scripts/pilot-host.sh" status >/dev/null
  "$ROOT/scripts/staging-integrations.sh" status >/dev/null
  [[ -s "$PRIVATE_KEY" ]] || die "pilot private key is missing; run make pilot-host-provision"
  start_tunnel
  for username in demo-operator fwalsh demo-auditor; do login "$username" >/dev/null; done
  TOKEN="$(login demo-operator | jq -er .token)"
  ORG_ID="$(find_id organizations Engineering)"; [[ -n "$ORG_ID" ]] || die "Engineering organization is missing"
  TEAM_ID="$(find_id teams backend-team)"; [[ -n "$TEAM_ID" ]] || die "backend-team is missing"
  INVENTORY_ID="$(ensure inventories inventories "$INVENTORY_NAME" "$(jq -nc --argjson org "$ORG_ID" --arg name "$INVENTORY_NAME" '{organization_id:$org,name:$name,kind:"static"}')")"
  HOST_ID="$(find_id "inventories/$INVENTORY_ID/hosts/" "$HOST_NAME")"
  [[ -n "$HOST_ID" ]] || HOST_ID="$(post "inventories/$INVENTORY_ID/hosts/" "$(jq -nc --arg name "$HOST_NAME" --arg address "$TARGET_ADDRESS" '{name:$name,enabled:true,variables:{ansible_host:$address,ansible_user:"praetor",ansible_port:22}}')" | jq -er .id)"
  CREDENTIAL_ID="$(find_id credentials "$CREDENTIAL_NAME")"
  if [[ -z "$CREDENTIAL_ID" ]]; then
    SECRET_REQUEST="$(mktemp "${TMPDIR:-/tmp}/praetor-pilot-credential.XXXXXX")"
    jq -nc --argjson organization_id "$ORG_ID" --arg name "$CREDENTIAL_NAME" --rawfile key "$PRIVATE_KEY" \
      '{organization_id:$organization_id,credential_type_id:1,name:$name,description:"Disposable pilot managed-host identity",inputs:{username:"praetor",ssh_private_key:$key}}' >"$SECRET_REQUEST"
    response="$(curl -fsS -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' --data-binary "@$SECRET_REQUEST" "$API/credentials")"
    rm -f "$SECRET_REQUEST"; SECRET_REQUEST=""
    CREDENTIAL_ID="$(jq -er .id <<<"$response")"
    [[ "$(jq -r '.inputs.ssh_private_key' <<<"$response")" == '$encrypted$' ]] || die "credential response was not sealed"
  fi
  grant_team_role inventory "$INVENTORY_ID" "Inventory Use" "$TEAM_ID"
  grant_team_role credential "$CREDENTIAL_ID" "Credential Use" "$TEAM_ID"
  echo "seeded pilot inventory $INVENTORY_ID, host $HOST_ID, and sealed credential $CREDENTIAL_ID for backend-team"
}

status_check() {
  "$ROOT/scripts/pilot-host.sh" status >/dev/null
  "$ROOT/scripts/staging-integrations.sh" status >/dev/null
  start_tunnel
  TOKEN="$(login demo-operator | jq -er .token)"
  lookup
  credential="$(get "credentials/$CREDENTIAL_ID")"
  [[ "$(jq -r '.inputs.ssh_private_key' <<<"$credential")" == '$encrypted$' ]] || die "credential API exposed an unsealed private key"
  backend_token="$TOKEN"
  frontend_token="$(login fwalsh | jq -er .token)"
  auditor_token="$(login demo-auditor | jq -er .token)"
  get_as "$backend_token" inventories | items | jq -e --argjson id "$INVENTORY_ID" 'any(.[]; .id == $id)' >/dev/null || die "backend-team cannot see pilot inventory"
  get_as "$backend_token" credentials | items | jq -e --argjson id "$CREDENTIAL_ID" 'any(.[]; .id == $id)' >/dev/null || die "backend-team cannot see pilot credential"
  get_as "$frontend_token" inventories | items | jq -e --argjson id "$INVENTORY_ID" 'all(.[]; .id != $id)' >/dev/null || die "frontend-team can see pilot inventory"
  get_as "$frontend_token" credentials | items | jq -e --argjson id "$CREDENTIAL_ID" 'all(.[]; .id != $id)' >/dev/null || die "frontend-team can see pilot credential"
  auditor_credential="$(get_as "$auditor_token" "credentials/$CREDENTIAL_ID")"
  [[ "$(jq -r '.inputs.ssh_private_key' <<<"$auditor_credential")" == '$encrypted$' ]] || die "auditor API response was not sealed"

  access="$(get "access?content_type=credential&object_id=$CREDENTIAL_ID")"
  use_role="$(get 'role-definitions?content_type=credential' | jq -r '.[] | select(.name == "Credential Use") | .id')"
  [[ "$(jq --argjson role "$use_role" --argjson team "$TEAM_ID" '[.[] | select(.role_definition_id == $role and (.teams | any(.id == $team)))] | length' <<<"$access")" == 1 ]] || die "backend-team Credential Use assignment is missing or duplicated"
  jq -e --argjson role "$use_role" --argjson team "$TEAM_ID" 'all(.[]; .role_definition_id != $role or ((.users | length) == 0 and (.teams | length) == 1 and .teams[0].id == $team))' <<<"$access" >/dev/null || die "credential use is assigned outside backend-team"

  db_pod="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" get pods -l "app.kubernetes.io/instance=$RELEASE,app.kubernetes.io/component=postgresql" -o jsonpath='{.items[0].metadata.name}')"
  row="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$db_pod" -- psql -U postgres -d praetor -At -F '|' -c "select inputs::text,secrets_service_id::text,secrets_service_version from credentials where id=$CREDENTIAL_ID")"
  [[ "$row" == *'$encrypted$'* && "$row" != *'PRIVATE KEY'* ]] || die "Praetor database credential row is not plaintext-free"
  service_id="$(cut -d'|' -f2 <<<"$row")"; service_version="$(cut -d'|' -f3 <<<"$row")"
  [[ -n "$service_id" && "$service_version" =~ ^[1-9][0-9]*$ ]] || die "opaque Secrets reference/version is missing"
  audit_count="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" exec praetor-staging-audit-postgres-0 -- psql -U postgres -d praetor_audit -Atc "select count(*) from remote_audit_records where event->>'operation'='credential_created' and event->>'credential_id'='$service_id' and event->>'organization_id'='$ORG_ID'")"
  [[ "$audit_count" -ge 1 ]] || die "credential_created audit event is missing"
  [[ "$(get credentials | items | jq --arg name "$CREDENTIAL_NAME" '[.[] | select(.name == $name)] | length')" == 1 ]] || die "pilot credential is duplicated"
  [[ "$(get inventories | items | jq --arg name "$INVENTORY_NAME" '[.[] | select(.name == $name)] | length')" == 1 ]] || die "pilot inventory is duplicated"
  echo "healthy: pilot credential is sealed, referenced by UUID/version, audited, idempotent, and usable only by backend-team"
}

case "$COMMAND" in
  plan) plan ;;
  seed) seed ;;
  status) status_check ;;
esac
