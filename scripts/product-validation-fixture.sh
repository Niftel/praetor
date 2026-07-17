#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAMESPACE="${PRAETOR_VALIDATION_NAMESPACE:-praetor-secrets}"
RELEASE="${PRAETOR_HELM_RELEASE:-praetor}"
CHART="$ROOT/deployments/helm/praetor-v2"
MANIFEST="$ROOT/deployments/product-validation/fixture.yaml"
LABEL="app.kubernetes.io/part-of=praetor-validation-fixture"

usage() { echo "usage: $0 <create|status|cleanup>"; }
die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for command in curl docker helm jq kubectl; do need "$command"; done

FIXTURE_PREFIX="Praetor Validation"
API_PORT="${PRAETOR_VALIDATION_API_PORT:-18081}"
API="http://127.0.0.1:$API_PORT/api/v1"
PORT_FORWARD_PID=""
PORT_FORWARD_LOG=""

stop_api_tunnel() {
  [[ -z "$PORT_FORWARD_PID" ]] || kill "$PORT_FORWARD_PID" 2>/dev/null || true
  [[ -z "$PORT_FORWARD_LOG" ]] || rm -f "$PORT_FORWARD_LOG"
  PORT_FORWARD_PID=""; PORT_FORWARD_LOG=""
}

start_api_tunnel() {
  PORT_FORWARD_LOG="$(mktemp "${TMPDIR:-/tmp}/praetor-validation-port-forward.XXXXXX")"
  kubectl port-forward -n "$NAMESPACE" "svc/$RELEASE-api" "$API_PORT:8080" >"$PORT_FORWARD_LOG" 2>&1 &
  PORT_FORWARD_PID=$!
  for _ in $(seq 1 30); do
    curl -fsS "$API/ping" >/dev/null 2>&1 && return
    kill -0 "$PORT_FORWARD_PID" 2>/dev/null || { cat "$PORT_FORWARD_LOG" >&2; die "API tunnel stopped"; }
    sleep 1
  done
  die "API did not become reachable"
}

login() {
  curl -fsS -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg username "$1" --arg password "$2" '{username:$username,password:$password}')" \
    "$API/auth/login" | jq -er .token
}

