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
usage: $0 <bootstrap|validate-issue|sync-issue|sync-pr|verify-main|repair-project|audit-completion|close-milestone>

  bootstrap       create labels, milestones, project, fields, and saved state
  validate-issue  validate EVENT_PATH issue content before accepting it
  sync-issue      add/update an issue in the project from EVENT_PATH
  sync-pr         move the linked issue from In Progress to In Review/Verification
  verify-main     mark merged issues Done only after successful main workflows
  repair-project  reconcile canonical project Status from authoritative flow labels
  audit-completion fail when milestone, issue, project, or security-debt state disagrees
  close-milestone close MILESTONE_TITLE only after the completion audit passes
EOF
}

project_gh() {
  if [[ -z "${PROJECT_GH_TOKEN:-}" ]]; then
    echo "warning: project synchronization requires PROJECT_AUTOMATION_TOKEN" >&2
    return 1
  fi
  GH_TOKEN="$PROJECT_GH_TOKEN" gh "$@"
}

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
  project_gh project list --owner "$OWNER" --format json --limit 100 |
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
    project_gh project create --owner "$OWNER" --title "$(jq -r .project_title "$CONFIG")" >/dev/null
    number="$(project_number)"
  fi
  project_gh project field-create "$number" --owner "$OWNER" --name "Priority" --data-type SINGLE_SELECT \
    --single-select-options "P0,P1,P2,P3" >/dev/null 2>&1 || true
  project_gh project field-create "$number" --owner "$OWNER" --name "Security gate" --data-type SINGLE_SELECT \
    --single-select-options "Required,Recommended,Not blocking" >/dev/null 2>&1 || true
  local project_id field_json
  project_id="$(project_gh project view "$number" --owner "$OWNER" --format json --jq .id)"
  field_json="$(project_gh project field-list "$number" --owner "$OWNER" --format json |
    jq '.fields[] | select(.name == "Status")')"
  [[ -n "$field_json" ]] || { echo "error: canonical project Status field is missing" >&2; exit 1; }
  jq -n \
    --argjson project_number "$number" \
    --arg project_id "$project_id" \
    --arg field_id "$(jq -r .id <<<"$field_json")" \
    --argjson options "$(jq '.options | map({key:.name,value:.id}) | from_entries' <<<"$field_json")" \
    '{project_number:$project_number,project_id:$project_id,project_status:{field_id:$field_id,options:$options}}' \
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
  local issue="$1" wanted="$2" current_label existing
  current_label="$(jq -r --arg status "$wanted" '.labels | to_entries[] | select(.value == $status) | .key' "$CONFIG")"
  existing="$(gh issue view "$issue" --repo "$REPOSITORY" --json labels --jq '.labels[].name')"
  while IFS= read -r label; do
    [[ "$label" == "$current_label" ]] || ! grep -Fxq "$label" <<<"$existing" ||
      gh issue edit "$issue" --repo "$REPOSITORY" --remove-label "$label" >/dev/null
  done < <(jq -r '.labels | keys[]' "$CONFIG")
  grep -Fxq "$current_label" <<<"$existing" ||
    gh issue edit "$issue" --repo "$REPOSITORY" --add-label "$current_label" >/dev/null
}

has_open_linked_pr() {
  local issue="$1" pattern pulls
  pulls="$(gh pr list --repo "$REPOSITORY" --state open --limit 100 --json body)"
  pattern="(?i)(close[sd]?|fix(e[sd])?|resolve[sd]?)[[:space:]]+#${issue}([^0-9]|$)"
  jq -e --arg pattern "$pattern" 'any(.[]; (.body // "") | test($pattern))' <<<"$pulls" >/dev/null
}

