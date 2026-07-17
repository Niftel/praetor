#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SECRETS_ROOT="${PRAETOR_SECRETS_ROOT:-$ROOT/../praetor-secrets}"
CLUSTER="${PRAETOR_STAGING_CLUSTER:-praetor-staging}"
CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-$CLUSTER}"
NAMESPACE="praetor-staging"
RELEASE="praetor-staging"
TRUST_DOMAIN="staging.praetor.local"
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-$HOME/.local/share/praetor/staging}"
PKI_ROOT="$DATA_ROOT/pki"
SECRETS_IMAGE_REPOSITORY="ghcr.io/niftel/praetor-secrets"
SECRETS_IMAGE_TAG="${PRAETOR_STAGING_SECRETS_TAG:-sha-1e703aa}"
SECRETS_IMAGE_DIGEST="${PRAETOR_STAGING_SECRETS_DIGEST:-sha256:27914e653598964f2d0bd8de25d3e79e3f339f83defc0c8efa919f1daaf7150c}"
MANIFEST="$ROOT/deployments/staging/integrations.yaml"
LDAP_CONFIG="$ROOT/deployments/staging/ldap.yaml"

usage() { echo "usage: $0 <plan|bootstrap|status|verify>" >&2; exit 2; }
die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }

for tool in kubectl helm jq openssl mkcert go docker curl; do need "$tool"; done
[[ "${1:-}" =~ ^(plan|bootstrap|status|verify)$ ]] || usage

plan() {
  cat <<EOF
Praetor staging integration plan
  namespace:       $NAMESPACE
  browser TLS:     mkcert CA -> Secret/praetor-staging-ingress-tls
  LDAP:            verified LDAPS -> StatefulSet/praetor-staging-ldap
  Secrets Service: $SECRETS_IMAGE_REPOSITORY:$SECRETS_IMAGE_TAG@$SECRETS_IMAGE_DIGEST
  persistent DBs:  StatefulSet/praetor-staging-secrets-postgres, StatefulSet/praetor-staging-audit-postgres
  credentials:     generated below $PKI_ROOT (0700) and projected only through Kubernetes Secrets
EOF
}

apply_identity() {
  local name="$1" directory="$2"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" create secret generic "$name" \
    --from-file="$directory" --dry-run=client -o yaml | kubectl --context "$CONTEXT" apply -f - >/dev/null
}

