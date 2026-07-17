#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMMAND="${1:-}"
ARCHIVE="${2:-}"
CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}"
NAMESPACE="${PRAETOR_STAGING_NAMESPACE:-praetor-staging}"
RELEASE="${PRAETOR_STAGING_RELEASE:-praetor-staging}"
RESTORE_NAMESPACE="${PRAETOR_RESTORE_NAMESPACE:-praetor-staging-restore}"
RESTORE_RELEASE="${PRAETOR_RESTORE_RELEASE:-praetor-restore}"
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-$HOME/.local/share/praetor/staging}"
RECOVERY_ROOT="${PRAETOR_STAGING_RECOVERY_ROOT:-$DATA_ROOT/recovery}"
RECIPIENT_CERT="${PRAETOR_BACKUP_RECIPIENT_CERT:-$RECOVERY_ROOT/recipient.crt}"
RECIPIENT_KEY="${PRAETOR_BACKUP_RECIPIENT_KEY:-$RECOVERY_ROOT/recipient.key}"
CHART="$ROOT/deployments/helm/praetor-v2"
LOCK="$ROOT/deployments/staging/release-lock.yaml"
VALUES="$ROOT/deployments/staging/values.yaml"
POSTGRES_IMAGE="postgres:15@sha256:74e110c41804365e3915fcc09d5e7a1eff50161aaa94d5da0e58e0cd75ae509c"
NATS_IMAGE="nats:2.10-alpine@sha256:b83efabe3e7def1e0a4a31ec6e078999bb17c80363f881df35edc70fcb6bb927"

usage() {
  cat >&2 <<EOF
usage: $0 <plan|init-recipient|backup [archive.cms]|verify archive.cms|restore archive.cms|exercise>

  plan            describe the non-mutating recovery journey
  init-recipient  create a local recovery certificate and private key (0600)
  backup          create a CMS-encrypted, integrity-manifested staging backup
  verify          decrypt privately and verify every recorded SHA-256 digest
  restore         restore into an isolated namespace and start a healthy Praetor release
  exercise        prove a supported Helm rollback and locked re-upgrade on staging
EOF
  exit 2
}

die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command '$1' is not installed"; }
for tool in kubectl helm jq openssl tar shasum go; do need "$tool"; done
[[ "$COMMAND" =~ ^(plan|init-recipient|backup|verify|restore|exercise)$ ]] || usage
umask 077

source_pod() {
  local selector="$1"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" get pods -l "$selector" \
    --field-selector=status.phase=Running -o jsonpath='{.items[0].metadata.name}'
}

praetor_count_query="SELECT 'credential_references',count(*) FROM credentials WHERE secrets_service_id IS NOT NULL UNION ALL SELECT 'credentials',count(*) FROM credentials UNION ALL SELECT 'hosts',count(*) FROM hosts UNION ALL SELECT 'inventories',count(*) FROM inventories UNION ALL SELECT 'organizations',count(*) FROM organizations UNION ALL SELECT 'teams',count(*) FROM teams UNION ALL SELECT 'users',count(*) FROM users UNION ALL SELECT 'workflow_jobs',count(*) FROM workflow_jobs UNION ALL SELECT 'workflow_templates',count(*) FROM workflow_templates ORDER BY 1"

source_praetor_counts() {
  local pod
  pod="$(source_pod "app.kubernetes.io/instance=$RELEASE,app.kubernetes.io/component=postgresql")"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$pod" -- \
    psql -U postgres -d praetor -At -F, -c "$praetor_count_query" |
    jq -Rn '[inputs | split(",") | {(.[0]): (.[1]|tonumber)}] | add'
}

plan() {
  cat <<EOF
Praetor staging recovery plan
  source:       $CONTEXT / $NAMESPACE / Helm release $RELEASE
  encryption:   CMS EnvelopedData, AES-256-CBC, certificate recipient
  databases:    Praetor, Secrets Service, audit (pg_dump custom format)
  objects:      LDAP directory, NATS JetStream, executor jobs/packs/SSH state
  restore:      isolated namespace $RESTORE_NAMESPACE / release $RESTORE_RELEASE
  rollback:     latest prior successful Helm revision only; arbitrary schema downgrade is forbidden
  evidence:     counts and SHA-256 digests only; archives never enter workflow artifacts
EOF
}

