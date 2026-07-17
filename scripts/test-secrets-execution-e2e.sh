#!/usr/bin/env bash
set -euo pipefail

# Prove the deployed Praetor -> Secrets Service execution path with a real job:
# API creates an encrypted credential, scheduler snapshots a run binding,
# executor resolves it by run ID, and the playbook completes successfully.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAMESPACE="${PRAETOR_NAMESPACE:-praetor-secrets}"
RELEASE="${PRAETOR_E2E_RELEASE:-praetor}"
API_SERVICE="${PRAETOR_API_SERVICE:-praetor-api}"
API_PORT="${PRAETOR_E2E_API_PORT:-18080}"
SECRETS_SERVICE="${PRAETOR_E2E_SECRETS_SERVICE:-praetor-secrets}"
SECRETS_PORT="${PRAETOR_E2E_SECRETS_PORT:-18443}"
ADMIN_USERNAME="${PRAETOR_E2E_USERNAME:-admin}"
ADMIN_PASSWORD="${PRAETOR_E2E_PASSWORD:-admin}"
AUDITOR_USERNAME="${PRAETOR_E2E_AUDITOR_USERNAME:-demo-auditor}"
AUDITOR_PASSWORD="${PRAETOR_E2E_AUDITOR_PASSWORD:-praetor123}"
ORGANIZATION_ID="${PRAETOR_E2E_ORGANIZATION_ID:-}"
PROJECT_URL="${PRAETOR_E2E_PROJECT_URL:-https://github.com/Niftel/praetor.git}"
PLAYBOOK="${PRAETOR_E2E_PLAYBOOK:-playbooks/validate-credential-injection.yml}"
PROJECT_REF="${PRAETOR_E2E_PROJECT_REF:-${GITHUB_HEAD_REF:-}}"
TIMEOUT_SECONDS="${PRAETOR_E2E_TIMEOUT_SECONDS:-180}"
SECRETS_DB_SELECTOR="app=${PRAETOR_E2E_SECRETS_DB_APP:-praetor-secrets-postgres}"
AUDIT_DB_SELECTOR="app=${PRAETOR_E2E_AUDIT_DB_APP:-praetor-audit-postgres}"
SECRETS_DB_NAME="${PRAETOR_E2E_SECRETS_DB_NAME:-postgres}"
AUDIT_DB_NAME="${PRAETOR_E2E_AUDIT_DB_NAME:-postgres}"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command '$1' is not installed" >&2
    exit 1
  }
}

for command in curl jq kubectl openssl; do
  need "$command"
done

first_pod() { kubectl get pods -n "$NAMESPACE" -l "$1" -o name | head -n1 | cut -d/ -f2; }
EXECUTOR_POD="$(first_pod "app.kubernetes.io/component=executor,app.kubernetes.io/instance=$RELEASE")"
PRAETOR_DB_POD="$(first_pod "app.kubernetes.io/component=postgresql,app.kubernetes.io/instance=$RELEASE")"
SECRETS_DB_POD="$(first_pod "$SECRETS_DB_SELECTOR")"
AUDIT_DB_POD="$(first_pod "$AUDIT_DB_SELECTOR")"

for value in "$EXECUTOR_POD" "$PRAETOR_DB_POD" "$SECRETS_DB_POD" "$AUDIT_DB_POD"; do
  if [[ -z "$value" ]]; then
    echo "error: integrated Praetor/Secrets stack is incomplete in namespace '$NAMESPACE'" >&2
    exit 1
  fi
done

EXECUTOR_ARCH="$(
  kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- uname -m |
    sed -e 's/^aarch64$/arm64/' -e 's/^x86_64$/amd64/'
)"
PACK="${PRAETOR_E2E_PACK:-$ROOT/build/runtime/ansible-runtime-linux-$EXECUTOR_ARCH.tar.gz}"
if [[ "$EXECUTOR_ARCH" != "arm64" && "$EXECUTOR_ARCH" != "amd64" ]]; then
  echo "error: unsupported executor architecture '$EXECUTOR_ARCH'" >&2
  exit 1
