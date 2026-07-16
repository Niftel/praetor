#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG="${PRAETOR_DEVELOPMENT_FLOW_CONFIG:-$ROOT/.github/development-flow.json}"
STATE_FILE="${PRAETOR_DEVELOPMENT_FLOW_STATE:-$ROOT/.github/development-flow-state.json}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "error: required command '$1' is not installed" >&2; exit 1; }; }
for command in gh jq; do need "$command"; done

OWNER="$(jq -r .owner "$CONFIG")"
REPO="$(jq -r .repository "$CONFIG")"
REPOSITORY="$OWNER/$REPO"

usage() {
  cat <<EOF
usage: $0 <bootstrap|validate-issue|sync-issue|sync-pr|verify-main>

  bootstrap       create labels, milestones, project, fields, and saved state
  validate-issue  validate EVENT_PATH issue content before accepting it
  sync-issue      add/update an issue in the project from EVENT_PATH
  sync-pr         move the linked issue from In Progress to In Review/Verification
  verify-main     mark merged issues Done only after successful main workflows
EOF
}

graphql() { gh api graphql "$@"; }

ensure_label() {
  local name="$1" description="$2"
  gh label create "$name" --repo "$REPOSITORY" --color 1D76DB --description "$description" --force >/dev/null
}

ensure_milestone() {
  local title="$1"
  gh api "repos/$REPOSITORY/milestones?state=all" --jq ".[] | select(.title == \"$title\") | .number" | grep -q . ||
    gh api "repos/$REPOSITORY/milestones" -f title="$title" >/dev/null
}

project_number() {
  gh project list --owner "$OWNER" --format json --limit 100 |
    jq -r --arg title "$(jq -r .project_title "$CONFIG")" \
      '.projects[] | select(.title == $title) | .number' | head -n1
}

bootstrap() {
  while IFS=$'\t' read -r name description; do ensure_label "$name" "$description"; done \
    < <(jq -r '.labels | to_entries[] | [.key,.value] | @tsv' "$CONFIG")
  while IFS= read -r milestone; do ensure_milestone "$milestone"; done < <(jq -r '.milestones[]' "$CONFIG")

  local number
  number="$(project_number)"
  if [[ -z "$number" ]]; then
    gh project create --owner "$OWNER" --title "$(jq -r .project_title "$CONFIG")" >/dev/null
    number="$(project_number)"
  fi
  gh project field-create "$number" --owner "$OWNER" --name "Priority" --data-type SINGLE_SELECT \
    --single-select-options "P0,P1,P2,P3" >/dev/null 2>&1 || true
  gh project field-create "$number" --owner "$OWNER" --name "Security gate" --data-type SINGLE_SELECT \
    --single-select-options "Required,Recommended,Not blocking" >/dev/null 2>&1 || true
  gh project field-create "$number" --owner "$OWNER" --name "Workflow Status" --data-type SINGLE_SELECT \
    --single-select-options "$(jq -r '.statuses | join(",")' "$CONFIG")" >/dev/null 2>&1 || true

  local project_id field_json
  project_id="$(gh project view "$number" --owner "$OWNER" --format json --jq .id)"
  field_json="$(gh project field-list "$number" --owner "$OWNER" --format json |
    jq '.fields[] | select(.name == "Workflow Status")')"
  [[ -n "$field_json" ]] || { echo "error: Workflow Status field was not created" >&2; exit 1; }
  jq -n \
    --argjson project_number "$number" \
    --arg project_id "$project_id" \
    --arg field_id "$(jq -r .id <<<"$field_json")" \
    --argjson options "$(jq '.options | map({key:.name,value:.id}) | from_entries' <<<"$field_json")" \
    '{project_number:$project_number,project_id:$project_id,workflow_status:{field_id:$field_id,options:$options}}' \
    > "$STATE_FILE"
  echo "development flow bootstrapped in project $number"
}

require_section() {
  local body="$1" heading="$2"
  grep -Eqi "^#{2,3}[[:space:]]+$heading([[:space:]]*$|[[:space:]])" <<<"$body" ||
    { echo "error: issue is missing required section '$heading'" >&2; return 1; }
}

validate_issue() {
  local event="${EVENT_PATH:-${GITHUB_EVENT_PATH:-}}" body
  [[ -n "$event" && -f "$event" ]] || { echo "error: EVENT_PATH or GITHUB_EVENT_PATH is required" >&2; exit 1; }
  body="$(jq -r '.issue.body // ""' "$event")"
  local failed=0
  for section in "Outcome" "Scope" "Acceptance criteria" "Required tests" "Security and RBAC impact" "Dependencies"; do
    require_section "$body" "$section" || failed=1
  done
  grep -Eq -- '- \[[ xX]\]' <<<"$body" ||
    { echo "error: acceptance criteria must contain a checklist" >&2; failed=1; }
  (( failed == 0 ))
}

state_project_number() {
  [[ -s "$STATE_FILE" ]] || { echo "error: run '$0 bootstrap' and commit $STATE_FILE" >&2; exit 1; }
  jq -r .project_number "$STATE_FILE"
}

set_flow_label() {
  local issue="$1" wanted="$2" current
  current="$(jq -r --arg status "$wanted" '.labels | to_entries[] | select(.value == $status) | .key' "$CONFIG")"
  while IFS= read -r label; do
    [[ "$label" == "$current" ]] || gh issue edit "$issue" --repo "$REPOSITORY" --remove-label "$label" >/dev/null 2>&1 || true
  done < <(jq -r '.labels | keys[]' "$CONFIG")
  gh issue edit "$issue" --repo "$REPOSITORY" --add-label "$current" >/dev/null
}