init_recipient() {
  install -d -m 0700 "$RECOVERY_ROOT"
  [[ ! -e "$RECIPIENT_KEY" && ! -e "$RECIPIENT_CERT" ]] || die "recipient already exists below $RECOVERY_ROOT"
  openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 3650 \
    -subj '/CN=Praetor staging recovery/' -keyout "$RECIPIENT_KEY" -out "$RECIPIENT_CERT" >/dev/null 2>&1
  chmod 0600 "$RECIPIENT_KEY" "$RECIPIENT_CERT"
  echo "created recovery recipient certificate; keep $RECIPIENT_KEY outside source control and backups"
}

decrypt_archive() {
  local archive="$1" output="$2"
  [[ -s "$archive" ]] || die "backup archive does not exist or is empty: $archive"
  [[ -s "$RECIPIENT_CERT" && -s "$RECIPIENT_KEY" ]] || die "recovery recipient is missing; run init-recipient"
  openssl cms -decrypt -binary -inform DER -in "$archive" \
    -recip "$RECIPIENT_CERT" -inkey "$RECIPIENT_KEY" -out "$output"
}

verify_tree() {
  local directory="$1"
  (cd "$directory" && shasum -a 256 -c SHA256SUMS) >/dev/null || die "backup integrity verification failed"
  jq -e '.schema_version == 1 and .encrypted == true and (.stores | length == 7)' \
    "$directory/evidence.json" >/dev/null || die "backup evidence envelope is invalid"
}

backup() (
  [[ -s "$RECIPIENT_CERT" ]] || die "recovery recipient certificate is missing; run init-recipient"
  "$ROOT/scripts/staging-integrations.sh" status >/dev/null
  "$ROOT/scripts/staging-release.sh" status >/dev/null
  install -d -m 0700 "$RECOVERY_ROOT/backups"
  local created work archive main_db secrets_db audit_db ldap nats executor
  created="$(date -u +%Y%m%dT%H%M%SZ)"
  archive="${ARCHIVE:-$RECOVERY_ROOT/backups/praetor-staging-$created.cms}"
  [[ ! -e "$archive" ]] || die "refusing to overwrite backup: $archive"
  work="$(mktemp -d "${TMPDIR:-/tmp}/praetor-recovery-backup.XXXXXX")"
  trap 'find "$work" -depth -delete 2>/dev/null || true' EXIT

  main_db="$(source_pod "app.kubernetes.io/instance=$RELEASE,app.kubernetes.io/component=postgresql")"
  secrets_db="$(source_pod 'app=praetor-staging-secrets-postgres')"
  audit_db="$(source_pod 'app=praetor-staging-audit-postgres')"
  ldap="$(source_pod 'app=praetor-staging-ldap')"
  nats="$(source_pod "app.kubernetes.io/instance=$RELEASE,app.kubernetes.io/component=nats")"
  executor="$(source_pod "app.kubernetes.io/instance=$RELEASE,app.kubernetes.io/component=executor")"
  [[ -n "$main_db$secrets_db$audit_db$ldap$nats$executor" ]] || die "one or more persistent staging pods are unavailable"

  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$main_db" -- pg_dump -U postgres -d praetor -Fc >"$work/praetor.dump"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$secrets_db" -- pg_dump -U postgres -d praetor_secrets -Fc >"$work/secrets.dump"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$audit_db" -- pg_dump -U postgres -d praetor_audit -Fc >"$work/audit.dump"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$ldap" -- slapcat -n 1 >"$work/ldap.ldif"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$nats" -- tar -C /data -czf - jetstream >"$work/nats-jetstream.tar.gz"
  kubectl --context "$CONTEXT" -n "$NAMESPACE" exec "$executor" -- \
    tar -czf - /var/lib/praetor /opt/praetor/packs /home/praetor/.ssh >"$work/executor-state.tar.gz"

  source_praetor_counts >"$work/praetor-counts.json"
  (cd "$work" && shasum -a 256 audit.dump executor-state.tar.gz ldap.ldif nats-jetstream.tar.gz praetor-counts.json praetor.dump secrets.dump >SHA256SUMS)
  jq -n --arg created_at "$created" --arg context "$CONTEXT" --arg namespace "$NAMESPACE" --arg release "$RELEASE" \
    '{schema_version:1,created_at:$created_at,source:{context:$context,namespace:$namespace,release:$release},encrypted:true,stores:["praetor-postgres","secrets-postgres","audit-postgres","ldap","nats-jetstream","executor-state","integrity-counts"]}' \
    >"$work/evidence.json"
  (cd "$work" && tar -cf - .) | openssl cms -encrypt -binary -aes-256-cbc -outform DER -out "$archive" "$RECIPIENT_CERT"
  chmod 0600 "$archive"
  [[ -s "$archive" ]] || die "encrypted backup was not created"
  echo "encrypted staging backup created: $archive"
)