fi
if [[ ! -s "$PACK" ]]; then
  echo "error: $EXECUTOR_ARCH Execution Pack not found at '$PACK'" >&2
  echo "build it first with: make execpack" >&2
  exit 1
fi

echo "==> Staging the released Execution Pack"
kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- \
  sh -c 'mkdir -p /tmp/build/runtime'
kubectl cp "$PACK" \
  "$NAMESPACE/$EXECUTOR_POD:/tmp/build/runtime/ansible-runtime-linux-$EXECUTOR_ARCH.tar.gz"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/praetor-secrets-e2e.XXXXXX")"
chmod 700 "$WORK"
PORT_FORWARD_LOG="$WORK/api-port-forward.log"
SECRETS_FORWARD_LOG="$WORK/secrets-port-forward.log"
LOGIN_RESPONSE="$WORK/login.json"
trap 'kill "${PORT_FORWARD_PID:-}" "${SECRETS_FORWARD_PID:-}" 2>/dev/null || true; rm -rf "$WORK"' EXIT

kubectl port-forward -n "$NAMESPACE" "svc/$API_SERVICE" "$API_PORT:8080" \
  >"$PORT_FORWARD_LOG" 2>&1 &
PORT_FORWARD_PID=$!

API="http://127.0.0.1:$API_PORT/api/v1"
for _ in $(seq 1 30); do
  if curl -fsS "$API/ping" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$PORT_FORWARD_PID" 2>/dev/null; then
    echo "error: API port-forward stopped unexpectedly" >&2
    cat "$PORT_FORWARD_LOG" >&2
    exit 1
  fi
  sleep 1
done
curl -fsS "$API/ping" >/dev/null

kubectl port-forward -n "$NAMESPACE" "svc/$SECRETS_SERVICE" "$SECRETS_PORT:8443" \
  >"$SECRETS_FORWARD_LOG" 2>&1 &
SECRETS_FORWARD_PID=$!
SECRETS_HOST="praetor-secrets.$NAMESPACE.svc"
SECRETS_API="https://$SECRETS_HOST:$SECRETS_PORT/internal/v1"

