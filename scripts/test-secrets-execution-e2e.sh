#!/usr/bin/env bash
set -euo pipefail

# Prove the deployed Praetor -> Secrets Service execution path with a real job:
# API creates an encrypted credential, scheduler snapshots a run binding,
# executor resolves it by run ID, and the playbook completes successfully.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAMESPACE="${PRAETOR_NAMESPACE:-praetor-secrets}"
API_SERVICE="${PRAETOR_API_SERVICE:-praetor-api}"
API_PORT="${PRAETOR_E2E_API_PORT:-18080}"
ADMIN_USERNAME="${PRAETOR_E2E_USERNAME:-admin}"
ADMIN_PASSWORD="${PRAETOR_E2E_PASSWORD:-admin}"
ORGANIZATION_ID="${PRAETOR_E2E_ORGANIZATION_ID:-2}"
PROJECT_URL="${PRAETOR_E2E_PROJECT_URL:-https://github.com/Niftel/praetor.git}"
PLAYBOOK="${PRAETOR_E2E_PLAYBOOK:-playbooks/ping.yml}"
TIMEOUT_SECONDS="${PRAETOR_E2E_TIMEOUT_SECONDS:-180}"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command '$1' is not installed" >&2
    exit 1
  }
}

for command in curl jq kubectl openssl; do
  need "$command"
done

EXECUTOR_POD="$(
  kubectl get pods -n "$NAMESPACE" \
    -l app.kubernetes.io/component=executor,app.kubernetes.io/instance=praetor \
    -o jsonpath='{.items[0].metadata.name}'
)"
PRAETOR_DB_POD="$(
  kubectl get pods -n "$NAMESPACE" \
    -l app.kubernetes.io/component=postgresql,app.kubernetes.io/instance=praetor \
    -o jsonpath='{.items[0].metadata.name}'
)"
SECRETS_DB_POD="$(
  kubectl get pods -n "$NAMESPACE" \
    -l app=praetor-secrets-postgres \
    -o jsonpath='{.items[0].metadata.name}'
)"

for value in "$EXECUTOR_POD" "$PRAETOR_DB_POD" "$SECRETS_DB_POD"; do
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

PORT_FORWARD_LOG="$(mktemp "${TMPDIR:-/tmp}/praetor-e2e-port-forward.XXXXXX.log")"
LOGIN_RESPONSE="$(mktemp "${TMPDIR:-/tmp}/praetor-e2e-login.XXXXXX.json")"
trap 'kill "${PORT_FORWARD_PID:-}" 2>/dev/null || true; rm -f "$PORT_FORWARD_LOG" "$LOGIN_RESPONSE"' EXIT

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

echo "==> Authenticating through the Praetor API"
curl -fsS -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg username "$ADMIN_USERNAME" --arg password "$ADMIN_PASSWORD" \
    '{username:$username,password:$password}')" \
  "$API/auth/login" >"$LOGIN_RESPONSE"
TOKEN="$(jq -er .token "$LOGIN_RESPONSE")"

api_post() {
  local path="$1"
  local body="$2"
  curl -fsS -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d "$body" "$API/$path"
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

echo "==> Creating the SCM project and credential-backed template"
PROJECT="$(
  api_post projects "$(jq -nc \
    --argjson organization_id "$ORGANIZATION_ID" \
    --arg name "Secrets E2E $STAMP" \
    --arg scm_url "$PROJECT_URL" \
    '{organization_id:$organization_id,name:$name,scm_type:"git",scm_url:$scm_url}')"
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
    kubectl exec -n "$NAMESPACE" "$SECRETS_DB_POD" -- sh -c \
      'PGPASSWORD="$POSTGRES_PASSWORD" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc \
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
  kubectl exec -n "$NAMESPACE" "$SECRETS_DB_POD" -- sh -c \
    'PGPASSWORD="$POSTGRES_PASSWORD" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc \
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

if [[ -n "${PRAETOR_E2E_EVIDENCE_FILE:-}" ]]; then
  jq -n \
    --argjson credential_id "$CREDENTIAL_ID" \
    --argjson project_id "$PROJECT_ID" \
    --argjson job_id "$JOB_ID" \
    --arg run_id "$RUN_ID" \
    --arg status "$STATUS" \
    '{
      credential_id:$credential_id,
      project_id:$project_id,
      job_id:$job_id,
      run_id:$run_id,
      status:$status
    }' >"$PRAETOR_E2E_EVIDENCE_FILE"
fi

echo "PASS: credential $CREDENTIAL_ID stayed encrypted; run $RUN_ID resolved once and completed successfully"