verify_backup() (
  local work bundle
  [[ -n "$ARCHIVE" ]] || usage
  work="$(mktemp -d "${TMPDIR:-/tmp}/praetor-recovery-verify.XXXXXX")"
  bundle="$work/bundle.tar"
  trap 'find "$work" -depth -delete 2>/dev/null || true' EXIT
  decrypt_archive "$ARCHIVE" "$bundle"
  tar -xf "$bundle" -C "$work"
  rm -f "$bundle"
  verify_tree "$work"
  echo "backup decrypts and every integrity digest matches"
)

restore() (
  [[ -n "$ARCHIVE" ]] || usage
  local work bundle generated source_secret
  local started_at started_epoch duration evidence_dir evidence
  started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  started_epoch="$(date +%s)"
  work="$(mktemp -d "${TMPDIR:-/tmp}/praetor-recovery-restore.XXXXXX")"
  bundle="$work/bundle.tar"
  generated="$work/locked-values.yaml"
  trap 'find "$work" -depth -delete 2>/dev/null || true' EXIT
  decrypt_archive "$ARCHIVE" "$bundle"
  tar -xf "$bundle" -C "$work" && rm -f "$bundle"
  verify_tree "$work"

  kubectl --context "$CONTEXT" create namespace "$RESTORE_NAMESPACE" --dry-run=client -o yaml | kubectl --context "$CONTEXT" apply -f - >/dev/null
  for secret in praetor-staging-registry; do
    kubectl --context "$CONTEXT" -n "$NAMESPACE" get secret "$secret" -o json |
      jq --arg namespace "$RESTORE_NAMESPACE" 'del(.metadata.uid,.metadata.resourceVersion,.metadata.creationTimestamp,.metadata.managedFields) | .metadata.namespace=$namespace' |
      kubectl --context "$CONTEXT" apply -f - >/dev/null
  done
  source_secret="$(kubectl --context "$CONTEXT" -n "$NAMESPACE" get secret praetor-staging-runtime -o json)"
  jq --arg namespace "$RESTORE_NAMESPACE" --arg url "postgres://postgres:postgres@praetor-restore-postgres:5432/praetor?sslmode=disable" \
    'del(.metadata.uid,.metadata.resourceVersion,.metadata.creationTimestamp,.metadata.managedFields) | .metadata.name="praetor-restore-runtime" | .metadata.namespace=$namespace | .data.DATABASE_URL=($url|@base64)' \
    <<<"$source_secret" | kubectl --context "$CONTEXT" apply -f - >/dev/null

  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" delete pod praetor-restore-postgres praetor-restore-nats praetor-restore-ldap-check --ignore-not-found --wait=true >/dev/null
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" run praetor-restore-postgres --image="$POSTGRES_IMAGE" --env=POSTGRES_PASSWORD=postgres --env=POSTGRES_DB=praetor --port=5432 >/dev/null
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" expose pod praetor-restore-postgres --port=5432 >/dev/null 2>&1 || true
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" wait --for=condition=Ready pod/praetor-restore-postgres --timeout=180s >/dev/null
  ready=false
  consecutive=0
  for _ in $(seq 1 60); do
    if kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" exec praetor-restore-postgres -- pg_isready -U postgres -d praetor >/dev/null 2>&1; then
      consecutive=$((consecutive + 1))
      if (( consecutive >= 3 )); then
        ready=true
        break
      fi
    else
      consecutive=0
    fi
    sleep 2
  done
  [[ "$ready" == true ]] || die "isolated restore PostgreSQL did not become ready within 120 seconds"
  for database in secrets audit; do
    kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" exec praetor-restore-postgres -- createdb -U postgres "praetor_$database"
  done
  for item in praetor secrets audit; do
    kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" cp "$work/$item.dump" "praetor-restore-postgres:/tmp/$item.dump"
    db="$item"; [[ "$item" == praetor ]] || db="praetor_$item"
    kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" exec praetor-restore-postgres -- pg_restore -U postgres -d "$db" --no-owner --exit-on-error "/tmp/$item.dump" >/dev/null
  done
  restored_counts="$(kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" exec praetor-restore-postgres -- psql -U postgres -d praetor -At -F, -c \
    "$praetor_count_query" |
    jq -Rn '[inputs | split(",") | {(.[0]): (.[1]|tonumber)}] | add')"
  jq -e --argjson restored "$restored_counts" '. == $restored' "$work/praetor-counts.json" >/dev/null || die "restored Praetor integrity counts differ from backup"

  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" apply -f - >/dev/null <<YAML
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: praetor-restore-nats}
spec: {accessModes: [ReadWriteOnce], storageClassName: local-path, resources: {requests: {storage: 1Gi}}}
---
apiVersion: v1
kind: Pod
metadata: {name: praetor-restore-nats, labels: {app: praetor-restore-nats}}
spec:
  containers:
    - name: nats
      image: $NATS_IMAGE
      command: [sh, -c, "sleep 3600"]
      volumeMounts: [{name: data, mountPath: /data}]
  volumes: [{name: data, persistentVolumeClaim: {claimName: praetor-restore-nats}}]
