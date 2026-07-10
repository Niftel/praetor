#!/usr/bin/env bash
# Reconciler DB integration harness: stands up a throwaway Postgres, applies EVERY
# migration in order (full schema, unlike the cherry-picked chaos harness), and runs
# the reconciler's SQL-level tests — the FOR UPDATE SKIP LOCKED claim, lease/expiry,
# hot-before-cold ordering, and cold-tier demotion/promotion. These prove the parts
# that pure unit tests can't: real row-lock contention and real UPDATE semantics.
#
# Usage: scripts/reconciler-it.sh
set -euo pipefail

cd "$(dirname "$0")/.."

DB_NAME=praetor-recon-it-db
DB_PORT=5549
DB_URL="postgres://postgres:postgres@localhost:${DB_PORT}/praetor?sslmode=disable"

teardown() { docker rm -f "$DB_NAME" >/dev/null 2>&1 || true; }
trap teardown EXIT
teardown

echo "==> starting throwaway Postgres ($DB_NAME:$DB_PORT)"
docker run -d --name "$DB_NAME" \
  -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=praetor \
  -p ${DB_PORT}:5432 postgres:15 >/dev/null

for _ in $(seq 1 30); do
  docker exec "$DB_NAME" pg_isready -U postgres >/dev/null 2>&1 && break
  sleep 1
done

echo "==> applying all migrations"
# Same order the migrator uses: filename-sorted *.up.sql (numeric prefixes first,
# then the date-prefixed RBAC file). ON_ERROR_STOP so a bad apply fails loudly.
for f in $(ls db/migrations/*.up.sql | sort); do
  docker exec -i "$DB_NAME" psql -U postgres -d praetor -q -v ON_ERROR_STOP=1 < "$f" >/dev/null
done

echo "==> running reconciler integration tests"
TEST_DATABASE_URL="$DB_URL" \
  go test ./services/reconciler/core/ -run 'Claim|Reschedule|Advance' -count=1 -v -timeout 120s

echo "==> reconciler integration: all passed"
