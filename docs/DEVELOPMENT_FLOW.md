# Praetor development flow

The repository, issues, and automation are the source of truth. The GitHub
Project is a generated view, not an independently maintained roadmap.

## Lifecycle

`Backlog → Ready → In Progress → In Review → Verification → Done`

- Every item begins as an issue created with the development-item template.
- Moving work to Ready is an explicit prioritization decision.
- A branch and pull request move the linked issue through active review.
- A merged pull request moves the issue to Verification.
- Only a successful required workflow on the merged `main` commit moves it to Done.
- Follow-up work is a new linked issue; completed parents are not held open.

## Bootstrap

Create a fine-grained token with repository Issues and organization Projects
read/write access. Store it as the repository secret
`PROJECT_AUTOMATION_TOKEN`, then run the **Development flow** workflow with
`bootstrap=true`. Commit the generated `.github/development-flow-state.json`.

The bootstrap command is safe to rerun:

```sh
GH_TOKEN=... ./scripts/development-flow.sh bootstrap
```

Configuration lives in `.github/development-flow.json`. Do not hard-code project
IDs in workflows or documentation.

## Local validation

```sh
bash -n scripts/development-flow.sh
go test ./tests -run DevelopmentFlow
```
