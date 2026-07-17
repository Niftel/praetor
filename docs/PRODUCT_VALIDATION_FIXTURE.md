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
