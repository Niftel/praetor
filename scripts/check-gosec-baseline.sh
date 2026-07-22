#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPORT="${1:-$ROOT/.gosec/gosec.sarif}"
BASELINE="${GOSEC_BASELINE:-$ROOT/.github/gosec-high-baseline.json}"

for command in jq comm sort; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "error: required command '$command' is not installed" >&2
    exit 1
  }
done
[[ -s "$REPORT" ]] || { echo "error: gosec SARIF report is missing: $REPORT" >&2; exit 1; }
[[ -s "$BASELINE" ]] || { echo "error: gosec high-severity baseline is missing: $BASELINE" >&2; exit 1; }
jq -e '.version == 1 and (.findings | type == "array")' "$BASELINE" >/dev/null || {
  echo "error: unsupported gosec baseline schema" >&2
  exit 1
}
jq -e '.runs[0].tool.driver.rules and .runs[0].results' "$REPORT" >/dev/null || {
  echo "error: malformed gosec SARIF report" >&2
  exit 1
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

jq -S -c '.findings[]' "$BASELINE" | LC_ALL=C sort -u > "$tmp/baseline"
jq -S -c '
  (.runs[0].tool.driver.rules
    | map(select((.properties.tags // []) | index("HIGH")))
    | map(.id)) as $high_rules
  | .runs[0].results[]
  | select(.ruleId as $rule | $high_rules | index($rule))
  | {
      rule: .ruleId,
      file: .locations[0].physicalLocation.artifactLocation.uri,
      message: .message.text,
      snippet: .locations[0].physicalLocation.region.snippet.text
    }
' "$REPORT" | LC_ALL=C sort -u > "$tmp/current"

comm -13 "$tmp/baseline" "$tmp/current" > "$tmp/new"
comm -23 "$tmp/baseline" "$tmp/current" > "$tmp/resolved"

total="$(jq '[.runs[0].results[]] | length' "$REPORT")"
high="$(wc -l < "$tmp/current" | tr -d ' ')"
echo "gosec: $total total finding(s), $high high-severity finding(s)"

failed=0
if [[ -s "$tmp/new" ]]; then
  echo "error: new high-severity gosec finding(s):" >&2
  jq -r '"  \(.rule) \(.file): \(.message) — \(.snippet)"' "$tmp/new" >&2
  failed=1
fi
if [[ -s "$tmp/resolved" ]]; then
  echo "error: resolved gosec finding(s) remain in the baseline; remove these entries:" >&2
  jq -r '"  \(.rule) \(.file): \(.message) — \(.snippet)"' "$tmp/resolved" >&2
  failed=1
fi
if (( failed == 0 )); then
  echo "gosec: no new high-severity regressions and baseline is current"
fi
exit "$failed"
