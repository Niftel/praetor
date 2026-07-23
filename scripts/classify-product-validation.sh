#!/usr/bin/env bash
set -euo pipefail

# Pushes and manual runs always produce complete release-grade evidence.
if [[ "${EVENT_NAME:-}" != pull_request ]]; then
  echo true
  exit 0
fi

# Pull-request paths arrive on stdin. Only changes to the validation harness
# itself need the full disposable-cluster journey before merge; all other
# changes receive isolated gates here and the complete lifecycle on main.
if grep -Eq '^(\.github/workflows/product-validation-fixture\.yml|deployments/product-validation/|scripts/(bootstrap-product-validation-base|classify-product-validation|product-validation-fixture|generate-readiness-report|test-secrets-execution-e2e|validate-delegated-api-e2e|validate-ldap-operator-journey|validate-execution-recovery-e2e|validate-dynamic-inventory-e2e|validate-notification-delivery-e2e)\.sh|cmd/readiness-report/|internal/readiness/|playbooks/validate-execution-recovery\.yml)'; then
  echo true
else
  echo false
fi
