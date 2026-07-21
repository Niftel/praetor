# Pipeline performance

## Baseline

GitHub Actions run `29812284924` (2026-07-21) is the baseline for the full
product-validation fixture. It completed successfully in 10 minutes 11 seconds.

| Phase | Duration | Share |
| --- | ---: | ---: |
| Clean cluster and integrated base | 5m 50s | 57% |
| Complete fixture lifecycle | 3m 37s | 36% |
| Secrets Service evidence | 19s | 3% |
| Delegated API evidence | 9s | 1% |
| Setup, reporting, and teardown | 16s | 3% |

## PR execution model

The workflow uses two tiers:

1. `preflight` validates Helm and lifecycle contracts before allocating a k3d
   cluster. A failure stops the expensive job.
2. `lifecycle` retains every clean-cluster LDAP, RBAC, approval, execution,
   recovery, secrets, delegated API, and readiness-evidence journey.

Superseded runs for the same pull request are cancelled. Main-branch runs are
never cancelled, because their evidence may be used for release decisions.

The lifecycle builds the changed Praetor API, migrator, UI, and Secrets Service
from source. Unchanged scheduler, executor, ingestion, consumer, and reconciler
images come from `deployments/staging/release-lock.yaml`. They are pulled by
immutable digest, retagged for the isolated k3d cluster, imported locally, and
still probed with `imagePullPolicy=Never`. This avoids rebuilding five sibling
repositories without introducing mutable inputs or reducing integration
coverage.

The regular CI workflow treats deployable concerns independently. Go/API,
deployment-contract, and UI checks run as parallel jobs, while database
compatibility remains independently parallel. A small aggregate `test` job
preserves the established required-check name and fails unless every isolated
gate succeeds.

## Regression target

The first target is a successful full lifecycle under eight minutes on a normal
GitHub-hosted runner, with preflight feedback under five minutes. GitHub run
step timings remain the source of truth; later optimizations must update this
baseline and preserve the same required journeys.
