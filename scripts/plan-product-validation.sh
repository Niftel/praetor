#!/usr/bin/env bash
set -euo pipefail

event="${EVENT_NAME:-workflow_dispatch}"
selected="${JOURNEY:-all}"
paths="$(cat)"

run_fixture=false
run_ldap=false
run_dynamic=false
run_recovery=false
run_notification=false
run_secrets=false
run_delegated=false
run_fleet=false
run_readiness=false

select_all() {
  run_fixture=true
  run_ldap=true
  run_dynamic=true
  run_recovery=true
  run_notification=true
  run_secrets=true
  run_delegated=true
  run_fleet=true
  run_readiness=true
}

select_journey() {
  case "$1" in
    all) select_all ;;
    fixture) run_fixture=true ;;
    ldap) run_fixture=true; run_ldap=true ;;
    dynamic-inventory) run_fixture=true; run_dynamic=true ;;
    execution-recovery) run_fixture=true; run_recovery=true ;;
    notification-delivery) run_fixture=true; run_notification=true ;;
    secrets) run_fixture=true; run_secrets=true ;;
    delegated-api) run_delegated=true ;;
    fleet-scale) run_fixture=true; run_fleet=true ;;
    *) echo "error: unsupported product-validation journey '$1'" >&2; exit 2 ;;
  esac
}

if [[ "$event" != pull_request ]]; then
  select_journey "$selected"
else
  # Generic cluster/chart/harness changes can affect every journey. Specific
  # journey scripts select only their own proof so a focused fix stays focused.
  if grep -Eq '^(\.github/workflows/product-validation-fixture\.yml|deployments/(product-validation/|helm/praetor-v2/)|scripts/(bootstrap-product-validation-base|check-product-validation-fast|classify-product-validation|plan-product-validation|product-validation-fixture|generate-readiness-report)\.sh|cmd/readiness-report/|internal/readiness/)' <<<"$paths"; then
    select_all
  else
    grep -Eq '^scripts/validate-ldap-operator-journey\.sh$' <<<"$paths" && { run_fixture=true; run_ldap=true; }
    grep -Eq '^(scripts/validate-dynamic-inventory-e2e\.sh|tests/(dynamic_inventory_staging_contract|inventory_sync_history_contract)_test\.go)$' <<<"$paths" && { run_fixture=true; run_dynamic=true; }
    grep -Eq '^(scripts/validate-execution-recovery-e2e\.sh|playbooks/validate-execution-recovery\.yml)$' <<<"$paths" && { run_fixture=true; run_recovery=true; }
    grep -Eq '^(scripts/validate-notification-delivery-e2e\.sh|tests/notification_delivery_staging_contract_test\.go|web/components/NotificationDeliveryHistory\.test\.tsx)$' <<<"$paths" && { run_fixture=true; run_notification=true; }
    grep -Eq '^scripts/test-secrets-execution-e2e\.sh$' <<<"$paths" && { run_fixture=true; run_secrets=true; }
    grep -Eq '^scripts/validate-delegated-api-e2e\.sh$' <<<"$paths" && run_delegated=true
    grep -Eq '^(scripts/validate-fleet-scale-(e2e|live)\.sh|tests/fleet_scale_validation_contract_test\.go|services/api/handlers/(bulk.*|delegated_launch.*)\.go|web/(components/ui/BulkSelection(\.test)?|pages/(FleetScaleJourney\.test|TemplatesPage)|services/api\.bulk\.test)\.tsx?)$' <<<"$paths" && { run_fixture=true; run_fleet=true; }
  fi
fi

run_cluster=false
if [[ "$run_fixture" == true || "$run_ldap" == true || "$run_dynamic" == true || "$run_recovery" == true || "$run_notification" == true || "$run_secrets" == true ]]; then
  run_cluster=true
fi

printf 'run_cluster=%s\n' "$run_cluster"
printf 'run_fixture=%s\n' "$run_fixture"
printf 'run_ldap=%s\n' "$run_ldap"
printf 'run_dynamic=%s\n' "$run_dynamic"
printf 'run_recovery=%s\n' "$run_recovery"
printf 'run_notification=%s\n' "$run_notification"
printf 'run_secrets=%s\n' "$run_secrets"
printf 'run_delegated=%s\n' "$run_delegated"
printf 'run_fleet=%s\n' "$run_fleet"
printf 'run_readiness=%s\n' "$run_readiness"
