#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EVIDENCE_DIR="${PRAETOR_READINESS_EVIDENCE_DIR:-$ROOT/build/readiness-evidence}"
OUTPUT="${PRAETOR_READINESS_REPORT:-$ROOT/build/production-candidate-readiness.json}"
PRAETOR_REVISION="${PRAETOR_REVISION:-}"
SECRETS_REVISION="${PRAETOR_SECRETS_REVISION:-}"
FIXTURE_REVISION="${PRAETOR_FIXTURE_REVISION:-$PRAETOR_REVISION}"
FINDINGS_FILE="${PRAETOR_READINESS_FINDINGS_FILE:-}"
GENERATED_AT="${PRAETOR_READINESS_GENERATED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

die() { echo "error: $*" >&2; exit 1; }
for command in go jq shasum; do command -v "$command" >/dev/null || die "$command is required"; done
[[ -n "$PRAETOR_REVISION" && -n "$SECRETS_REVISION" ]] || die "PRAETOR_REVISION and PRAETOR_SECRETS_REVISION are required"

journeys=(ldap-operator secrets-service delegated-api execution-recovery)
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

jq -n \
  --arg generated_at "$GENERATED_AT" \
  --arg praetor "$PRAETOR_REVISION" \
  --arg secrets "$SECRETS_REVISION" \
  --arg fixture "$FIXTURE_REVISION" \
  --argjson journeys "$(cat "$work/journeys.json")" \
  --argjson findings "$(cat "$work/findings.json")" \
  '{schema_version:1,generated_at:$generated_at,revisions:{praetor:$praetor,secrets_service:$secrets,fixture:$fixture},journeys:$journeys,findings:$findings}' \
  >"$work/manifest.json"

mkdir -p "$(dirname "$OUTPUT")"
go run "$ROOT/cmd/readiness-report" -input "$work/manifest.json" -output "$OUTPUT"
echo "readiness report: $OUTPUT"
