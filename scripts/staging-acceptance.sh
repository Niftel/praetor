#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMMAND="${1:-}"
CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}"
NAMESPACE="${PRAETOR_STAGING_NAMESPACE:-praetor-staging}"
RELEASE="${PRAETOR_STAGING_RELEASE:-praetor-staging}"
API_PORT="${PRAETOR_STAGING_ACCEPTANCE_PORT:-18082}"
API="http://127.0.0.1:$API_PORT/api/v1"
PASSWORD="${PRAETOR_STAGING_ACCEPTANCE_PASSWORD:-praetor123}"
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-$HOME/.local/share/praetor/staging}"
EVIDENCE_ROOT="$DATA_ROOT/acceptance/evidence"
MANIFEST="$ROOT/deployments/staging/acceptance.yaml"
PREFIX="Praetor Validation"
PORT_FORWARD_PID=""
PORT_FORWARD_LOG=""
DB_FORWARD_PID=""
DB_FORWARD_LOG=""
DB_CREATED=false

usage() { echo "usage: $0 <plan|seed|status|run>" >&2; exit 2; }
die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for tool in curl go jq kubectl nc; do need "$tool"; done
[[ "$COMMAND" =~ ^(plan|seed|status|run)$ ]] || usage
umask 077

cleanup() {
  [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null || true
  [[ -z "$PORT_FORWARD_LOG" ]] || rm -f "$PORT_FORWARD_LOG"
  [[ -z "$DB_FORWARD_PID" ]] || kill "$DB_FORWARD_PID" 2>/dev/null || true
  [[ -z "$DB_FORWARD_LOG" ]] || rm -f "$DB_FORWARD_LOG"
  if [[ "$DB_CREATED" == true ]]; then
    kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pod/praetor-staging-delegated-db service/praetor-staging-delegated-db --ignore-not-found --wait=false >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

start_tunnel() {
  PORT_FORWARD_LOG="$(mktemp "${TMPDIR:-/tmp}/praetor-staging-acceptance.XXXXXX")"
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
get() { curl -fsS -H "Authorization: Bearer $TOKEN" "$API/$1"; }
post() { curl -fsS -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$2" "$API/$1"; }
post_status() {
  local output
  output="$(mktemp "${TMPDIR:-/tmp}/praetor-staging-acceptance-response.XXXXXX")"
  STATUS="$(curl -sS -o "$output" -w '%{http_code}' -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$2" "$API/$1")"
  RESPONSE="$(cat "$output")"; rm -f "$output"
}
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
  existing="$(get "access?content_type=$content_type&object_id=$object_id" 2>/dev/null || true)"
  if ! jq -e --argjson role "$role_id" --argjson team "$team_id" '.[]? | select(.role_definition_id == $role and .team_id == $team)' <<<"$existing" >/dev/null 2>&1; then
    post access "$(jq -nc --arg type "$content_type" --argjson object "$object_id" --argjson role "$role_id" --argjson team "$team_id" '{content_type:$type,object_id:$object,role_definition_id:$role,team_id:$team}')" >/dev/null
  fi
}

plan() {
  cat <<EOF
Praetor staging acceptance plan
  identity mappings: demo-operator, mwebb, fwalsh, demo-auditor through verified LDAPS
  object boundary:   Engineering/backend-team owns synthetic inventory, host, and workflow access
  negative checks:   frontend-team visibility/approval and requester self-approval are denied
  notifications:     digest-pinned staging-only sink; approval delivery asserted once
  delegated API:     complete fail-closed scope suite with no skipped cases
  evidence:          sanitized mode-0600 JSON below $EVIDENCE_ROOT
EOF
}

seed() {
  "$ROOT/scripts/staging-integrations.sh" status >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" apply -f "$MANIFEST" >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status deployment/praetor-staging-acceptance-sink --timeout=180s >/dev/null
  start_tunnel
  for username in demo-operator mwebb fwalsh demo-auditor; do login "$username" >/dev/null; done
  TOKEN="$(login demo-operator | jq -er .token)"
  org_id="$(find_id organizations Engineering)"; [[ -n "$org_id" ]] || die "Engineering mapping is missing"
  team_id="$(find_id teams backend-team)"; [[ -n "$team_id" ]] || die "backend-team mapping is missing"
  inventory_id="$(ensure inventories inventories "$PREFIX Inventory" "$(jq -nc --argjson org "$org_id" --arg name "$PREFIX Inventory" '{organization_id:$org,name:$name,kind:"static"}')")"
  host_id="$(find_id "inventories/$inventory_id/hosts/" "$PREFIX Host")"
  [[ -n "$host_id" ]] || host_id="$(post "inventories/$inventory_id/hosts/" "$(jq -nc --arg name "$PREFIX Host" '{name:$name,enabled:true,variables:{ansible_connection:"local"}}')" | jq -er .id)"
  project_id="$(ensure projects projects "$PREFIX Project" "$(jq -nc --argjson org "$org_id" --arg name "$PREFIX Project" '{organization_id:$org,name:$name,scm_type:"git",scm_url:"https://github.com/Niftel/praetor.git"}')")"
  job_id="$(ensure job-templates job-templates "$PREFIX Job" "$(jq -nc --argjson org "$org_id" --argjson inv "$inventory_id" --argjson project "$project_id" --arg name "$PREFIX Job" '{organization_id:$org,inventory_id:$inv,project_id:$project,name:$name,playbook:"playbooks/ping.yml",job_type:"run",forks:1}')")"
  workflow_id="$(ensure workflow-templates workflow-templates "$PREFIX LDAP Workflow" "$(jq -nc --argjson org "$org_id" --arg name "$PREFIX LDAP Workflow" '{organization_id:$org,name:$name,nodes:[{node_key:"approval",node_type:"approval",name:"Team approval"}],edges:[]}' )")"
  grant_team_role inventory "$inventory_id" "Inventory Use" "$team_id"
  grant_team_role workflow_template "$workflow_id" "Workflow Template Execute" "$team_id"
  grant_team_role workflow_template "$workflow_id" "Workflow Template Approve" "$team_id"
  notification_id="$(ensure notification-templates "notification-templates?organization_id=$org_id" "$PREFIX Notifications" "$(jq -nc --argjson org "$org_id" --arg name "$PREFIX Notifications" '{organization_id:$org,name:$name,notification_type:"webhook",config:{url:"http://praetor-staging-acceptance-sink:8080/echo"}}')")"
  attachments="$(get "workflow-templates/$workflow_id/notifications")"
  if ! jq -e --argjson id "$notification_id" '.[] | select(.notification_template_id == $id and .event == "approval")' <<<"$attachments" >/dev/null; then
    post_status "workflow-templates/$workflow_id/notifications" "$(jq -nc --argjson id "$notification_id" '{notification_template_id:$id,event:"approval"}')"
    [[ "$STATUS" == 204 ]] || die "could not attach approval notification: HTTP $STATUS"
  fi
  echo "seeded synthetic staging acceptance resources in Engineering (inventory $inventory_id, host $host_id, job $job_id, workflow $workflow_id)"
}

status_acceptance() {
  "$ROOT/scripts/staging-release.sh" status >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status deployment/praetor-staging-acceptance-sink --timeout=30s >/dev/null || die "acceptance notification sink is unavailable"
  echo "healthy: persistent staging release and acceptance sink are ready"
}

run_acceptance() {
  status_acceptance
  install -d -m 0700 "$EVIDENCE_ROOT"
  ldap_evidence="$EVIDENCE_ROOT/ldap-operator.json"
  PRAETOR_VALIDATION_NAMESPACE="$NAMESPACE" PRAETOR_HELM_RELEASE="$RELEASE" \
    PRAETOR_VALIDATION_API_PORT="$API_PORT" PRAETOR_VALIDATION_LDAP_PASSWORD="$PASSWORD" \
    PRAETOR_LDAP_EVIDENCE_FILE="$ldap_evidence" "$ROOT/scripts/validate-ldap-operator-journey.sh" >/dev/null
  chmod 0600 "$ldap_evidence"
  delivery_count="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" logs deployment/praetor-staging-acceptance-sink --since=10m | jq -Rsc --argjson job "$(jq -r .workflow_job_id "$ldap_evidence")" '[split("\n")[] | fromjson? | select(.job_id == $job and .event == "approval")] | length')"
  [[ "$delivery_count" == 1 ]] || die "approval notification was delivered $delivery_count times, expected exactly 1"

  kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pod praetor-staging-delegated-db --ignore-not-found --wait=true >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" delete service praetor-staging-delegated-db --ignore-not-found >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" run praetor-staging-delegated-db \
    --image=postgres:15@sha256:74e110c41804365e3915fcc09d5e7a1eff50161aaa94d5da0e58e0cd75ae509c \
    --labels=app.kubernetes.io/part-of=praetor-staging-acceptance \
    --env=POSTGRES_PASSWORD=postgres --env=POSTGRES_DB=praetor --port=5432 >/dev/null
  DB_CREATED=true
  kubectl --context "$CONTEXT" -n "$NAMESPACE" expose pod praetor-staging-delegated-db --port=5432 >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" wait --for=condition=Ready pod/praetor-staging-delegated-db --timeout=180s >/dev/null
  DB_FORWARD_LOG="$(mktemp "${TMPDIR:-/tmp}/praetor-staging-delegated-db.XXXXXX")"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" port-forward pod/praetor-staging-delegated-db 55436:5432 >"$DB_FORWARD_LOG" 2>&1 & DB_FORWARD_PID=$!
  for _ in $(seq 1 30); do nc -z 127.0.0.1 55436 >/dev/null 2>&1 && break; sleep 1; done
  DATABASE_URL='postgres://postgres:postgres@127.0.0.1:55436/praetor?sslmode=disable' go run "$ROOT/cmd/migrator" >/dev/null
  TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:55436/praetor?sslmode=disable' \
    PRAETOR_DELEGATED_EVIDENCE_FILE="$EVIDENCE_ROOT/delegated-api.json" "$ROOT/scripts/validate-delegated-api-e2e.sh" >/dev/null
  kill "$DB_FORWARD_PID" 2>/dev/null || true; DB_FORWARD_PID=""
  rm -f "$DB_FORWARD_LOG"; DB_FORWARD_LOG=""
  kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pod praetor-staging-delegated-db --wait=true >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" delete service praetor-staging-delegated-db >/dev/null
  DB_CREATED=false
  chmod 0600 "$EVIDENCE_ROOT/delegated-api.json"
  jq -n --arg recorded_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --argjson notification_deliveries "$delivery_count" \
    '{schema_version:1,journey:"staging-acceptance",result:"pass",recorded_at:$recorded_at,checks:["ldap-login","organization-team-mapping","inventory-host-scope","team-approval-isolation","requester-self-approval-denial","auditor-attribution","delegated-api-scope","notification-delivery"],notification_deliveries:$notification_deliveries}' \
    >"$EVIDENCE_ROOT/staging-acceptance.json"
  chmod 0600 "$EVIDENCE_ROOT/staging-acceptance.json"
  echo "scripted staging acceptance passed; sanitized evidence: $EVIDENCE_ROOT"
}

case "$COMMAND" in
  plan) plan ;;
  seed) seed ;;
  status) status_acceptance ;;
  run) run_acceptance ;;
esac
