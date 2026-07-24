# Extracted Repository Health Baseline

Baseline date: 2026-07-14

This baseline covers the five extracted deployable services and every
independently released shared module declared under `sharedModules` in
`platform-compatibility.yaml`. Checks run with `GOWORK=off`, so local workspace
replacements cannot hide an undeclared dependency.

## Results

| Repository | Independent module | CI | Image CI | Vet | Build | Test |
| --- | --- | --- | --- | --- | --- | --- |
| `scheduler` | Yes | Yes | Yes | Pass | Pass | Pass |
| `reconciler` | Yes | Yes | Yes | Pass | Pass | Environment-blocked |
| `executor` | Yes | Yes | Yes | Pass | Pass | Pass |
| `ingestion` | Yes | Yes | Yes | Pass | Pass | Pass |
| `consumer` | Yes | Yes | Yes | Pass | Pass | Pass |

The reconciler test failure is not an observed code failure. Its
`TestHarvestSendsInternalToken` test uses `httptest.NewServer`, and the managed
analysis sandbox prohibits binding the IPv6 loopback listener. The same sandbox
restriction also affects HTTP tests in this repository. Reconciler CI runs the
test normally on GitHub Actions and remains the authoritative result.

The Go command also attempted non-essential module stat-cache writes outside the
writable workspace. Isolated `GOCACHE` directories were used successfully; the
stat-cache warnings did not change vet, build, or test results.

## Common baseline already present

Every extracted service currently has:

- Go 1.26.5 declared in `go.mod`
- A clean standalone module boundary
- A root Dockerfile
- A repository README
- GitHub Actions for formatting, vet, build, and race-enabled tests
- GitHub Actions for tagged and commit-addressed GHCR images
- Superseded-run cancellation

This is a stronger starting point than the repository layout initially implied:
the primary remaining problem is cross-repository release coordination, not the
absence of service-level CI.

## Shared-module inventory

The compatibility manifest is authoritative. Each entry records:

- module path and GitHub repository;
- released semantic version;
- owning subsystem; and
- whether the module is security-sensitive.

The current inventory covers contracts/domain vocabulary (`models`, `events`,
`launch`, `packspec`), infrastructure adapters (`db`, `eventbus`, `objectstore`,
`metrics`, `hostconn`), security modules (`crypto`, `credentials`, `runtoken`,
`rbac`), and application utilities (`env`, `plog`, `render`, `registry`,
`notify`, `store`).

Every module is checked for `gofmt`, `go vet`, `go build`, and `go test` with an
isolated build cache and shared isolated module cache.

## Repeat the check

With the service repositories checked out beside `praetor`:

```bash
make workspace-health
```

Use `PRAETOR_WORKSPACE_DIR` when the repositories share a different parent:

```bash
PRAETOR_WORKSPACE_DIR=/path/to/workspace make workspace-health
```

The command deliberately fails for a missing repository, missing manifest
metadata, formatting drift, or any failed vet, build, or test check. It does not
mutate sibling repositories.

Pass service names directly for a focused check:

```bash
./scripts/check-workspace-health.sh scheduler executor
```

Check only local shared-module siblings:

```bash
make shared-module-health
```

Check downloads of the exact released module versions, as CI and remote release
preflight do. This supports both tagged releases and Go pseudo-versions:

```bash
make shared-module-health-remote
```

The `Shared module health` workflow runs this released-tag matrix when its
inventory or checker changes. Remote platform release preflight repeats it after
verifying component images, repository tags, and downloadable Go modules.

## GitHub Actions runtime baseline

Third-party actions are pinned to immutable 40-character commit SHAs. The
readable release beside each pin is documentation only and must resolve to that
exact commit.

The current action baseline uses Node 24 releases:

| Action | Release |
| --- | --- |
| `actions/checkout` | `v7.0.0` |
| `actions/setup-go` | `v7.0.0` |
| `actions/attest` | `v4.2.0` |
| `azure/setup-helm` | `v5.0.1` |
| `actions/upload-artifact` | `v7.0.1` |

GitHub-hosted `ubuntu-latest` runners satisfy their runtime requirements.
Self-hosted runners must run Actions Runner `v2.327.1` or newer. Workflows that
run authenticated Git commands from a container after `actions/checkout` must
use `v2.329.0` or newer. The repository's workflows do not currently use that
container-authenticated checkout path.

Major-version changes preserve the existing least-privilege workflow
permissions. Pull requests build images without publishing or attesting them;
the merged `main` image workflow is the controlled path that publishes the
commit-addressed images and exercises registry attestation.
