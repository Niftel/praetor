# Product validation fixture

The fixture adds synthetic LDAP identities and a local notification receiver to
the integrated Praetor and Secrets Service development namespace. It never
deletes the namespace, databases, persistent volumes, or Secrets Service keys.

```sh
./scripts/product-validation-fixture.sh create
./scripts/product-validation-fixture.sh status
./scripts/product-validation-fixture.sh cleanup
```

Creation is idempotent: ConfigMaps and workloads use stable names and Helm
reuses the installed release values. Cleanup selects only resources labelled
`app.kubernetes.io/part-of=praetor-validation-fixture` and disables the
fixture-owned LDAP mount. API resources are deleted in dependency order by
their reserved `Praetor Validation` names; LDAP-mapped identities and all
unrelated platform data remain intact. All identities and passwords are synthetic.