api_get() { curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$API/$1"; }
api_post() {
  curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' -d "$2" "$API/$1"
}
grant_team_role() {
  local content_type="$1" object_id="$2" role_name="$3" team_id="$4" role_id
  role_id="$(api_get "role-definitions?content_type=$content_type" | jq -er --arg name "$role_name" '.[] | select(.name == $name) | .id' | head -n1)"
  api_post access "$(jq -nc --arg type "$content_type" --argjson object "$object_id" --argjson role "$role_id" --argjson team "$team_id" '{content_type:$type,object_id:$object,role_definition_id:$role,team_id:$team}')" >/dev/null
}
find_named_id() {
  jq -r --arg name "$2" '(if type == "object" and has("items") then .items else . end)[] | select(.name == $name) | .id' <<<"$(api_get "$1")" | head -n1
}
ensure_named() {
  local path="$1" list_path="$2" name="$3" body="$4" id
  id="$(find_named_id "$list_path" "$name")"
  if [[ -z "$id" ]]; then id="$(api_post "$path" "$body" | jq -er .id)"; fi
  printf '%s' "$id"
}

seed_api_resources() {
  start_api_tunnel
  trap stop_api_tunnel RETURN
  # Login-time mapping creates/refreshes Engineering and backend-team first.
  login demo-operator praetor123 >/dev/null
  ADMIN_TOKEN="$(login "${PRAETOR_VALIDATION_ADMIN_USERNAME:-admin}" "${PRAETOR_VALIDATION_ADMIN_PASSWORD:-admin}")"
  local org_id inventory_id host_id project_id template_id workflow_id ldap_workflow_id team_id
  org_id="$(find_named_id organizations/ Engineering)"; [[ -n "$org_id" ]] || die "LDAP mapping did not create Engineering"
  team_id="$(find_named_id teams/ backend-team)"; [[ -n "$team_id" ]] || die "LDAP mapping did not create backend-team"
  inventory_id="$(ensure_named inventories inventories/ "$FIXTURE_PREFIX Inventory" "$(jq -nc --argjson org "$org_id" --arg name "$FIXTURE_PREFIX Inventory" '{organization_id:$org,name:$name,kind:"static"}')")"
  host_id="$(find_named_id "inventories/$inventory_id/hosts/" "$FIXTURE_PREFIX Host")"
  [[ -n "$host_id" ]] || host_id="$(api_post "inventories/$inventory_id/hosts/" "$(jq -nc --arg name "$FIXTURE_PREFIX Host" '{name:$name,enabled:true,variables:{ansible_connection:"local"}}')" | jq -er .id)"
  project_id="$(ensure_named projects projects "$FIXTURE_PREFIX Project" "$(jq -nc --argjson org "$org_id" --arg name "$FIXTURE_PREFIX Project" '{organization_id:$org,name:$name,scm_type:"git",scm_url:"https://github.com/Niftel/praetor.git"}')")"
  template_id="$(ensure_named job-templates job-templates/ "$FIXTURE_PREFIX Job" "$(jq -nc --argjson org "$org_id" --argjson inv "$inventory_id" --argjson project "$project_id" --arg name "$FIXTURE_PREFIX Job" '{organization_id:$org,inventory_id:$inv,project_id:$project,name:$name,playbook:"playbooks/ping.yml",job_type:"run",forks:1}')")"
  workflow_id="$(ensure_named workflow-templates workflow-templates "$FIXTURE_PREFIX Workflow" "$(jq -nc --argjson org "$org_id" --argjson jt "$template_id" --arg name "$FIXTURE_PREFIX Workflow" '{organization_id:$org,name:$name,nodes:[{node_key:"approval",node_type:"approval",name:"Team approval"},{node_key:"execute",node_type:"job",job_template_id:$jt,name:"Run validation"}],edges:[{parent_key:"approval",child_key:"execute",edge_type:"success"}]}')")"
  ldap_workflow_id="$(ensure_named workflow-templates workflow-templates "$FIXTURE_PREFIX LDAP Workflow" "$(jq -nc --argjson org "$org_id" --arg name "$FIXTURE_PREFIX LDAP Workflow" '{organization_id:$org,name:$name,nodes:[{node_key:"approval",node_type:"approval",name:"Team approval"}],edges:[]}')")"
  # The backend team is the synthetic operator boundary: members may use the
  # inventory, launch the workflow, and decide its approval gate. The API still
  # forbids the requester from deciding their own request.
  grant_team_role inventory "$inventory_id" "Inventory Use" "$team_id"
  grant_team_role workflow_template "$ldap_workflow_id" "Workflow Template Execute" "$team_id"
  grant_team_role workflow_template "$ldap_workflow_id" "Workflow Template Approve" "$team_id"
  ensure_named notification-templates "notification-templates?organization_id=$org_id" "$FIXTURE_PREFIX Notifications" "$(jq -nc --argjson org "$org_id" --arg name "$FIXTURE_PREFIX Notifications" '{organization_id:$org,name:$name,notification_type:"webhook",config:{url:"http://praetor-validation-notification-sink:8080/echo"}}')" >/dev/null
  ensure_named "organizations/$org_id/service-principals/" "organizations/$org_id/service-principals/" "$FIXTURE_PREFIX API" "$(jq -nc --arg name "$FIXTURE_PREFIX API" '{name:$name,description:"Synthetic delegated validation principal"}')" >/dev/null
  jq -n --argjson organization_id "$org_id" --argjson team_id "$team_id" --argjson inventory_id "$inventory_id" --argjson host_id "$host_id" --argjson project_id "$project_id" --argjson job_template_id "$template_id" --argjson workflow_id "$workflow_id" --argjson ldap_workflow_id "$ldap_workflow_id" '{organization_id:$organization_id,team_id:$team_id,inventory_id:$inventory_id,host_id:$host_id,project_id:$project_id,job_template_id:$job_template_id,workflow_id:$workflow_id,ldap_workflow_id:$ldap_workflow_id}'
  stop_api_tunnel
  trap - RETURN
}

cluster_ready() {
  docker info >/dev/null 2>&1 || die "Docker daemon is unavailable"
  kubectl get --raw=/readyz >/dev/null 2>&1 || die "Kubernetes API is unavailable; run make local-cluster-start"
}

status_fixture() {
  cluster_ready
  local failed=0 target
  for target in "deployment/$RELEASE-api" deployment/praetor-secrets deployment/praetor-audit-sink deployment/praetor-validation-ldap deployment/praetor-validation-notification-sink; do
    if ! kubectl get "$target" -n "$NAMESPACE" >/dev/null 2>&1; then
      echo "unhealthy: $target is missing" >&2; failed=1
    elif ! kubectl wait --for=condition=available "$target" -n "$NAMESPACE" --timeout=1s >/dev/null 2>&1; then
      echo "unhealthy: $target is not available" >&2; failed=1
    else
      echo "healthy: $target"
    fi
  done
  (( failed == 0 )) || return 1
}

create_fixture() {
  cluster_ready
  kubectl get namespace "$NAMESPACE" >/dev/null 2>&1 || die "namespace '$NAMESPACE' is missing; deploy the integrated stack first"
  helm status "$RELEASE" -n "$NAMESPACE" >/dev/null 2>&1 || die "Helm release '$RELEASE' is missing from '$NAMESPACE'"
  kubectl -n "$NAMESPACE" create configmap praetor-validation-ldap-bootstrap --from-file=bootstrap.ldif="$ROOT/deployments/ldap/bootstrap.ldif" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "$NAMESPACE" create configmap praetor-validation-ldap-config --from-file=ldap.yaml="$ROOT/deployments/ldap/ldap-config.yaml" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl apply -n "$NAMESPACE" -f "$MANIFEST" >/dev/null
  kubectl rollout status deployment/praetor-validation-ldap -n "$NAMESPACE" --timeout=180s
  kubectl rollout status deployment/praetor-validation-notification-sink -n "$NAMESPACE" --timeout=180s
  helm upgrade "$RELEASE" "$CHART" -n "$NAMESPACE" --reuse-values --set ldap.enabled=true --set ldap.existingConfigMap=praetor-validation-ldap-config --set secrets.ldapBindPassword=admin --wait --timeout 5m >/dev/null
  kubectl rollout status "deployment/$RELEASE-api" -n "$NAMESPACE" --timeout=180s
  status_fixture
  echo "==> Seeding API resources"
  seed_api_resources
}

cleanup_fixture() {
  cluster_ready
  local db_pod
  db_pod="$(kubectl get pods -n "$NAMESPACE" -l "app.kubernetes.io/instance=$RELEASE,app.kubernetes.io/component=postgresql" -o jsonpath='{.items[0].metadata.name}')"
  [[ -n "$db_pod" ]] || die "Praetor database pod is missing"
  kubectl exec -i -n "$NAMESPACE" "$db_pod" -- psql -v ON_ERROR_STOP=1 -U postgres -d praetor >/dev/null <<'SQL'
BEGIN;
DELETE FROM workflow_templates WHERE name = 'Praetor Validation Workflow';
DELETE FROM workflow_templates WHERE name = 'Praetor Validation LDAP Workflow';
DELETE FROM service_principals WHERE name = 'Praetor Validation API';
DELETE FROM notification_templates WHERE name = 'Praetor Validation Notifications';
DELETE FROM job_templates WHERE name = 'Praetor Validation Job';
DELETE FROM hosts WHERE name = 'Praetor Validation Host';
DELETE FROM inventories WHERE name = 'Praetor Validation Inventory';
DELETE FROM projects WHERE name = 'Praetor Validation Project';
COMMIT;
SQL
  kubectl delete all -n "$NAMESPACE" -l "$LABEL" --ignore-not-found >/dev/null
  kubectl delete configmap -n "$NAMESPACE" praetor-validation-ldap-bootstrap praetor-validation-ldap-config --ignore-not-found >/dev/null
  if helm status "$RELEASE" -n "$NAMESPACE" >/dev/null 2>&1; then
    helm upgrade "$RELEASE" "$CHART" -n "$NAMESPACE" --reuse-values --set ldap.enabled=false --set ldap.existingConfigMap= --wait --timeout 5m >/dev/null
  fi
  echo "validation fixture removed; persistent platform data and secrets were preserved"
}

case "${1:-}" in
  create) create_fixture ;;
  status) status_fixture ;;
  cleanup) cleanup_fixture ;;
  *) usage >&2; exit 2 ;;
esac
