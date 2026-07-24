#!/usr/bin/env bash
set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is required}"

starting_versions=(55 62 65 67)

for version in "${starting_versions[@]}"; do
  echo "==> Preparing migration $version"
  go run ./cmd/migrationfixture prepare "$version"
  echo "==> Upgrading migration $version to current"
  go run ./cmd/migrator
  go run ./cmd/migrationfixture assert
done

echo "==> Proving the latest reversible boundary"
go run ./cmd/migrationfixture rollback 79
go run ./cmd/migrator
go run ./cmd/migrationfixture assert

echo "Database compatibility matrix passed"
