---
sidebar_position: 7
title: Change-aware CI
---

# Change-aware CI

Praetor uses one repository-owned planner for local verification and GitHub Actions. The planner reads changed paths and selects only the Go, UI, database, deployment, security, product-validation, CodeQL, and image gates that can be affected.

Run the same plan before pushing:

```bash
make verify-changed
```

The default comparison base is `origin/main`. Override it when validating a stacked branch:

```bash
BASE_REF=origin/another-branch make verify-changed
```

To build the affected container images as well as running the functional gates:

```bash
make verify-changed-images
```

Image builds are opt-in locally because they require a healthy Docker daemon. Pull-request Actions build affected images automatically.

## Gate selection

The planner is [`scripts/plan-ci.sh`](https://github.com/Niftel/praetor/blob/main/scripts/plan-ci.sh). Its output is a stable key-value contract consumed by local and remote runners. Representative behavior:

| Change | Selected work |
| --- | --- |
| Documentation only | Classifier and aggregate checks only |
| API Go code | Go, database, security, Go CodeQL, API image |
| UI source | UI, JavaScript CodeQL, UI image |
| UI tests only | UI tests and build; no image |
| Database migration | Database compatibility and migrator image |
| Helm or deployment automation | Deployment contracts and targeted product validation |
| Module versions | Go, database, security, API and migrator images |

Unknown non-documentation paths conservatively select the Go gate. Changes to the planner, its local runner, the Makefile, or the primary CI workflow select every validation family.

## Required checks

Individual jobs are allowed to skip when the planner proves they are unaffected. Stable aggregate jobs remain present on every pull request and fail if classification fails or any selected child job does not succeed.

Post-merge test and security work is not repeated. The Image workflow builds only affected artifacts and its stable `image-gate` completion is the single signal used to reconcile linked project issues after merge. Weekly CodeQL and vulnerability scans remain scheduled because their external security intelligence can change without a source commit.