extract_identity() {
  local secret="$1" destination="$2" key
  mkdir -p "$destination"
  chmod 700 "$destination"
  for key in ca.crt tls.crt tls.key; do
    kubectl get secret -n "$NAMESPACE" "$secret" -o json |
      jq -er --arg key "$key" '.data[$key]' |
      openssl base64 -d -A >"$destination/$key"
  done
  chmod 600 "$destination"/*
}
extract_identity praetor-api-identity "$WORK/api-identity"
extract_identity praetor-scheduler-identity "$WORK/scheduler-identity"
extract_identity praetor-executor-identity "$WORK/executor-identity"

secrets_request() {
  local identity="$1" method="$2" path="$3" body="${4:-}" header="${5:-}" output="$WORK/secrets-response.json"
  local args=(--silent --show-error --output "$output" --write-out '%{http_code}'
    --resolve "$SECRETS_HOST:$SECRETS_PORT:127.0.0.1"
    --cacert "$WORK/$identity-identity/ca.crt"
    --cert "$WORK/$identity-identity/tls.crt"
    --key "$WORK/$identity-identity/tls.key"
    --request "$method" --header 'Accept: application/json')
  [[ -z "$header" ]] || args+=(--header "$header")
  if [[ -n "$body" ]]; then
    args+=(--header 'Content-Type: application/json' --data "$body")
  fi
  SECRETS_STATUS="$(curl "${args[@]}" "$SECRETS_API/$path")"
  SECRETS_RESPONSE="$(cat "$output")"
}

uuid() {
  local hex
  hex="$(openssl rand -hex 16)"
  printf '%s-%s-%s-%s-%s\n' "${hex:0:8}" "${hex:8:4}" "${hex:12:4}" "${hex:16:4}" "${hex:20:12}"
}

echo "==> Authenticating through the Praetor API"
curl -fsS -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg username "$ADMIN_USERNAME" --arg password "$ADMIN_PASSWORD" \
    '{username:$username,password:$password}')" \
  "$API/auth/login" >"$LOGIN_RESPONSE"
TOKEN="$(jq -er .token "$LOGIN_RESPONSE")"

if [[ -z "$ORGANIZATION_ID" ]]; then
  ORGANIZATION_ID="$(curl -fsS -H "Authorization: Bearer $TOKEN" "$API/organizations/" |
    jq -er '(if type == "object" then .items else . end)[] | select(.name == "Engineering") | .id')"
fi

api_post() {
  local path="$1"
  local body="$2"
  curl -fsS -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d "$body" "$API/$path"
}
api_get() {
  curl -fsS -H "Authorization: Bearer $TOKEN" "$API/$1"
}
api_delete() {
  curl -fsS -X DELETE -H "Authorization: Bearer $TOKEN" "$API/$1"
}

STAMP="$(date -u +%Y%m%d%H%M%S)-$$"
SENTINEL="${PRAETOR_E2E_SENTINEL:-praetor-e2e-$(openssl rand -hex 16)}"

echo "==> Creating a fresh service-backed Machine credential"
CREDENTIAL="$(
  api_post credentials "$(jq -nc \
    --argjson organization_id "$ORGANIZATION_ID" \
    --arg name "Secrets E2E $STAMP" \
    --arg password "$SENTINEL" \
    '{
      organization_id:$organization_id,
      credential_type_id:1,
      name:$name,
      inputs:{username:"automation",password:$password}
    }')"
)"
CREDENTIAL_ID="$(jq -er .id <<<"$CREDENTIAL")"
if [[ "$(jq -r '.inputs.password' <<<"$CREDENTIAL")" != '$encrypted$' ]]; then
  echo "error: credential API response did not mask the submitted password" >&2
  exit 1
fi

STORED_INPUTS="$(
  kubectl exec -n "$NAMESPACE" "$PRAETOR_DB_POD" -- \
    psql -U postgres -d praetor -Atc \
    "select inputs::text from credentials where id=$CREDENTIAL_ID"
)"
if grep -Fq "$SENTINEL" <<<"$STORED_INPUTS"; then
  echo "error: credential plaintext was stored in the Praetor database" >&2
  exit 1
fi
if ! grep -Fq '$encrypted$' <<<"$STORED_INPUTS"; then
  echo "error: Praetor credential record does not contain a secret placeholder" >&2
  exit 1
fi

SERVICE_ROW="$(
  kubectl exec -n "$NAMESPACE" "$PRAETOR_DB_POD" -- \
    psql -U postgres -d praetor -Atc \
    "select secrets_service_id::text||'|'||secrets_service_version::text from credentials where id=$CREDENTIAL_ID"
)"
SERVICE_CREDENTIAL_ID="${SERVICE_ROW%%|*}"
SERVICE_CREDENTIAL_VERSION="${SERVICE_ROW#*|}"
[[ -n "$SERVICE_CREDENTIAL_ID" && "$SERVICE_CREDENTIAL_VERSION" =~ ^[0-9]+$ ]] || {
  echo "error: Praetor stored no usable Secrets Service credential reference" >&2
  exit 1
}

echo "==> Verifying public API credential scope"
OUTSIDER_LOGIN="$(curl -fsS -H 'Content-Type: application/json' \
  -d '{"username":"fwalsh","password":"praetor123"}' "$API/auth/login")"
OUTSIDER_TOKEN="$(jq -er .token <<<"$OUTSIDER_LOGIN")"
if curl -fsS -H "Authorization: Bearer $OUTSIDER_TOKEN" "$API/credentials" |
  jq -e --argjson id "$CREDENTIAL_ID" '[(if type == "object" then .items else . end)[] | select(.id == $id)] | length == 0' >/dev/null; then
  :
else
  echo "error: unauthorized team can list the credential metadata" >&2
  exit 1
fi
OUTSIDER_BODY="$WORK/outsider-credential.json"
OUTSIDER_STATUS="$(curl -sS -o "$OUTSIDER_BODY" -w '%{http_code}' \
  -H "Authorization: Bearer $OUTSIDER_TOKEN" "$API/credentials/$CREDENTIAL_ID")"
if [[ "$OUTSIDER_STATUS" != "403" ]]; then
  echo "error: unauthorized credential read returned $OUTSIDER_STATUS, expected 403" >&2
  exit 1
fi

echo "==> Creating the SCM project and credential-backed template"
PROJECT="$(
  api_post projects "$(jq -nc \
    --argjson organization_id "$ORGANIZATION_ID" \
    --arg name "Secrets E2E $STAMP" \
    --arg scm_url "$PROJECT_URL" \
    --arg scm_branch "$PROJECT_REF" \
    '{organization_id:$organization_id,name:$name,scm_type:"git",scm_url:$scm_url,scm_branch:$scm_branch}')"
)"
PROJECT_ID="$(jq -er .id <<<"$PROJECT")"

TEMPLATE="$(
  api_post job-templates "$(jq -nc \
    --argjson organization_id "$ORGANIZATION_ID" \
    --argjson project_id "$PROJECT_ID" \
    --argjson credential_id "$CREDENTIAL_ID" \
    --arg name "Secrets E2E $STAMP" \
    --arg playbook "$PLAYBOOK" \
    '{
      organization_id:$organization_id,
      name:$name,
      project_id:$project_id,
      playbook:$playbook,
      credential_id:$credential_id,
      forks:1,
      job_type:"run",
      verbosity:1
    }')"
)"
UJT_ID="$(jq -er .unified_job_template_id <<<"$TEMPLATE")"

echo "==> Launching the real execution run"
LAUNCH="$(
  api_post jobs "$(jq -nc \
    --argjson unified_job_template_id "$UJT_ID" \
    --arg name "Secrets E2E $STAMP" \
    '{unified_job_template_id:$unified_job_template_id,name:$name}')"
)"
JOB_ID="$(jq -er .id <<<"$LAUNCH")"

DEADLINE=$((SECONDS + TIMEOUT_SECONDS))
STATUS=""
RUN_ID=""
while (( SECONDS < DEADLINE )); do
  ROW="$(
    kubectl exec -n "$NAMESPACE" "$PRAETOR_DB_POD" -- \
      psql -U postgres -d praetor -Atc \
      "select status||'|'||coalesce(current_run_id::text,'') from unified_jobs where id=$JOB_ID"
  )"
  STATUS="${ROW%%|*}"
  RUN_ID="${ROW#*|}"
  case "$STATUS" in
    successful|failed|canceled|error) break ;;
  esac
  sleep 2
done

if [[ "$STATUS" != "successful" || -z "$RUN_ID" ]]; then
  echo "error: E2E job $JOB_ID ended with status '${STATUS:-unknown}'" >&2
  if [[ -n "$RUN_ID" ]]; then
    kubectl exec -n "$NAMESPACE" "$PRAETOR_DB_POD" -- \
      psql -U postgres -d praetor -P pager=off -c \
      "select event_type,stdout_snippet from job_events where execution_run_id='$RUN_ID' order by seq" >&2
  fi
  exit 1
fi

echo "==> Verifying scoped resolution and terminal cancellation"
DEADLINE=$((SECONDS + 30))
BINDING=""
while (( SECONDS < DEADLINE )); do
  BINDING="$(
    kubectl exec -n "$NAMESPACE" "$SECRETS_DB_POD" -- env POSTGRES_DB="$SECRETS_DB_NAME" sh -c \
      'PGPASSWORD="${POSTGRES_PASSWORD:-validation-only}" psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-postgres}" -Atc \
       "select state||'\''|'\''||executor_identity||'\''|'\''||resolution_count from run_bindings where run_id='\''$1'\''"' \
      sh "$RUN_ID"
  )"
  [[ "$BINDING" == canceled\|praetor-executor:*\|1 ]] && break
  sleep 1
done
if [[ "$BINDING" != canceled\|praetor-executor:*\|1 ]]; then
  echo "error: unexpected Secrets Service binding result for run $RUN_ID" >&2
  exit 1
fi

ATTEMPTS="$(
  kubectl exec -n "$NAMESPACE" "$SECRETS_DB_POD" -- env POSTGRES_DB="$SECRETS_DB_NAME" sh -c \
    'PGPASSWORD="${POSTGRES_PASSWORD:-validation-only}" psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-postgres}" -Atc \
     "select count(*) from resolution_attempts where run_id='\''$1'\''"' \
    sh "$RUN_ID"
)"
if [[ "$ATTEMPTS" != "1" ]]; then
  echo "error: expected exactly one credential resolution attempt, got $ATTEMPTS" >&2
  exit 1
fi

COMPLETED="$(
  kubectl exec -n "$NAMESPACE" "$PRAETOR_DB_POD" -- \
    psql -U postgres -d praetor -Atc \
    "select count(*) from job_events where execution_run_id='$RUN_ID' and event_type='JOB_COMPLETED'"
)"
if [[ "$COMPLETED" != "1" ]]; then
  echo "error: successful run is missing its JOB_COMPLETED event" >&2
  exit 1
fi
if kubectl exec -n "$NAMESPACE" "$EXECUTOR_POD" -- \
  grep -Fq "$SENTINEL" "/var/lib/praetor/jobs/$RUN_ID/manifest.json" 2>/dev/null; then
  echo "error: terminal executor manifest retained planted secret material" >&2
  exit 1
fi

EXECUTOR_IDENTITY="${BINDING#*|}"
EXECUTOR_IDENTITY="${EXECUTOR_IDENTITY%|*}"
REQUESTED_AT="$(jq -nr 'now | todateiso8601')"

assert_denied_without_secret() {
  local expected_status="$1" context="$2"
  if [[ "$SECRETS_STATUS" != "$expected_status" ]]; then
    echo "error: $context returned $SECRETS_STATUS, expected $expected_status" >&2
    exit 1
  fi
  if grep -Fq "$SENTINEL" <<<"$SECRETS_RESPONSE"; then
    echo "error: $context exposed planted secret material" >&2
    exit 1
  fi
}

echo "==> Proving completed runs cannot resolve their canceled binding"
secrets_request executor POST "runs/$RUN_ID/credential:resolve" \
  "$(jq -nc --arg attempt_id "$(uuid)" --arg requested_at "$REQUESTED_AT" '{attempt_id:$attempt_id,requested_at:$requested_at}')"
assert_denied_without_secret 403 "completed-run credential replay"

echo "==> Proving workload identity and explicit cancellation boundaries"
CANCEL_RUN_ID="$(uuid)"
CANCEL_DISPATCH_ID="$(uuid)"
NOT_BEFORE="$(jq -nr 'now | todateiso8601')"
EXPIRES_AT="$(jq -nr 'now + 300 | todateiso8601')"
REGISTER_BODY="$(jq -nc \
  --arg run_id "$CANCEL_RUN_ID" \
  --arg dispatch_id "$CANCEL_DISPATCH_ID" \
  --arg organization_id "$ORGANIZATION_ID" \
  --arg credential_id "$SERVICE_CREDENTIAL_ID" \
  --arg executor_identity "$EXECUTOR_IDENTITY" \
  --arg not_before "$NOT_BEFORE" \
  --arg expires_at "$EXPIRES_AT" \
  '{run_id:$run_id,dispatch_id:$dispatch_id,organization_id:$organization_id,credential_id:$credential_id,executor_identity:$executor_identity,not_before:$not_before,expires_at:$expires_at,max_resolutions:2}')"
secrets_request scheduler POST run-bindings "$REGISTER_BODY" "Idempotency-Key: $CANCEL_DISPATCH_ID"
[[ "$SECRETS_STATUS" == 201 ]] || { echo "error: cancellation test binding registration returned $SECRETS_STATUS" >&2; exit 1; }

secrets_request api POST "runs/$CANCEL_RUN_ID/credential:resolve" \
  "$(jq -nc --arg attempt_id "$(uuid)" --arg requested_at "$REQUESTED_AT" '{attempt_id:$attempt_id,requested_at:$requested_at}')"
assert_denied_without_secret 403 "wrong-workload credential resolution"

secrets_request scheduler POST "run-bindings/$CANCEL_RUN_ID/cancel" \
  "$(jq -nc --arg dispatch_id "$CANCEL_DISPATCH_ID" '{dispatch_id:$dispatch_id,reason:"run_canceled"}')"
[[ "$SECRETS_STATUS" == 200 ]] || { echo "error: binding cancellation returned $SECRETS_STATUS" >&2; exit 1; }
secrets_request executor POST "runs/$CANCEL_RUN_ID/credential:resolve" \
  "$(jq -nc --arg attempt_id "$(uuid)" --arg requested_at "$(jq -nr 'now | todateiso8601')" '{attempt_id:$attempt_id,requested_at:$requested_at}')"
assert_denied_without_secret 403 "canceled binding resolution"

echo "==> Proving expiration is enforced by the service"
EXPIRED_RUN_ID="$(uuid)"
EXPIRED_DISPATCH_ID="$(uuid)"
NOT_BEFORE="$(jq -nr 'now | todateiso8601')"
EXPIRES_AT="$(jq -nr 'now + 2 | todateiso8601')"
REGISTER_BODY="$(jq -nc \
  --arg run_id "$EXPIRED_RUN_ID" \
  --arg dispatch_id "$EXPIRED_DISPATCH_ID" \
  --arg organization_id "$ORGANIZATION_ID" \
  --arg credential_id "$SERVICE_CREDENTIAL_ID" \
  --arg executor_identity "$EXECUTOR_IDENTITY" \
  --arg not_before "$NOT_BEFORE" \
  --arg expires_at "$EXPIRES_AT" \
  '{run_id:$run_id,dispatch_id:$dispatch_id,organization_id:$organization_id,credential_id:$credential_id,executor_identity:$executor_identity,not_before:$not_before,expires_at:$expires_at,max_resolutions:1}')"
secrets_request scheduler POST run-bindings "$REGISTER_BODY" "Idempotency-Key: $EXPIRED_DISPATCH_ID"
[[ "$SECRETS_STATUS" == 201 ]] || { echo "error: expiration test binding registration returned $SECRETS_STATUS" >&2; exit 1; }
sleep 3
secrets_request executor POST "runs/$EXPIRED_RUN_ID/credential:resolve" \
  "$(jq -nc --arg attempt_id "$(uuid)" --arg requested_at "$(jq -nr 'now | todateiso8601')" '{attempt_id:$attempt_id,requested_at:$requested_at}')"
assert_denied_without_secret 403 "expired binding resolution"
EXPIRED_STATE="$(kubectl exec -n "$NAMESPACE" "$SECRETS_DB_POD" -- env POSTGRES_DB="$SECRETS_DB_NAME" sh -c \
  'PGPASSWORD="${POSTGRES_PASSWORD:-validation-only}" psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-postgres}" -Atc \
   "select state from run_bindings where run_id='\''$1'\''"' \
  sh "$EXPIRED_RUN_ID")"
[[ "$EXPIRED_STATE" == expired ]] || { echo "error: expired binding remained in state '$EXPIRED_STATE'" >&2; exit 1; }

echo "==> Retiring the credential and rejecting new claims"
api_delete "credentials/$CREDENTIAL_ID" >/dev/null
secrets_request api GET "credentials/$SERVICE_CREDENTIAL_ID" "" "X-Praetor-Organization-ID: $ORGANIZATION_ID"
[[ "$SECRETS_STATUS" == 200 ]] || { echo "error: retired credential metadata returned $SECRETS_STATUS" >&2; exit 1; }
jq -e --argjson previous "$SERVICE_CREDENTIAL_VERSION" '.state == "retired" and .version > $previous and (.inputs? | not)' <<<"$SECRETS_RESPONSE" >/dev/null || {
  echo "error: credential retirement metadata is inconsistent or secret-bearing" >&2
  exit 1
}
REVOKED_RUN_ID="$(uuid)"
REVOKED_DISPATCH_ID="$(uuid)"
REGISTER_BODY="$(jq -nc \
  --arg run_id "$REVOKED_RUN_ID" \
  --arg dispatch_id "$REVOKED_DISPATCH_ID" \
  --arg organization_id "$ORGANIZATION_ID" \
  --arg credential_id "$SERVICE_CREDENTIAL_ID" \
  --arg executor_identity "$EXECUTOR_IDENTITY" \
  --arg not_before "$(jq -nr 'now | todateiso8601')" \
  --arg expires_at "$(jq -nr 'now + 300 | todateiso8601')" \
  '{run_id:$run_id,dispatch_id:$dispatch_id,organization_id:$organization_id,credential_id:$credential_id,executor_identity:$executor_identity,not_before:$not_before,expires_at:$expires_at,max_resolutions:1}')"
secrets_request scheduler POST run-bindings "$REGISTER_BODY" "Idempotency-Key: $REVOKED_DISPATCH_ID"
assert_denied_without_secret 404 "retired credential binding registration"

echo "==> Scanning API, audit, database, and workload artifacts for plaintext"
printf '%s\n' "$CREDENTIAL" >"$WORK/credential-response.json"
ACTIVITY_STATUS="$(curl -sS -o "$WORK/praetor-activity.json" -w '%{http_code}' \
  -H "Authorization: Bearer $TOKEN" "$API/activity-stream?limit=500")"
if [[ "$ACTIVITY_STATUS" == 403 ]]; then
  AUDITOR_LOGIN="$(curl -fsS -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg username "$AUDITOR_USERNAME" --arg password "$AUDITOR_PASSWORD" '{username:$username,password:$password}')" \
    "$API/auth/login")"
  AUDITOR_TOKEN="$(jq -er .token <<<"$AUDITOR_LOGIN")"
  curl -fsS -H "Authorization: Bearer $AUDITOR_TOKEN" "$API/activity-stream?limit=500" >"$WORK/praetor-activity.json"
elif [[ "$ACTIVITY_STATUS" != 200 ]]; then
  echo "error: activity stream returned $ACTIVITY_STATUS" >&2
  exit 1
fi
kubectl exec -n "$NAMESPACE" "$PRAETOR_DB_POD" -- \
  pg_dump -U postgres -d praetor --data-only >"$WORK/praetor-database.sql"
kubectl exec -n "$NAMESPACE" "$SECRETS_DB_POD" -- env POSTGRES_DB="$SECRETS_DB_NAME" sh -c \
  'PGPASSWORD="${POSTGRES_PASSWORD:-validation-only}" pg_dump -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-postgres}" --data-only' >"$WORK/secrets-database.sql"
kubectl exec -n "$NAMESPACE" "$AUDIT_DB_POD" -- env POSTGRES_DB="$AUDIT_DB_NAME" sh -c \
  'PGPASSWORD="${POSTGRES_PASSWORD:-validation-only}" pg_dump -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-postgres}" --data-only' >"$WORK/audit-database.sql"
while IFS= read -r pod; do
  kubectl logs -n "$NAMESPACE" "$pod" --all-containers=true >"$WORK/log-${pod#pod/}.txt" 2>/dev/null || true
done < <(kubectl get pods -n "$NAMESPACE" -o name)
while IFS= read -r artifact; do
  if grep -Fq "$SENTINEL" "$artifact"; then
    echo "error: planted secret appeared in captured $(basename "$artifact")" >&2
    exit 1
  fi
done < <(find "$WORK" -type f ! -path '*/api-identity/*' ! -path '*/scheduler-identity/*' ! -path '*/executor-identity/*')

if [[ -n "${PRAETOR_E2E_EVIDENCE_FILE:-}" ]]; then
  umask 077
  jq -n \
    --arg result pass \
    --argjson credential_id "$CREDENTIAL_ID" \
    --argjson project_id "$PROJECT_ID" \
    --argjson job_id "$JOB_ID" \
    --arg run_id "$RUN_ID" \
    --arg status "$STATUS" \
    '{
      schema_version:1,
      journey:"secrets-service",
      result:$result,
      credential_id:$credential_id,
      project_id:$project_id,
      job_id:$job_id,
      run_id:$run_id,
      status:$status
    }' >"$PRAETOR_E2E_EVIDENCE_FILE"
fi

echo "PASS: credential $CREDENTIAL_ID stayed encrypted and scoped; run $RUN_ID resolved once; canceled, expired, unauthorized, and retired paths failed closed"
