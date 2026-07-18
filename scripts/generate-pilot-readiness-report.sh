#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA_ROOT="${PRAETOR_STAGING_DATA_ROOT:-$HOME/.local/share/praetor/staging}"
EVIDENCE_DIR="${PRAETOR_PILOT_EVIDENCE_DIR:-$DATA_ROOT/pilot/evidence}"
OUTPUT="${PRAETOR_PILOT_READINESS_REPORT:-$DATA_ROOT/pilot/managed-host-pilot-readiness.json}"
LOCK="${PRAETOR_STAGING_RELEASE_LOCK:-$ROOT/deployments/staging/release-lock.yaml}"
SECRETS_REVISION="${PRAETOR_SECRETS_REVISION:-}"
FINDINGS_OVERRIDE="${PRAETOR_PILOT_FINDINGS_FILE:-}"

die() { echo "error: $*" >&2; exit 1; }
for command in git gh go jq kubectl shasum; do command -v "$command" >/dev/null || die "$command is required"; done
for evidence in managed-host-journey managed-host-faults credential-faults; do
  [[ -s "$EVIDENCE_DIR/$evidence.json" ]] || die "missing pilot evidence $EVIDENCE_DIR/$evidence.json"
done

work="$(mktemp -d "${TMPDIR:-/tmp}/praetor-pilot-readiness.XXXXXX")"
trap 'rm -rf "$work"' EXIT
umask 077

cp "$EVIDENCE_DIR/managed-host-journey.json" "$work/managed-host-pilot.json"
cp "$EVIDENCE_DIR/managed-host-faults.json" "$work/managed-host-pilot-faults.json"
cp "$EVIDENCE_DIR/credential-faults.json" "$work/secrets-service.json"

praetor_revision="$(jq -er '.revisions.source' "$work/managed-host-pilot.json")"
fixture_revision="$(jq -er '.source_revision' "$work/managed-host-pilot-faults.json")"
execution_pack="$(jq -er '.revisions.execution_pack' "$work/managed-host-pilot.json")"
target_image="$(jq -er '.revisions.target_image' "$work/managed-host-pilot.json")"

if [[ -z "$SECRETS_REVISION" ]]; then
  image="$(kubectl --context "${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}" -n "${PRAETOR_STAGING_NAMESPACE:-praetor-staging}" get deployment praetor-secrets -o jsonpath='{.spec.template.spec.containers[0].image}')"
  short_revision="${image##*:sha-}"
  [[ "$short_revision" =~ ^[0-9a-f]{7,40}$ ]] || die "deployed Secrets Service image is not source-pinned: $image"
  SECRETS_REVISION="$(gh api "repos/Niftel/praetor-secrets/commits/$short_revision" --jq .sha)"
fi

awk '
  /^components:/ { components=1; next }
  components && /^  [a-z][a-z-]*:/ { name=$1; sub(/:$/, "", name) }
  components && /^    digest: sha256:/ { print name "\t" $2 }
' "$LOCK" | jq -Rn '[inputs | split("\t") | {(.[0]):.[1]}] | add' >"$work/components.json"
jq -e 'type == "object" and length == 8 and all(.[]; test("^sha256:[0-9a-f]{64}$"))' "$work/components.json" >/dev/null || die "release lock did not yield eight pinned component digests"

if [[ -n "$FINDINGS_OVERRIDE" ]]; then
  cp "$FINDINGS_OVERRIDE" "$work/findings.json"
else
  gh issue list --repo Niftel/praetor --state open --milestone "Pilot Readiness" --limit 100 --json number,title,url |
    jq '[.[] | select(.number != 173 and .number != 178) | {id:("pilot-blocker-" + (.number|tostring)),category:"reliability",classification:"release-blocking",status:"open",issue:.url}]' >"$work/findings.json"
fi
jq -e 'type == "array"' "$work/findings.json" >/dev/null || die "pilot findings must be a JSON array"

PRAETOR_READINESS_PROFILE=managed-host-pilot \
PRAETOR_READINESS_EVIDENCE_DIR="$work" \
PRAETOR_READINESS_REPORT="$OUTPUT" \
PRAETOR_READINESS_FINDINGS_FILE="$work/findings.json" \
PRAETOR_READINESS_COMPONENTS_FILE="$work/components.json" \
PRAETOR_REVISION="$praetor_revision" \
PRAETOR_SECRETS_REVISION="$SECRETS_REVISION" \
PRAETOR_FIXTURE_REVISION="$fixture_revision" \
PRAETOR_EXECUTION_PACK_REVISION="$execution_pack" \
PRAETOR_TARGET_IMAGE_REVISION="$target_image" \
  "$ROOT/scripts/generate-readiness-report.sh"

chmod 0600 "$OUTPUT"
if grep -Eiq '(bearer |password|private.?key|BEGIN [A-Z ]+ KEY|172\.29\.)' "$OUTPUT"; then
  die "sensitive material appeared in pilot readiness report"
fi
echo "managed-host pilot decision: $OUTPUT"
