# Extracted Repository Health Baseline

Baseline date: 2026-07-14

This report covers the five deployable services extracted from the Praetor
integration repository. Checks were run from clean sibling working trees with
`GOWORK=off`, so local workspace replacements could not hide an undeclared
dependency.

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

## Repeat the check

With the service repositories checked out beside `praetor`:

```bash
make workspace-health
```

Use `PRAETOR_WORKSPACE_DIR` when the repositories share a different parent:

```bash
PRAETOR_WORKSPACE_DIR=/path/to/workspace make workspace-health
```

The command deliberately fails for a missing repository or any failed vet,
build, or test check. It does not mutate sibling repositories.

Pass service names directly for a focused check:

```bash
./scripts/check-workspace-health.sh scheduler executor
```

## Remaining gaps

1. Promotion is still initiated manually; there is not yet one workflow that
   tags every component repository and waits for all image publications.
2. Database compatibility is represented by the supported migration range, but
   cross-version upgrade/downgrade scenarios are not yet executable tests.
3. Shared modules have not yet received the same standalone health inventory.

The RBAC v1-to-v4 consumer migration is complete. Praetor now uses the native
`github.com/praetordev/rbac/v4` loader, an embedded versioned policy, atomic
last-known-good refresh, integrity pinning, and v4 decision provenance.

The compatibility gate validates the declared component set, Helm image tags,
Go dependency versions, wire-contract fixture version, and migration range on
every pull request. The remote release preflight additionally verifies every
component repository tag, deployable image, component module, and shared
contract module declared in `platform-compatibility.yaml`.
