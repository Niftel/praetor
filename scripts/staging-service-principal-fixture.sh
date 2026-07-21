#!/usr/bin/env bash
set -euo pipefail

CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}"
NAMESPACE="${PRAETOR_STAGING_NAMESPACE:-praetor-staging}"
RELEASE="${PRAETOR_STAGING_RELEASE:-praetor-staging}"
API_PORT="${PRAETOR_DELEGATED_FIXTURE_API_PORT:-18084}"
API="http://127.0.0.1:$API_PORT/api/v1"
ADMIN_USERNAME="${PRAETOR_DELEGATED_FIXTURE_ADMIN_USERNAME:-demo-operator}"
ADMIN_PASSWORD="${PRAETOR_STAGING_ACCEPTANCE_PASSWORD:-praetor123}"
LDAP_PASSWORD="${PRAETOR_STAGING_ACCEPTANCE_PASSWORD:-praetor123}"
ORGANIZATION_NAME="${PRAETOR_DELEGATED_FIXTURE_ORGANIZATION:-Engineering}"
WORKFLOW_NAME="${PRAETOR_DELEGATED_FIXTURE_WORKFLOW:-Pilot Managed Host Workflow}"
INVENTORY_NAME="${PRAETOR_DELEGATED_FIXTURE_INVENTORY:-Pilot Engineering Inventory}"
HOST_NAME="${PRAETOR_DELEGATED_FIXTURE_HOST:-pilot-managed-host}"
APPROVAL_TEAM_NAME="${PRAETOR_DELEGATED_FIXTURE_APPROVAL_TEAM:-backend-team}"
APPROVER_USERNAME="${PRAETOR_DELEGATED_FIXTURE_APPROVER:-mwebb}"
PRINCIPAL_NAME="${PRAETOR_DELEGATED_FIXTURE_PRINCIPAL:-Praetor Delegated Staging Fixture}"
SECRET_NAME="${PRAETOR_DELEGATED_FIXTURE_SECRET:-praetor-delegated-staging-fixture}"
ALLOWED_EXTRA_VAR="${PRAETOR_DELEGATED_FIXTURE_ALLOWED_EXTRA_VAR:-request_id}"
LIFETIME_HOURS="${PRAETOR_DELEGATED_FIXTURE_LIFETIME_HOURS:-24}"
MAX_HOSTS="${PRAETOR_DELEGATED_FIXTURE_MAX_HOSTS:-1}"
TIMEOUT="${PRAETOR_DELEGATED_FIXTURE_TIMEOUT_SECONDS:-240}"
LABEL_KEY="app.kubernetes.io/part-of"
LABEL_VALUE="praetor-delegated-staging-fixture"
PORT_FORWARD_PID=""
PORT_FORWARD_LOG=""

usage() {
  echo "usage: $0 <plan|setup|validate|cleanup|rehearse>" >&2
}
die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for command in curl jq kubectl; do need "$command"; done
kube() { kubectl --context "$CONTEXT" "$@"; }

