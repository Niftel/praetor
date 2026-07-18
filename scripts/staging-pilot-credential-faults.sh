#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}"
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-$HOME/.local/share/praetor/staging}"
EVIDENCE_FILE="${PRAETOR_PILOT_CREDENTIAL_EVIDENCE_FILE:-$DATA_ROOT/pilot/evidence/credential-faults.json}"
PROJECT_REF="${PRAETOR_PILOT_FAULT_PROJECT_REF:-$(git -C "$ROOT" rev-parse --abbrev-ref HEAD)}"

if [[ "$(kubectl config current-context)" != "$CONTEXT" ]]; then
  echo "error: current Kubernetes context is not '$CONTEXT'" >&2
  echo "select it explicitly before running this staging-only destructive validation" >&2
  exit 1
fi

install -d -m 0700 "$(dirname "$EVIDENCE_FILE")"

env \
  PRAETOR_NAMESPACE="${PRAETOR_STAGING_NAMESPACE:-praetor-staging}" \
  PRAETOR_E2E_RELEASE="${PRAETOR_STAGING_RELEASE:-praetor-staging}" \
  PRAETOR_API_SERVICE="${PRAETOR_STAGING_API_SERVICE:-praetor-staging-api}" \
  PRAETOR_E2E_SECRETS_SERVICE="${PRAETOR_STAGING_SECRETS_SERVICE:-praetor-secrets}" \
  PRAETOR_E2E_USERNAME="${PRAETOR_PILOT_OPERATOR_USERNAME:-demo-operator}" \
  PRAETOR_E2E_PASSWORD="${PRAETOR_STAGING_ACCEPTANCE_PASSWORD:-praetor123}" \
  PRAETOR_E2E_AUDITOR_USERNAME="${PRAETOR_PILOT_AUDITOR_USERNAME:-demo-auditor}" \
  PRAETOR_E2E_AUDITOR_PASSWORD="${PRAETOR_STAGING_ACCEPTANCE_PASSWORD:-praetor123}" \
  PRAETOR_E2E_PROJECT_REF="$PROJECT_REF" \
  PRAETOR_E2E_SECRETS_DB_APP="${PRAETOR_STAGING_SECRETS_DB_APP:-praetor-staging-secrets-postgres}" \
  PRAETOR_E2E_AUDIT_DB_APP="${PRAETOR_STAGING_AUDIT_DB_APP:-praetor-staging-audit-postgres}" \
  PRAETOR_E2E_SECRETS_DB_NAME="${PRAETOR_STAGING_SECRETS_DB_NAME:-praetor_secrets}" \
  PRAETOR_E2E_AUDIT_DB_NAME="${PRAETOR_STAGING_AUDIT_DB_NAME:-praetor_audit}" \
  PRAETOR_E2E_EVIDENCE_FILE="$EVIDENCE_FILE" \
  bash "$ROOT/scripts/test-secrets-execution-e2e.sh"

chmod 0600 "$EVIDENCE_FILE"
jq -e '.result == "pass" and (.checks | length == 9)' "$EVIDENCE_FILE" >/dev/null
echo "pilot credential fault matrix passed; sanitized evidence: $EVIDENCE_FILE"
