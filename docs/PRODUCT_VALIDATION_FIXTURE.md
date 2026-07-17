# Product validation fixture

The fixture adds synthetic LDAP identities and a local notification receiver to
the integrated Praetor and Secrets Service development namespace. It never
deletes the namespace, databases, persistent volumes, or Secrets Service keys.

```sh
PRAETOR_SECRETS_ROOT=../praetor-secrets ./scripts/bootstrap-product-validation-base.sh
./scripts/product-validation-fixture.sh create
./scripts/product-validation-fixture.sh status
make validation-ldap-operator-journey
./scripts/product-validation-fixture.sh cleanup
```

The bootstrap command is for a clean k3d cluster. It generates ephemeral PKI
and master keys with the Secrets Service's development bootstrap, deploys two
isolated PostgreSQL instances, installs the Secrets Service and audit sink, and
then installs the released Praetor component set. CI runs the complete lifecycle
from a fresh cluster.

Creation is idempotent: ConfigMaps and workloads use stable names and Helm
reuses the installed release values. Cleanup selects only resources labelled
`app.kubernetes.io/part-of=praetor-validation-fixture` and disables the
fixture-owned LDAP mount. API resources are deleted in dependency order by
their reserved `Praetor Validation` names; LDAP-mapped identities and all
unrelated platform data remain intact. All identities and passwords are synthetic.

The LDAP operator journey signs in four synthetic identities through the public
API and verifies the complete authorization boundary: organization and team
mapping, scoped inventory and host access, authorized workflow launch, approval
visibility limited to the assigned team, rejection of cross-team and self
approval, successful completion, and requester/approver attribution in the
auditor-visible activity stream. Its final output is sanitized JSON containing
only the workflow run ID, terminal status, and synthetic actor/team names.

## Credential execution lifecycle

With an architecture-matched Execution Pack in `build/runtime`, the live secrets
gate exercises the deployed API, scheduler, executor, ingestion service, and
Secrets Service rather than mocks:

```sh
make secrets-execution-e2e
```

It plants a random canary as a Machine credential password, verifies that
Praetor stores only the Secrets Service reference and a masking placeholder,
executes the playbook, and proves that the run resolves its credential exactly
once. It then checks cross-team metadata denial, wrong-workload resolution,
completed-run replay, explicit cancellation, expiry, and credential retirement.
Finally, it scans captured API responses, activity and audit data, database
dumps, terminal executor manifests, and workload logs for the canary. Evidence
output contains IDs and terminal status only; it never contains credential
material.