cleanup_tunnel() {
  [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null || true
  [[ -z "$PORT_FORWARD_LOG" ]] || rm -f "$PORT_FORWARD_LOG"
}
trap cleanup_tunnel EXIT

validate_configuration() {
  [[ "$MAX_HOSTS" =~ ^[1-9][0-9]*$ ]] || die "maximum host count must be positive"
  [[ "$LIFETIME_HOURS" =~ ^[1-9][0-9]*$ ]] || die "credential lifetime must be positive"
  (( LIFETIME_HOURS <= 24 * 30 )) || die "fixture lifetime cannot exceed 30 days"
  [[ "$ALLOWED_EXTRA_VAR" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die "allowed extra variable must be an identifier"
  [[ "$SECRET_NAME" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || die "secret name is not DNS-safe"
}

cluster_ready() {
  kube get --raw=/readyz >/dev/null 2>&1 || die "Kubernetes API is unavailable in context '$CONTEXT'"
  kube get namespace "$NAMESPACE" >/dev/null 2>&1 || die "namespace '$NAMESPACE' is missing"
}

start_tunnel() {
  cleanup_tunnel
  PORT_FORWARD_LOG="$(mktemp "${TMPDIR:-/tmp}/praetor-delegated-fixture.XXXXXX")"
  kube port-forward -n "$NAMESPACE" "svc/$RELEASE-api" "$API_PORT:8080" >"$PORT_FORWARD_LOG" 2>&1 &
  PORT_FORWARD_PID=$!
  for _ in $(seq 1 30); do
    curl -fsS "$API/ping" >/dev/null 2>&1 && return
    kill -0 "$PORT_FORWARD_PID" 2>/dev/null || { cat "$PORT_FORWARD_LOG" >&2; die "API tunnel stopped"; }
    sleep 1
  done
  die "API did not become reachable"
}

login_token() {
  local username="$1" password="$2"
  curl -fsS -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg username "$username" --arg password "$password" '{username:$username,password:$password}')" \
    "$API/auth/login" | jq -er .token
}
api_get() { curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$API/$1"; }
api_write() {
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -fsS -X "$method" -H "Authorization: Bearer $ADMIN_TOKEN" \
      -H 'Content-Type: application/json' -d "$body" "$API/$path"
  else
    curl -fsS -X "$method" -H "Authorization: Bearer $ADMIN_TOKEN" "$API/$path"
  fi
}
unwrap() { jq -c 'if type == "object" and has("items") then .items else . end'; }
find_named_id() {
  local path="$1" name="$2"
  api_get "$path" | unwrap | jq -r --arg name "$name" '.[] | select(.name == $name) | .id' | head -n1
}
require_named_id() {
  local path="$1" name="$2" kind="$3" id
  id="$(find_named_id "$path" "$name")"
  [[ -n "$id" ]] || die "$kind '$name' is missing; create the product validation fixture first"
  printf '%s' "$id"
}

load_resources() {
  ORGANIZATION_ID="$(require_named_id organizations/ "$ORGANIZATION_NAME" organization)"
  WORKFLOW_ID="$(require_named_id workflow-templates "$WORKFLOW_NAME" workflow)"
  INVENTORY_ID="$(require_named_id inventories/ "$INVENTORY_NAME" inventory)"
  APPROVAL_TEAM_ID="$(require_named_id teams/ "$APPROVAL_TEAM_NAME" team)"
  HOST_ID="$(api_get "inventories/$INVENTORY_ID/hosts/" | unwrap | jq -r --arg name "$HOST_NAME" '.[] | select(.name == $name and .enabled == true) | .id' | head -n1)"
  [[ -n "$HOST_ID" ]] || die "enabled host '$HOST_NAME' is missing from inventory '$INVENTORY_NAME'"
}

principal_id() {
  local principals count
  principals="$(api_get "organizations/$ORGANIZATION_ID/service-principals/")"
  count="$(jq --arg name "$PRINCIPAL_NAME" '[.[] | select(.name == $name)] | length' <<<"$principals")"
  (( count <= 1 )) || die "multiple fixture principals exist; refusing an ambiguous cleanup"
  jq -r --arg name "$PRINCIPAL_NAME" '.[] | select(.name == $name) | .id' <<<"$principals"
}

apply_secret_from_credential() {
  local response="$1" principal_id="$2" credential_id="$3"
  jq -c \
    --arg namespace "$NAMESPACE" --arg name "$SECRET_NAME" \
    --arg label_key "$LABEL_KEY" --arg label_value "$LABEL_VALUE" \
    --arg principal_id "$principal_id" --arg credential_id "$credential_id" '
      {
        apiVersion:"v1", kind:"Secret", type:"Opaque",
        metadata:{namespace:$namespace,name:$name,labels:{($label_key):$label_value},annotations:{
          "praetor.io/service-principal-id":$principal_id,
          "praetor.io/service-credential-id":$credential_id
        }},
        data:{token:(.token | @base64)}
      }' <<<"$response" | kube apply -f - >/dev/null
}

setup_fixture() {
  validate_configuration; cluster_ready; start_tunnel
  ADMIN_TOKEN="$(login_token "$ADMIN_USERNAME" "$ADMIN_PASSWORD")"
  load_resources

  local principal response credential_id active_credentials active_id grant_body grants grant_id duplicate_ids
  principal="$(principal_id)"
  if [[ -z "$principal" ]]; then
    principal="$(api_write POST "organizations/$ORGANIZATION_ID/service-principals/" \
      "$(jq -nc --arg name "$PRINCIPAL_NAME" '{name:$name,description:"Synthetic bounded delegated staging fixture"}')" | jq -er .id)"
  else
    api_write PATCH "service-principals/$principal" '{"enabled":true}' >/dev/null
  fi

  active_credentials="$(api_get "service-principals/$principal/credentials" | jq -c '[.[] | select(.revoked_at == null)]')"
  active_id="$(jq -r 'sort_by(.created_at,.id) | last | .id // empty' <<<"$active_credentials")"
  while read -r credential_id; do
    [[ -z "$credential_id" || "$credential_id" == "$active_id" ]] && continue
    api_write DELETE "service-principals/$principal/credentials/$credential_id" >/dev/null
  done < <(jq -r '.[].id' <<<"$active_credentials")

  expires_at="$(jq -nr --argjson hours "$LIFETIME_HOURS" 'now + ($hours * 3600) | todateiso8601')"
  credential_body="$(jq -nc --arg name "$PRINCIPAL_NAME credential" --arg expires "$expires_at" '{name:$name,expires_at:$expires}')"
  if [[ -n "$active_id" ]]; then
    response="$(api_write POST "service-principals/$principal/credentials/$active_id/rotate" "$credential_body")"
  else
    response="$(api_write POST "service-principals/$principal/credentials" "$credential_body")"
  fi
  credential_id="$(jq -er .id <<<"$response")"
  apply_secret_from_credential "$response" "$principal" "$credential_id"
  unset response

  if [[ "${PRAETOR_DELEGATED_FIXTURE_FAIL_AFTER_CREDENTIAL:-false}" == true ]]; then
    die "injected failure after credential storage"
  fi

  not_before="$(jq -nr 'now - 5 | todateiso8601')"
  grant_expires="$(jq -nr --argjson hours "$LIFETIME_HOURS" 'now + ($hours * 3600) | todateiso8601')"
  grant_body="$(jq -nc --argjson workflow "$WORKFLOW_ID" --argjson inventory "$INVENTORY_ID" \
    --argjson host "$HOST_ID" --argjson max "$MAX_HOSTS" --arg extra "$ALLOWED_EXTRA_VAR" \
    --argjson team "$APPROVAL_TEAM_ID" --arg not_before "$not_before" --arg expires "$grant_expires" \
    '{workflow_template_id:$workflow,inventory_id:$inventory,allowed_host_ids:[$host],allowed_group_ids:[],max_hosts:$max,allowed_extra_var_keys:[$extra],approval_team_id:$team,not_before:$not_before,expires_at:$expires}')"
  grants="$(api_get "service-principals/$principal/grants")"
  grant_id="$(jq -r --argjson workflow "$WORKFLOW_ID" --argjson inventory "$INVENTORY_ID" \
    '[.[] | select(.workflow_template_id == $workflow and .inventory_id == $inventory and .revoked_at == null)] | sort_by(.created_at,.id) | last | .id // empty' <<<"$grants")"
  duplicate_ids="$(jq -r --argjson workflow "$WORKFLOW_ID" --argjson inventory "$INVENTORY_ID" --arg keep "$grant_id" \
    '.[] | select(.workflow_template_id == $workflow and .inventory_id == $inventory and .revoked_at == null and (.id|tostring) != $keep) | .id' <<<"$grants")"
  while read -r duplicate; do
    [[ -z "$duplicate" ]] || api_write DELETE "service-principals/$principal/grants/$duplicate" >/dev/null
  done <<<"$duplicate_ids"
  if [[ -n "$grant_id" ]]; then
    api_write PUT "service-principals/$principal/grants/$grant_id" "$grant_body" >/dev/null
  else
    grant_id="$(api_write POST "service-principals/$principal/grants" "$grant_body" | jq -er .id)"
  fi

  echo "delegated staging fixture ready: principal=$principal grant=$grant_id secret=$NAMESPACE/$SECRET_NAME"
}

validate_fixture() {
  validate_configuration; cluster_ready; start_tunnel
  ADMIN_TOKEN="$(login_token "$ADMIN_USERNAME" "$ADMIN_PASSWORD")"
  APPROVER_TOKEN="$(login_token "$APPROVER_USERNAME" "$LDAP_PASSWORD")"
  load_resources
  local principal active_credentials active_grants service_token idempotency_key request response workflow_job_id replay approval_id approvals status
  principal="$(principal_id)"; [[ -n "$principal" ]] || die "fixture principal is missing"
  active_credentials="$(api_get "service-principals/$principal/credentials" | jq '[.[] | select(.revoked_at == null)] | length')"
  active_grants="$(api_get "service-principals/$principal/grants" | jq --argjson workflow "$WORKFLOW_ID" --argjson inventory "$INVENTORY_ID" '[.[] | select(.workflow_template_id == $workflow and .inventory_id == $inventory and .revoked_at == null)] | length')"
  [[ "$active_credentials" == 1 ]] || die "fixture has $active_credentials active credentials, expected 1"
  [[ "$active_grants" == 1 ]] || die "fixture has $active_grants active grants, expected 1"
  service_token="$(kube get secret "$SECRET_NAME" -n "$NAMESPACE" -o json | jq -er '.data.token | @base64d')"
  idempotency_key="fixture-$(date -u +%Y%m%d%H%M%S)-$$"
  request="$(jq -nc --arg requester "staging-fixture" --argjson inventory "$INVENTORY_ID" --argjson host "$HOST_ID" --arg key "$ALLOWED_EXTRA_VAR" \
    '{external_requester:$requester,inventory_id:$inventory,host_ids:[$host],extra_vars:{($key):"synthetic"}}')"
  response="$(curl -fsS -H "Authorization: Bearer $service_token" -H "Idempotency-Key: $idempotency_key" -H 'Content-Type: application/json' \
    -d "$request" "$API/delegated/workflow-templates/$WORKFLOW_ID/launch")"
  workflow_job_id="$(jq -er 'select(.replayed == false) | .workflow_job_id' <<<"$response")"
  replay="$(curl -fsS -H "Authorization: Bearer $service_token" -H "Idempotency-Key: $idempotency_key" -H 'Content-Type: application/json' \
    -d "$request" "$API/delegated/workflow-templates/$WORKFLOW_ID/launch")"
  jq -e --argjson job "$workflow_job_id" '.replayed == true and .workflow_job_id == $job' <<<"$replay" >/dev/null || die "idempotent replay did not return the original workflow"
  unset service_token response replay

  approval_id=""
  for _ in $(seq 1 60); do
    approvals="$(curl -fsS -H "Authorization: Bearer $APPROVER_TOKEN" "$API/workflow-approvals")"
    approval_id="$(jq -r --argjson job "$workflow_job_id" --argjson team "$APPROVAL_TEAM_ID" '.[] | select(.workflow_job_id == $job and .approval_team_id == $team) | .id' <<<"$approvals" | head -n1)"
    [[ -n "$approval_id" ]] && break
    sleep 1
  done
  [[ -n "$approval_id" ]] || die "approval was not routed to '$APPROVAL_TEAM_NAME'"
  curl -fsS -o /dev/null -X POST -H "Authorization: Bearer $APPROVER_TOKEN" "$API/workflow-job-nodes/$approval_id/approve"
  status=""
  for _ in $(seq 1 "$TIMEOUT"); do
    status="$(api_get "workflow-jobs/$workflow_job_id" | jq -r .status)"
    [[ "$status" =~ ^(successful|failed|error|canceled)$ ]] && break
    sleep 1
  done
  [[ "$status" == successful ]] || die "delegated workflow $workflow_job_id finished with status '${status:-unknown}'"
  echo "delegated staging fixture validated: workflow_job=$workflow_job_id replayed=true approval_team=$APPROVAL_TEAM_NAME status=$status"
}

cleanup_fixture() {
  validate_configuration; cluster_ready
  if kube get secret "$SECRET_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
    [[ "$(kube get secret "$SECRET_NAME" -n "$NAMESPACE" -o json | jq -r --arg key "$LABEL_KEY" '.metadata.labels[$key] // ""')" == "$LABEL_VALUE" ]] || \
      die "refusing to delete unlabelled Secret $NAMESPACE/$SECRET_NAME"
    kube delete secret "$SECRET_NAME" -n "$NAMESPACE" >/dev/null
  fi
  start_tunnel
  ADMIN_TOKEN="$(login_token "$ADMIN_USERNAME" "$ADMIN_PASSWORD")"
  ORGANIZATION_ID="$(find_named_id organizations/ "$ORGANIZATION_NAME")"
  [[ -n "$ORGANIZATION_ID" ]] || { echo "delegated staging fixture already absent"; return; }
  local principal
  principal="$(principal_id)"
  [[ -n "$principal" ]] || { echo "delegated staging fixture already absent"; return; }
  while read -r id; do [[ -z "$id" ]] || api_write DELETE "service-principals/$principal/grants/$id" >/dev/null; done \
    < <(api_get "service-principals/$principal/grants" | jq -r '.[] | select(.revoked_at == null) | .id')
  while read -r id; do [[ -z "$id" ]] || api_write DELETE "service-principals/$principal/credentials/$id" >/dev/null; done \
    < <(api_get "service-principals/$principal/credentials" | jq -r '.[] | select(.revoked_at == null) | .id')
  api_write DELETE "service-principals/$principal" >/dev/null
  echo "delegated staging fixture revoked: principal=$principal secret=$NAMESPACE/$SECRET_NAME"
}

plan_fixture() {
  validate_configuration
  cat <<EOF
Delegated staging fixture plan
  context:         $CONTEXT
  namespace:       $NAMESPACE
  organization:    $ORGANIZATION_NAME
  workflow:        $WORKFLOW_NAME
  inventory/host:  $INVENTORY_NAME / $HOST_NAME
  approval team:   $APPROVAL_TEAM_NAME
  principal:       $PRINCIPAL_NAME
  secret:          $SECRET_NAME
  maximum hosts:   $MAX_HOSTS
  variable allow:  $ALLOWED_EXTRA_VAR
  expiry:          ${LIFETIME_HOURS}h
EOF
}

rehearse_fixture() {
  cleanup_fixture
  if (export PRAETOR_DELEGATED_FIXTURE_FAIL_AFTER_CREDENTIAL=true; setup_fixture); then
    die "failure injection unexpectedly succeeded"
  fi
  setup_fixture
  setup_fixture
  validate_fixture
  cleanup_fixture
}

case "${1:-}" in
  plan) plan_fixture ;;
  setup) setup_fixture ;;
  validate) validate_fixture ;;
  cleanup) cleanup_fixture ;;
  rehearse) rehearse_fixture ;;
  *) usage; exit 2 ;;
esac