authoritative_issue_status() {
  local issue="$1" fallback="$2" details current
  details="$(gh issue view "$issue" --repo "$REPOSITORY" --json state,labels)"
  if jq -e '.labels | any(.name == "flow:done")' <<<"$details" >/dev/null; then
    echo "Done"
  elif [[ "$(jq -r .state <<<"$details")" == CLOSED ]]; then
    echo "Verification"
  elif has_open_linked_pr "$issue"; then
    echo "In Review"
  else
    current="$(jq -r --slurpfile config "$CONFIG" '
      [.labels[].name as $label | $config[0].labels[$label] // empty]
      | if length == 1 then .[0] else empty end
    ' <<<"$details")"
    echo "${current:-$fallback}"
  fi
}

reconcile_issue() {
  local issue="$1" url="$2" fallback="$3" status
  status="$(authoritative_issue_status "$issue" "$fallback")"
  set_flow_label "$issue" "$status"
  set_project_status "$url" "$status"
}

add_to_project() {
  local url="$1" number
  number="$(state_project_number)"
  project_gh project item-add "$number" --owner "$OWNER" --url "$url" >/dev/null 2>&1 || true
}

set_project_status() {
  local url="$1" status="$2" number item_id field_id option_id project_id items canonical
  number="$(state_project_number)"
  add_to_project "$url"
  if ! items="$(project_gh project item-list "$number" --owner "$OWNER" --format json --limit 500 2>/dev/null)"; then
    echo "warning: project synchronization requires PROJECT_AUTOMATION_TOKEN" >&2
    return 0
  fi
  item_id="$(jq -r --arg url "$url" '.items[] | select(.content.url == $url) | .id' <<<"$items" | head -n1)"
  [[ -n "$item_id" ]] || { echo "warning: project item for $url is not visible yet" >&2; return 0; }
  project_id="$(jq -r .project_id "$STATE_FILE")"
  canonical="$(canonical_project_status "$status")"
  field_id="$(jq -r .project_status.field_id "$STATE_FILE")"
  option_id="$(jq -r --arg status "$canonical" '.project_status.options[$status] // empty' "$STATE_FILE")"
  [[ -n "$option_id" ]] || { echo "error: project has no canonical Status option '$canonical'" >&2; return 1; }
  project_gh project item-edit --id "$item_id" --project-id "$project_id" --field-id "$field_id" \
    --single-select-option-id "$option_id" >/dev/null 2>&1 ||
    echo "warning: project synchronization requires PROJECT_AUTOMATION_TOKEN" >&2
}

canonical_project_status() {
  case "$1" in
    Backlog|Ready) echo "Todo" ;;
    "In Progress"|"In Review"|Verification) echo "In Progress" ;;
    Done) echo "Done" ;;
    *) echo "error: unsupported workflow status '$1'" >&2; return 1 ;;
  esac
}

