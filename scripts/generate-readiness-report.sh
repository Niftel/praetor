#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EVIDENCE_DIR="${PRAETOR_READINESS_EVIDENCE_DIR:-$ROOT/build/readiness-evidence}"
OUTPUT="${PRAETOR_READINESS_REPORT:-$ROOT/build/production-candidate-readiness.json}"
PRAETOR_REVISION="${PRAETOR_REVISION:-}"
SECRETS_REVISION="${PRAETOR_SECRETS_REVISION:-}"
FIXTURE_REVISION="${PRAETOR_FIXTURE_REVISION:-$PRAETOR_REVISION}"
FINDINGS_FILE="${PRAETOR_READINESS_FINDINGS_FILE:-}"
COMPONENTS_FILE="${PRAETOR_READINESS_COMPONENTS_FILE:-}"
EXECUTION_PACK_REVISION="${PRAETOR_EXECUTION_PACK_REVISION:-}"
TARGET_IMAGE_REVISION="${PRAETOR_TARGET_IMAGE_REVISION:-}"
PROFILE="${PRAETOR_READINESS_PROFILE:-product-validation}"
GENERATED_AT="${PRAETOR_READINESS_GENERATED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

die() { echo "error: $*" >&2; exit 1; }
for command in go jq shasum; do command -v "$command" >/dev/null || die "$command is required"; done
[[ -n "$PRAETOR_REVISION" && -n "$SECRETS_REVISION" ]] || die "PRAETOR_REVISION and PRAETOR_SECRETS_REVISION are required"

journeys=(ldap-operator secrets-service delegated-api execution-recovery)
if [[ "$PROFILE" == staging-release-candidate ]]; then
  journeys+=(staging-health staging-recovery ui-acceptance)
  [[ -n "$COMPONENTS_FILE" ]] || die "PRAETOR_READINESS_COMPONENTS_FILE is required for the staging-release-candidate profile"
elif [[ "$PROFILE" == managed-host-pilot ]]; then
  journeys=(managed-host-pilot managed-host-pilot-faults secrets-service)
  [[ -n "$COMPONENTS_FILE" ]] || die "PRAETOR_READINESS_COMPONENTS_FILE is required for the managed-host-pilot profile"
  [[ -n "$EXECUTION_PACK_REVISION" && -n "$TARGET_IMAGE_REVISION" ]] || die "pilot execution-pack and target-image revisions are required"
elif [[ "$PROFILE" != product-validation ]]; then
  die "unsupported readiness profile $PROFILE"
fi
work="$(mktemp -d "${TMPDIR:-/tmp}/praetor-readiness.XXXXXX")"
trap 'rm -rf "$work"' EXIT
umask 077
printf '[]\n' >"$work/journeys.json"

for journey in "${journeys[@]}"; do
  evidence="$EVIDENCE_DIR/$journey.json"
  [[ -s "$evidence" ]] || die "missing evidence artifact $evidence"
  jq -e --arg journey "$journey" \
    '.schema_version == 1 and .journey == $journey and (.result == "pass" or .result == "fail" or .result == "not-applicable")' \
    "$evidence" >/dev/null || die "invalid evidence envelope for $journey"
  if jq -e '[paths as $p | $p[-1] | select(type == "string" and test("token|password|private.?key|bind.?dn|secret.?value"; "i"))] | length > 0' "$evidence" >/dev/null; then
    die "sensitive field name detected in $journey evidence"
  fi
  result="$(jq -er .result "$evidence")"
  digest="$(shasum -a 256 "$evidence" | awk '{print $1}')"
  jq --arg name "$journey" --arg result "$result" --arg digest "$digest" \
    '. + [{name:$name,result:$result,evidence_sha256:$digest}]' "$work/journeys.json" >"$work/next.json"
  mv "$work/next.json" "$work/journeys.json"
done

if [[ -n "$FINDINGS_FILE" ]]; then
  jq -e 'type == "array"' "$FINDINGS_FILE" >/dev/null || die "findings file must contain a JSON array"
  cp "$FINDINGS_FILE" "$work/findings.json"
else
  printf '[]\n' >"$work/findings.json"
fi

if [[ -n "$COMPONENTS_FILE" ]]; then
  jq -e 'type == "object" and all(.[]; type == "string")' "$COMPONENTS_FILE" >/dev/null || die "components file must contain a JSON object"
  cp "$COMPONENTS_FILE" "$work/components.json"
else
  printf '{}\n' >"$work/components.json"
fi

jq -n \
  --arg generated_at "$GENERATED_AT" \
  --arg profile "$PROFILE" \
  --arg praetor "$PRAETOR_REVISION" \
  --arg secrets "$SECRETS_REVISION" \
  --arg fixture "$FIXTURE_REVISION" \
  --arg execution_pack "$EXECUTION_PACK_REVISION" \
  --arg target_image "$TARGET_IMAGE_REVISION" \
  --argjson journeys "$(cat "$work/journeys.json")" \
  --argjson findings "$(cat "$work/findings.json")" \
  --argjson components "$(cat "$work/components.json")" \
  '{schema_version:1,profile:$profile,generated_at:$generated_at,revisions:{praetor:$praetor,secrets_service:$secrets,fixture:$fixture,components:$components,execution_pack:$execution_pack,target_image:$target_image},journeys:$journeys,findings:$findings}' \
  >"$work/manifest.json"

mkdir -p "$(dirname "$OUTPUT")"
go build -o "$work/readiness-report" "$ROOT/cmd/readiness-report"
set +e
"$work/readiness-report" -input "$work/manifest.json" -output "$OUTPUT"
status=$?
set -e
echo "readiness report: $OUTPUT"
exit "$status"
