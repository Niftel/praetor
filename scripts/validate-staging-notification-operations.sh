#!/usr/bin/env bash
set -Eeuo pipefail

# Runs the complete notification operations contract against the durable,
# release-pinned staging environment. The receiver and all mutable API fixtures
# are synthetic and staging-only; evidence contains bounded identifiers only.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMMAND="${1:-}"
CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}"
NAMESPACE="${PRAETOR_STAGING_NAMESPACE:-praetor-staging}"
RELEASE="${PRAETOR_STAGING_RELEASE:-praetor-staging}"
PASSWORD="${PRAETOR_STAGING_ACCEPTANCE_PASSWORD:-praetor123}"
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-$HOME/.local/share/praetor/staging}"
EVIDENCE_ROOT="$DATA_ROOT/acceptance/evidence"
EVIDENCE_FILE="$EVIDENCE_ROOT/notification-operations.json"
SINK="praetor-staging-acceptance-sink"

usage() {
  echo "usage: $0 <plan|status|run>" >&2
  exit 2
}

die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for command in helm jq kubectl; do need "$command"; done
[[ "$COMMAND" =~ ^(plan|status|run)$ ]] || usage
umask 077

plan() {
  cat <<EOF
Persistent staging notification readiness plan
  context:   $CONTEXT
  namespace: $NAMESPACE
  release:   $RELEASE
  receiver:  deployment/$SINK (staging-only)
  evidence:  $EVIDENCE_FILE

Checks:
  target test delivery; job, inventory-sync, and team-approval delivery
  producer and worker restart recovery; bounded transient retry
  permanent failure classification; logical deduplication
  team and organization history isolation; secret redaction
  fixture target/policy deletion and receiver-log cleanup
EOF
}

status() {
  "$ROOT/scripts/staging-acceptance.sh" status >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" get configmap "$SINK" -o json |
    jq -e '.data["default.conf"] | contains("location = /permanent { return 400; }")' >/dev/null ||
    die "staging receiver does not expose the deterministic permanent-failure route; run staging acceptance seed"
  echo "healthy: persistent staging notification prerequisites are ready"
}

reset_receiver() {
  kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout restart "deployment/$SINK" >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status "deployment/$SINK" --timeout=180s >/dev/null ||
    die "staging notification receiver did not become ready"
}

run() {
  install -d -m 0700 "$EVIDENCE_ROOT"
  "$ROOT/scripts/staging-acceptance.sh" seed >/dev/null
  status

  # Start from an empty in-container access log. The same restart after the
  # journey removes captured request bodies from the controlled receiver.
  reset_receiver

  PRAETOR_VALIDATION_CONTEXT="$CONTEXT" \
    PRAETOR_VALIDATION_NAMESPACE="$NAMESPACE" \
    PRAETOR_HELM_RELEASE="$RELEASE" \
    PRAETOR_VALIDATION_ADMIN_USERNAME=demo-operator \
    PRAETOR_VALIDATION_ADMIN_PASSWORD="$PASSWORD" \
    PRAETOR_VALIDATION_LDAP_PASSWORD="$PASSWORD" \
    PRAETOR_NOTIFICATION_SINK_DEPLOYMENT="$SINK" \
    PRAETOR_NOTIFICATION_SINK_SERVICE="$SINK" \
    PRAETOR_NOTIFICATION_EVIDENCE_FILE="$EVIDENCE_FILE" \
    "$ROOT/scripts/validate-notification-delivery-e2e.sh" >/dev/null

  jq -e '
    .result == "pass" and
    (.checks | index("target-test-delivery")) and
    (.checks | index("fixture-resource-cleanup")) and
    (.checks | index("notification-history-secret-redaction"))
  ' "$EVIDENCE_FILE" >/dev/null || die "notification readiness evidence is incomplete"
  if grep -Fq "notification-history-secret-canary" "$EVIDENCE_FILE"; then
    die "notification readiness evidence contains the secret canary"
  fi

  reset_receiver
  if kubectl --context "$CONTEXT" -n "$NAMESPACE" logs "deployment/$SINK" --since=10m |
    grep -Fq "Notification Delivery E2E"; then
    die "receiver request data remains after cleanup"
  fi

  revision="$(helm status "$RELEASE" --kube-context "$CONTEXT" -n "$NAMESPACE" -o json | jq -er .version)"
  recorded_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  enriched="$(mktemp "${TMPDIR:-/tmp}/praetor-staging-notification-evidence.XXXXXX")"
  jq --arg recorded_at "$recorded_at" --arg release "$RELEASE" --arg namespace "$NAMESPACE" \
    --argjson revision "$revision" '
      . + {
        environment:"persistent-staging",
        recorded_at:$recorded_at,
        release:$release,
        namespace:$namespace,
        helm_revision:$revision
      }
      | .checks += ["receiver-data-cleanup"]
    ' "$EVIDENCE_FILE" >"$enriched"
  mv "$enriched" "$EVIDENCE_FILE"
  chmod 0600 "$EVIDENCE_FILE"
  echo "persistent staging notification readiness passed; sanitized evidence: $EVIDENCE_FILE"
}

case "$COMMAND" in
  plan) plan ;;
  status) status ;;
  run) run ;;
esac