project_issue_repairs() {
  jq -r --arg repository "https://github.com/$REPOSITORY" '
    .items[]
    | select(.content.type == "Issue" and .repository == $repository)
    | ([.labels[]? | select(startswith("flow:"))]) as $flow
    | if ($flow | length) != 1 then
        error("issue #\(.content.number) must have exactly one flow label")
      else . end
    | (if $flow[0] == "flow:done" then "Done"
       elif $flow[0] == "flow:backlog" or $flow[0] == "flow:ready" then "Todo"
       elif $flow[0] == "flow:in-progress" or $flow[0] == "flow:in-review" or $flow[0] == "flow:verification" then "In Progress"
       else error("issue #\(.content.number) has unsupported flow label \($flow[0])") end) as $expected
    | select((.status // "") != $expected)
    | [.id, $expected, .content.number] | @tsv
  '
}

repair_project() {
  local number items repairs project_id field_id item_id status issue option_id repaired=0
  number="$(state_project_number)"
  items="$(project_gh project item-list "$number" --owner "$OWNER" --format json --limit 500)"
  repairs="$(project_issue_repairs <<<"$items")"
  project_id="$(jq -r .project_id "$STATE_FILE")"
  field_id="$(jq -r .project_status.field_id "$STATE_FILE")"
  while IFS=$'\t' read -r item_id status issue; do
    [[ -n "$item_id" ]] || continue
    option_id="$(jq -r --arg status "$status" '.project_status.options[$status] // empty' "$STATE_FILE")"
    [[ -n "$option_id" ]] || { echo "error: project has no canonical Status option '$status'" >&2; return 1; }
    project_gh project item-edit --id "$item_id" --project-id "$project_id" --field-id "$field_id" \
      --single-select-option-id "$option_id" >/dev/null
    echo "repaired issue #$issue project Status -> $status"
    repaired=$((repaired + 1))
  done <<<"$repairs"
  echo "canonical project Status repaired from flow labels ($repaired item(s) changed)"
}

audit_milestone_issues() {
  local title="$1" issues invalid
  issues="$(gh issue list --repo "$REPOSITORY" --milestone "$title" --state all --limit 1000 --json number,state,labels,url)"
  invalid="$(jq -r '
    .[]
    | ([.labels[].name] | index("flow:done")) as $done
    | select(.state != "CLOSED" or ($done | not))
    | "#\(.number) state=\(.state) flow=" + ([.labels[].name | select(startswith("flow:"))] | join(","))
  ' <<<"$issues")"
  if [[ -n "$invalid" ]]; then
    echo "error: milestone '$title' is not complete:" >&2
    sed 's/^/  /' <<<"$invalid" >&2
    return 1
  fi
}

audit_closed_milestones() {
  local milestones title failed=0
  milestones="$(gh api "repos/$REPOSITORY/milestones?state=closed&per_page=100")"
  while IFS= read -r title; do
    [[ -n "$title" ]] || continue
    audit_milestone_issues "$title" || failed=1
  done < <(jq -r '.[].title' <<<"$milestones")
  (( failed == 0 ))
}

audit_project_statuses() {
  local number items invalid
  number="$(state_project_number)"
  items="$(project_gh project item-list "$number" --owner "$OWNER" --format json --limit 500)"
  invalid="$(jq -r --arg repository "$REPOSITORY" '
    .items[]
    | select(.content.type == "Issue")
    | select(.repository == ("https://github.com/" + $repository))
    | ([.labels[]? | select(startswith("flow:"))]) as $flow
    | (if ($flow | index("flow:done")) then "Done"
       elif ($flow | index("flow:backlog")) or ($flow | index("flow:ready")) then "Todo"
       elif ($flow | index("flow:in-progress")) or ($flow | index("flow:in-review")) or ($flow | index("flow:verification")) then "In Progress"
       else "INVALID" end) as $expected
    | select(($flow | length) != 1 or (.status // "") != $expected)
    | "#\(.content.number) project=\(.status // "missing") flow=\($flow | join(",")) expected=\($expected)"
  ' <<<"$items")"
  if [[ -n "$invalid" ]]; then
    echo "error: project Status disagrees with authoritative flow labels:" >&2
    sed 's/^/  /' <<<"$invalid" >&2
    return 1
  fi
}

audit_stale_closed_issues() {
  local now cutoff issues invalid
  now="${AUDIT_NOW_EPOCH:-$(date +%s)}"
  cutoff="$((now - 86400))"
  issues="$(gh issue list --repo "$REPOSITORY" --state closed --limit 1000 --json number,closedAt,labels,url)"
  invalid="$(jq -r --argjson cutoff "$cutoff" '
    .[]
    | ([.labels[].name | select(startswith("flow:"))]) as $flow
    | select(($flow | length) > 0 and ($flow | index("flow:done") | not))
    | select((.closedAt | fromdateiso8601) < $cutoff)
    | "#\(.number) closedAt=\(.closedAt) flow=\($flow | join(","))"
  ' <<<"$issues")"
  if [[ -n "$invalid" ]]; then
    echo "error: closed issues remained outside Done for more than 24 hours:" >&2
    sed 's/^/  /' <<<"$invalid" >&2
    return 1
  fi
}

audit_security_tracking() {
  local baseline="$ROOT/.github/gosec-high-baseline.json" issue state failed=0
  while IFS= read -r issue; do
    [[ -n "$issue" ]] || continue
    state="$(gh issue view "$issue" --repo "$REPOSITORY" --json state --jq .state)"
    if [[ "$state" != OPEN ]]; then
      echo "error: unresolved gosec baseline finding references non-open issue #$issue ($state)" >&2
      failed=1
    fi
  done < <(jq -r '[.findings[].tracking_issue] | unique[]' "$baseline")
  (( failed == 0 ))
}

audit_completion() {
  local failed=0
  audit_closed_milestones || failed=1
  audit_stale_closed_issues || failed=1
  audit_project_statuses || failed=1
  audit_security_tracking || failed=1
  if (( failed != 0 )); then
    echo "error: development completion audit failed" >&2
    return 1
  fi
  echo "development completion audit passed"
}

close_milestone() {
  local title="${1:-${MILESTONE_TITLE:-}}" milestones milestone number state
  [[ -n "$title" ]] || { echo "error: milestone title argument or MILESTONE_TITLE is required" >&2; return 1; }
  milestones="$(gh api "repos/$REPOSITORY/milestones?state=all&per_page=100")"
  milestone="$(jq -c --arg title "$title" '.[] | select(.title == $title)' <<<"$milestones")"
  [[ -n "$milestone" ]] || { echo "error: milestone '$title' does not exist" >&2; return 1; }
  number="$(jq -r .number <<<"$milestone")"
  state="$(jq -r .state <<<"$milestone")"
  [[ "$state" == open ]] || { echo "error: milestone '$title' is already $state" >&2; return 1; }
  audit_milestone_issues "$title"
  audit_project_statuses
  gh api --method PATCH "repos/$REPOSITORY/milestones/$number" -f state=closed >/dev/null
  echo "milestone '$title' closed after completion audit"
}

sync_issue() {
  local event="${EVENT_PATH:-${GITHUB_EVENT_PATH:-}}" number url action
  number="$(jq -r .issue.number "$event")"; url="$(jq -r .issue.html_url "$event")"; action="$(jq -r .action "$event")"
  add_to_project "$url"
  if [[ "$action" == opened || "$action" == edited ]]; then reconcile_issue "$number" "$url" "Backlog"; fi
  if [[ "$action" == closed ]]; then reconcile_issue "$number" "$url" "Verification"; fi
  if [[ "$action" == reopened ]]; then reconcile_issue "$number" "$url" "In Progress"; fi
  if [[ "$action" == labeled ]]; then
    local label status
    label="$(jq -r '.label.name // ""' "$event")"
    status="$(jq -r --arg label "$label" '.labels[$label] // empty' "$CONFIG")"
    [[ -z "$status" ]] || reconcile_issue "$number" "$url" "$status"
  fi
}

linked_issue() {
  local body="$1" issue
  issue="$(grep -Eio '(close[sd]?|fix(e[sd])?|resolve[sd]?)[[:space:]]+#[0-9]+' <<<"$body" |
    grep -Eo '[0-9]+' | head -n1 || true)"
  printf '%s\n' "$issue"
}

sync_pr() {
  local event="${EVENT_PATH:-${GITHUB_EVENT_PATH:-}}" body action merged issue
  body="$(jq -r '.pull_request.body // ""' "$event")"
  action="$(jq -r .action "$event")"
  merged="$(jq -r '.pull_request.merged // false' "$event")"
  issue="$(linked_issue "$body")"
  [[ -n "$issue" ]] || { echo "error: PR must use Closes/Fixes/Resolves #issue" >&2; exit 1; }
  add_to_project "$(jq -r .pull_request.html_url "$event")"
  local issue_url
  issue_url="$(gh issue view "$issue" --repo "$REPOSITORY" --json url --jq .url)"
  if [[ "$action" == closed && "$merged" == true ]]; then
    reconcile_issue "$issue" "$issue_url" "Verification"
  elif [[ "$action" == closed ]]; then
    reconcile_issue "$issue" "$issue_url" "In Progress"
  elif [[ "$action" != closed ]]; then
    reconcile_issue "$issue" "$issue_url" "In Review"
  fi
}

verify_main() {
  [[ "${GITHUB_REF:-refs/heads/main}" == refs/heads/main ]] || exit 0
  [[ "${WORKFLOW_CONCLUSION:-success}" == success ]] || exit 0
  local sha="${VERIFIED_SHA:-${GITHUB_SHA:-}}" prs issue
  [[ -n "$sha" ]] || { echo "error: VERIFIED_SHA or GITHUB_SHA is required" >&2; exit 1; }
  prs="$(gh api "repos/$REPOSITORY/commits/$sha/pulls" --jq '.[].number')"
  while IFS= read -r pr; do
    [[ -n "$pr" ]] || continue
    if ! required_pr_workflows_succeeded "$pr"; then
      echo "required pull-request workflows are not successful for PR #$pr"
      exit 0
    fi
    issue="$(linked_issue "$(gh pr view "$pr" --repo "$REPOSITORY" --json body --jq .body)")"
    [[ -n "$issue" ]] || continue
    set_flow_label "$issue" "Done"
    set_project_status "$(gh issue view "$issue" --repo "$REPOSITORY" --json url --jq .url)" "Done"
    gh issue close "$issue" --repo "$REPOSITORY" --comment "Verified on main commit \`$sha\`; required workflow completed successfully." >/dev/null 2>&1 || true
  done <<<"$prs"
}

required_pr_workflows_succeeded() {
  local pr="$1" checks required successes failures
  checks="$(gh pr checks "$pr" --repo "$REPOSITORY" --json workflow,state 2>/dev/null || true)"
  [[ -n "$checks" ]] || {
    echo "required workflow checks are unavailable for PR #$pr"
    return 1
  }
  while IFS= read -r required; do
    successes="$(jq -r --arg required "$required" \
      '[.[] | select(.workflow == $required and .state == "SUCCESS")] | length' <<<"$checks")"
    failures="$(jq -r --arg required "$required" '
      [.[] | select(
        .workflow == $required
        and (.state != "SUCCESS" and .state != "SKIPPED" and .state != "NEUTRAL")
      )] | length
    ' <<<"$checks")"
    if (( successes == 0 || failures != 0 )); then
      echo "required workflow '$required' is not successful for PR #$pr (successes=$successes failures=$failures)"
      return 1
    fi
  done < <(jq -r '.required_workflows[]' "$CONFIG")
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  case "${1:-}" in
    bootstrap) bootstrap ;;
    validate-issue) validate_issue ;;
    sync-issue) sync_issue ;;
    sync-pr) sync_pr ;;
    verify-main) verify_main ;;
    repair-project) repair_project ;;
    audit-completion) audit_completion ;;
    close-milestone) close_milestone "${2:-}" ;;
    *) usage >&2; exit 2 ;;
  esac
fi