bootstrap() {
  "$ROOT/scripts/staging-environment.sh" status >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" get secret praetor-staging-runtime >/dev/null ||
    die "praetor-staging-runtime must exist before integration bootstrap"
  [[ -f "$SECRETS_ROOT/cmd/praetor-dev-bootstrap/main.go" ]] || die "Secrets Service checkout not found at $SECRETS_ROOT"

  install -d -m 0700 "$PKI_ROOT"
  caroot="$(mkcert -CAROOT)"
  [[ -s "$caroot/rootCA.pem" ]] || die "mkcert root CA is missing; run 'mkcert -install' once"
  mkcert -cert-file "$PKI_ROOT/ingress.crt" -key-file "$PKI_ROOT/ingress.key" \
    praetor-staging.localhost ingest.praetor-staging.localhost localhost 127.0.0.1 ::1 >/dev/null
  mkcert -cert-file "$PKI_ROOT/ldap.crt" -key-file "$PKI_ROOT/ldap.key" \
    praetor-staging-ldap "praetor-staging-ldap.$NAMESPACE.svc" >/dev/null
  chmod 0600 "$PKI_ROOT"/*.key "$PKI_ROOT"/*.crt

  kubectl --context "$CONTEXT" -n "$NAMESPACE" create secret tls praetor-staging-ingress-tls \
    --cert="$PKI_ROOT/ingress.crt" --key="$PKI_ROOT/ingress.key" --dry-run=client -o yaml |
    kubectl --context "$CONTEXT" apply -f - >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" create secret generic praetor-staging-ldap-tls \
    --from-file=tls.crt="$PKI_ROOT/ldap.crt" --from-file=tls.key="$PKI_ROOT/ldap.key" \
    --from-file=ca.crt="$caroot/rootCA.pem" --dry-run=client -o yaml |
    kubectl --context "$CONTEXT" apply -f - >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" create configmap praetor-staging-ldap-config \
    --from-file=ldap.yaml="$LDAP_CONFIG" --from-file=ca.crt="$caroot/rootCA.pem" --dry-run=client -o yaml |
    kubectl --context "$CONTEXT" apply -f - >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" create configmap praetor-staging-ldap-seed \
    --from-file=bootstrap.ldif="$ROOT/deployments/ldap/bootstrap.ldif" --dry-run=client -o yaml |
    kubectl --context "$CONTEXT" apply -f - >/dev/null

  db_password_file="$PKI_ROOT/secrets-database-password"
  if [[ ! -s "$db_password_file" ]]; then
    openssl rand -hex 32 >"$db_password_file"
    chmod 0600 "$db_password_file"
  fi
  db_password="$(<"$db_password_file")"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" create secret generic praetor-staging-secrets-database \
    --from-file=password="$db_password_file" --dry-run=client -o yaml |
    kubectl --context "$CONTEXT" apply -f - >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" apply -f "$MANIFEST" >/dev/null
  for workload in statefulset/praetor-staging-ldap statefulset/praetor-staging-secrets-postgres statefulset/praetor-staging-audit-postgres; do
    kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status "$workload" --timeout=5m
  done
  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec praetor-staging-secrets-postgres-0 -- \
    sh -ec "psql -U postgres -Atqc \"SELECT 1 FROM pg_database WHERE datname='praetor_secrets'\" | grep -qx 1 || createdb -U postgres praetor_secrets"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec praetor-staging-audit-postgres-0 -- \
    sh -ec "psql -U postgres -Atqc \"SELECT 1 FROM pg_database WHERE datname='praetor_audit'\" | grep -qx 1 || createdb -U postgres praetor_audit"

  generated="$PKI_ROOT/generated-$RELEASE"
  if [[ ! -d "$generated" ]]; then
    secrets_url_file="$PKI_ROOT/secrets-database-url"
    audit_url_file="$PKI_ROOT/audit-database-url"
    printf 'postgres://postgres:%s@praetor-staging-secrets-postgres:5432/praetor_secrets?sslmode=disable\n' "$db_password" >"$secrets_url_file"
    printf 'postgres://postgres:%s@praetor-staging-audit-postgres:5432/praetor_audit?sslmode=disable\n' "$db_password" >"$audit_url_file"
    chmod 0600 "$secrets_url_file" "$audit_url_file"
    (cd "$SECRETS_ROOT" && GOCACHE="${TMPDIR:-/tmp}/praetor-staging-secrets-go-cache" go run ./cmd/praetor-dev-bootstrap \
      -output "$generated" -namespace "$NAMESPACE" -trust-domain "$TRUST_DOMAIN" \
      -scheduler-service-name "$RELEASE-scheduler" \
      -secrets-database-url-file "$secrets_url_file" -audit-database-url-file "$audit_url_file") >/dev/null
  fi
  "$generated/kubectl-secrets.sh" >/dev/null
  apply_identity praetor-api-identity "$generated/clients/praetor-api"
  apply_identity praetor-scheduler-identity "$generated/clients/praetor-scheduler"
  apply_identity praetor-executor-identity "$generated/clients/praetor-executor"

  remote="$(docker buildx imagetools inspect "$SECRETS_IMAGE_REPOSITORY:$SECRETS_IMAGE_TAG" --format '{{json .Manifest}}' | jq -r '.digest // .Digest')"
  [[ "$remote" == "$SECRETS_IMAGE_DIGEST" ]] || die "Secrets Service tag resolved to $remote, expected $SECRETS_IMAGE_DIGEST"
  target_arch="$(kubectl --context "$CONTEXT" get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')"
  platforms="$(docker buildx imagetools inspect "$SECRETS_IMAGE_REPOSITORY:$SECRETS_IMAGE_TAG" --format '{{json .Manifest}}' |
    jq -r '(.manifests // .Manifests)[] | (.platform.os // .Platform.OS) + "/" + (.platform.architecture // .Platform.Architecture)')"
  grep -Fxq "linux/$target_arch" <<<"$platforms" ||
    die "Secrets Service image has no linux/$target_arch manifest required by staging"
  helm dependency build "$SECRETS_ROOT/charts/praetor-secrets-stack" >/dev/null
  helm upgrade --install praetor-secrets-stack "$SECRETS_ROOT/charts/praetor-secrets-stack" \
    --kube-context "$CONTEXT" -n "$NAMESPACE" \
    --set praetor-secrets.image.repository="$SECRETS_IMAGE_REPOSITORY" \
    --set praetor-secrets.image.tag="$SECRETS_IMAGE_TAG" \
    --set 'praetor-secrets.image.pullSecrets[0].name=praetor-staging-registry' \
    --set praetor-secrets.trustDomain="$TRUST_DOMAIN" \
    --set praetor-secrets.secrets.runtimeSecret=praetor-secrets-runtime \
    --set praetor-secrets.secrets.serverTLSSecret=praetor-secrets-server \
    --set praetor-secrets.secrets.auditSinkTLSSecret=praetor-secrets-audit-client \
    --set praetor-audit-sink.image.repository="$SECRETS_IMAGE_REPOSITORY" \
    --set praetor-audit-sink.image.tag="$SECRETS_IMAGE_TAG" \
    --set 'praetor-audit-sink.image.pullSecrets[0].name=praetor-staging-registry' \
    --set praetor-audit-sink.trustDomain="$TRUST_DOMAIN" \
    --set praetor-audit-sink.secrets.runtimeSecret=praetor-audit-runtime \
    --set praetor-audit-sink.secrets.serverTLSSecret=praetor-audit-server \
    --wait --timeout 10m
  kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout restart \
    deployment/praetor-secrets deployment/praetor-audit-sink \
    deployment/$RELEASE-api deployment/$RELEASE-scheduler statefulset/$RELEASE-executor >/dev/null
  for workload in deployment/praetor-secrets deployment/praetor-audit-sink deployment/$RELEASE-api deployment/$RELEASE-scheduler statefulset/$RELEASE-executor; do
    kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status "$workload" --timeout=5m >/dev/null
  done
  status
}

status() {
  for workload in statefulset/praetor-staging-ldap statefulset/praetor-staging-secrets-postgres statefulset/praetor-staging-audit-postgres deployment/praetor-secrets deployment/praetor-audit-sink; do
    kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status "$workload" --timeout=180s >/dev/null || die "$workload is not healthy"
  done
  echo "healthy: staging TLS, LDAP, persistent Secrets Service databases, and workload identities are ready"
}

verify() {
  status
  ca="$(mkcert -CAROOT)/rootCA.pem"
  curl --fail --silent --show-error --cacert "$ca" "https://praetor-staging.localhost:8443/api/v1/ping" >/dev/null
  echo "verified: browser ingress certificate chains to the local trusted CA"
}

case "$1" in
  plan) plan ;;
  bootstrap) bootstrap ;;
  status) status ;;
  verify) verify ;;
esac