YAML
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" wait --for=condition=Ready pod/praetor-restore-nats --timeout=180s >/dev/null
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" cp "$work/nats-jetstream.tar.gz" praetor-restore-nats:/tmp/nats-jetstream.tar.gz
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" exec praetor-restore-nats -- tar -xzf /tmp/nats-jetstream.tar.gz -C /data
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" delete pod praetor-restore-nats --wait=true >/dev/null
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" apply -f - >/dev/null <<YAML
apiVersion: v1
kind: Pod
metadata: {name: praetor-restore-nats, labels: {app: praetor-restore-nats}}
spec:
  containers:
    - name: nats
      image: $NATS_IMAGE
      args: [-js, -sd, /data/jetstream, -m, "8222"]
      ports: [{name: client, containerPort: 4222}]
      readinessProbe: {tcpSocket: {port: client}, periodSeconds: 2}
      volumeMounts: [{name: data, mountPath: /data}]
  volumes: [{name: data, persistentVolumeClaim: {claimName: praetor-restore-nats}}]
YAML
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" expose pod praetor-restore-nats --port=4222 >/dev/null 2>&1 || true
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" wait --for=condition=Ready pod/praetor-restore-nats --timeout=180s >/dev/null

  # Validate the recovered directory backup with the same OpenLDAP tooling in
  # the isolated namespace. The source directory remains untouched.
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" run praetor-restore-ldap-check \
    --image=osixia/openldap@sha256:18742e9c449c9c1afe129d3f2f3ee15fb34cc43e5f940a20f3399728f41d7c28 \
    --env=LDAP_DOMAIN=praetor.local --env=LDAP_ADMIN_PASSWORD=restore-validation-only >/dev/null
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" wait --for=condition=Ready pod/praetor-restore-ldap-check --timeout=180s >/dev/null
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" cp "$work/ldap.ldif" praetor-restore-ldap-check:/tmp/restore.ldif
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" exec praetor-restore-ldap-check -- \
    slapadd -u -F /etc/ldap/slapd.d -n 1 -l /tmp/restore.ldif >/dev/null

  go run "$ROOT/cmd/stagingrelease" -lock "$LOCK" -output helm-values >"$generated"
  helm upgrade --install "$RESTORE_RELEASE" "$CHART" --kube-context "$CONTEXT" -n "$RESTORE_NAMESPACE" \
    -f "$VALUES" -f "$generated" \
    --set secrets.existingSecret=praetor-restore-runtime \
    --set database.external.url=postgres://postgres:postgres@praetor-restore-postgres:5432/praetor?sslmode=disable \
    --set database.bundled.enabled=false --set nats.external.url=nats://praetor-restore-nats:4222 --set nats.bundled.enabled=false \
    --set ldap.enabled=false --set ldap.existingConfigMap= --set secretsService.enabled=false --set ingress.enabled=false \
    --set hostRunner.callbackUrl=http://$RESTORE_RELEASE-ingestion:8081 --wait --timeout 10m >/dev/null
  kubectl --context "$CONTEXT" -n "$RESTORE_NAMESPACE" rollout status deployment/$RESTORE_RELEASE-api --timeout=180s >/dev/null
  duration=$(( $(date +%s) - started_epoch ))
  evidence_dir="$RECOVERY_ROOT/evidence"
  install -d -m 0700 "$evidence_dir"
  evidence="$evidence_dir/restore-$(date -u +%Y%m%dT%H%M%SZ).json"
  jq -n --arg started_at "$started_at" --arg completed_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg archive_sha256 "$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')" \
    --arg namespace "$RESTORE_NAMESPACE" --arg release "$RESTORE_RELEASE" --argjson duration_seconds "$duration" \
    --argjson counts "$restored_counts" \
    '{schema_version:1,journey:"staging-restore",result:"pass",started_at:$started_at,completed_at:$completed_at,duration_seconds:$duration_seconds,archive_sha256:$archive_sha256,restore:{namespace:$namespace,release:$release},integrity_counts:$counts}' \
    >"$evidence"
  chmod 0600 "$evidence"
  echo "isolated restore is healthy and all backed-up Praetor integrity counts match: namespace $RESTORE_NAMESPACE; sanitized evidence: $evidence"
)

