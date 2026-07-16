# Database migration compatibility

Praetor tests database upgrades as executable compatibility commitments. The
matrix constructs representative historical schemas from the repository's real
migration files, inserts sentinel tenant and authorization data, and then runs
the production `cmd/migrator` to the current schema.

The current matrix starts at migrations:

- `000055`: capability-based RBAC;
- `000062`: team-scoped workflow approvals;
- `000065`: secrets-backed credential binding cancellation; and
- `000067`: delegated launch grants.

Each upgrade must preserve organizations, users, credentials, custom role
definitions, Secrets Service credential references, and service principals when
those records existed at the starting version. The final schema must contain the
latest migration record and delegated-launch structures.

The matrix also executes the latest explicitly reversible boundary by applying
`000068` down, removing its migration-history record, and proving the production
migrator can reapply it. This is not a promise that arbitrary historical
downgrades are safe. A migration is rollback-supported only when the test names
that exact boundary and its down migration passes without weakening retained
data guarantees.

Run it against an isolated PostgreSQL database:

```sh
DATABASE_URL='postgres://postgres:postgres@localhost:5432/praetor?sslmode=disable' \
  ./scripts/database-compatibility.sh
```

CI runs the matrix with PostgreSQL 15 and follows it with API database smoke
tests. The test database must never be a live Praetor database because the
fixture command resets the `public` schema between starting versions.