add_to_project() {
  local url="$1" number
  number="$(state_project_number)"
  gh project item-add "$number" --owner "$OWNER" --url "$url" >/dev/null 2>&1 || true
}

set_project_status() {
  local url="$1" status="$2" number item_id field_id option_id project_id
  number="$(state_project_number)"
  add_to_project "$url"
  item_id="$(gh project item-list "$number" --owner "$OWNER" --format json --limit 500 \
    --jq ".items[] | select(.content.url == $(jq -Rsa . <<<"$url")) | .id" | head -n1)"
  [[ -n "$item_id" ]] || { echo "error: could not locate project item for $url" >&2; return 1; }
  project_id="$(jq -r .project_id "$STATE_FILE")"
  field_id="$(jq -r .workflow_status.field_id "$STATE_FILE")"
  option_id="$(jq -r --arg status "$status" '.workflow_status.options[$status] // empty' "$STATE_FILE")"
  [[ -n "$option_id" ]] || { echo "error: project has no Workflow Status option '$status'" >&2; return 1; }
  gh project item-edit --id "$item_id" --project-id "$project_id" --field-id "$field_id" \
    --single-select-option-id "$option_id" >/dev/null
}

sync_issue() {
  local event="${EVENT_PATH:-${GITHUB_EVENT_PATH:-}}" number url action
  number="$(jq -r .issue.number "$event")"; url="$(jq -r .issue.html_url "$event")"; action="$(jq -r .action "$event")"
  add_to_project "$url"
  if [[ "$action" == opened ]]; then set_flow_label "$number" "Backlog"; set_project_status "$url" "Backlog"; fi
  if [[ "$action" == closed ]]; then set_flow_label "$number" "Verification"; set_project_status "$url" "Verification"; fi
  if [[ "$action" == reopened ]]; then set_flow_label "$number" "In Progress"; set_project_status "$url" "In Progress"; fi
  if [[ "$action" == labeled ]]; then
    local label status
    label="$(jq -r '.label.name // ""' "$event")"
    status="$(jq -r --arg label "$label" '.labels[$label] // empty' "$CONFIG")"
    [[ -z "$status" ]] || set_project_status "$url" "$status"
  fi
}

linked_issue() {
  local body="$1"
  grep -Eio '(close[sd]?|fix(e[sd])?|resolve[sd]?)[[:space:]]+#[0-9]+' <<<"$body" |
    grep -Eo '[0-9]+' | head -n1
}

sync_pr() {
  local event="${EVENT_PATH:-${GITHUB_EVENT_PATH:-}}" body action merged issue
  body="$(jq -r '.pull_request.body // ""' "$event")"
  action="$(jq -r .action "$event")"
  merged="$(jq -r '.pull_request.merged // false' "$event")"
  issue="$(linked_issue "$body")"
  [[ -n "$issue" ]] || { echo "error: PR must use Closes/Fixes/Resolves #issue" >&2; exit 1; }
  add_to_project "$(jq -r .pull_request.html_url "$event")"
  if [[ "$action" == closed && "$merged" == true ]]; then
    set_flow_label "$issue" "Verification"
    set_project_status "$(gh issue view "$issue" --repo "$REPOSITORY" --json url --jq .url)" "Verification"
  elif [[ "$action" != closed ]]; then
    set_flow_label "$issue" "In Review"
    set_project_status "$(gh issue view "$issue" --repo "$REPOSITORY" --json url --jq .url)" "In Review"
  fi
}

verify_main() {
  [[ "${GITHUB_REF:-refs/heads/main}" == refs/heads/main ]] || exit 0
  [[ "${WORKFLOW_CONCLUSION:-success}" == success ]] || exit 0
  local sha="${VERIFIED_SHA:-${GITHUB_SHA:-}}" prs issue
  [[ -n "$sha" ]] || { echo "error: VERIFIED_SHA or GITHUB_SHA is required" >&2; exit 1; }
  local runs required count result
  runs="$(gh run list --repo "$REPOSITORY" --commit "$sha" --limit 100 --json name,status,conclusion)"
  while IFS= read -r required; do
    count="$(jq -r --arg required "$required" '[.[] | select(.name == $required)] | length' <<<"$runs")"
    result="$(jq -r --arg required "$required" '[.[] | select(.name == $required)] | first | (.status + ":" + (.conclusion // ""))' <<<"$runs")"
    if [[ "$count" == 0 || "$result" != "completed:success" ]]; then
      echo "required workflow '$required' is not yet successful for $sha ($result)"
      exit 0
    fi
  done < <(jq -r '.required_workflows[]' "$CONFIG")
  prs="$(gh api "repos/$REPOSITORY/commits/$sha/pulls" --jq '.[].number')"
  while IFS= read -r pr; do
    [[ -n "$pr" ]] || continue
    issue="$(linked_issue "$(gh pr view "$pr" --repo "$REPOSITORY" --json body --jq .body)")"
    [[ -n "$issue" ]] || continue
    set_flow_label "$issue" "Done"
    set_project_status "$(gh issue view "$issue" --repo "$REPOSITORY" --json url --jq .url)" "Done"
    gh issue close "$issue" --repo "$REPOSITORY" --comment "Verified on main commit \`$sha\`; required workflow completed successfully." >/dev/null 2>&1 || true
  done <<<"$prs"
}

case "${1:-}" in
  bootstrap) bootstrap ;;
  validate-issue) validate_issue ;;
  sync-issue) sync_issue ;;
  sync-pr) sync_pr ;;
  verify-main) verify_main ;;
  *) usage >&2; exit 2 ;;
esac