exercise() {
  "$ROOT/scripts/staging-release.sh" status >/dev/null
  local current previous before rollback_counts upgraded_counts started_at started_epoch duration evidence_dir evidence
  started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  started_epoch="$(date +%s)"
  before="$(source_praetor_counts)"
  current="$(helm status "$RELEASE" --kube-context "$CONTEXT" -n "$NAMESPACE" -o json | jq -r .version)"
  previous="$(helm history "$RELEASE" --kube-context "$CONTEXT" -n "$NAMESPACE" -o json | jq -r --argjson current "$current" '[.[] | select(.revision < $current and .status == "superseded")] | last | .revision // empty')"
  [[ -n "$previous" ]] || die "no prior successful Helm revision exists; unsupported arbitrary downgrade will not be attempted"
  helm rollback "$RELEASE" "$previous" --kube-context "$CONTEXT" -n "$NAMESPACE" --wait --timeout 10m >/dev/null
  "$ROOT/scripts/staging-integrations.sh" status >/dev/null
  rollback_counts="$(source_praetor_counts)"
  jq -e --argjson observed "$rollback_counts" '. == $observed' <<<"$before" >/dev/null || die "supported rollback changed protected application-state counts"
  "$ROOT/scripts/staging-release.sh" deploy >/dev/null
  "$ROOT/scripts/staging-release.sh" status >/dev/null
  upgraded_counts="$(source_praetor_counts)"
  jq -e --argjson observed "$upgraded_counts" '. == $observed' <<<"$before" >/dev/null || die "locked re-upgrade changed protected application-state counts"
  duration=$(( $(date +%s) - started_epoch ))
  evidence_dir="$RECOVERY_ROOT/evidence"
  install -d -m 0700 "$evidence_dir"
  evidence="$evidence_dir/upgrade-rollback-$(date -u +%Y%m%dT%H%M%SZ).json"
  jq -n --arg started_at "$started_at" --arg completed_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --argjson previous_revision "$previous" --argjson original_revision "$current" --argjson duration_seconds "$duration" \
    '{schema_version:1,journey:"staging-upgrade-rollback",result:"pass",started_at:$started_at,completed_at:$completed_at,duration_seconds:$duration_seconds,boundary:{previous_revision:$previous_revision,original_revision:$original_revision},preserved:["organizations","users","rbac-teams","inventories","hosts","workflows","execution-history","credentials","credential-references"]}' \
    >"$evidence"
  chmod 0600 "$evidence"
  echo "supported rollback to revision $previous and locked re-upgrade from revision $current preserved organizations, users, RBAC teams, inventories, hosts, workflows, execution history, credentials, and credential references; sanitized evidence: $evidence"
}

case "$COMMAND" in
  plan) plan ;;
  init-recipient) init_recipient ;;
  backup) backup ;;
  verify) verify_backup ;;
  restore) restore ;;
  exercise) exercise ;;
esac
