#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

for command in go helm; do
  command -v "$command" >/dev/null 2>&1 || { echo "error: $command is required" >&2; exit 1; }
done

helm lint deployments/helm/praetor-v2 \
  -f deployments/helm/praetor-v2/ci/values-k3d-local.yaml

# These are the contracts that otherwise fail only after the disposable cluster
# has been allocated. Keep this command identical locally and in GitHub Actions.
GOWORK=off go test ./tests -count=1 \
  -run 'Test(ProductValidation|DynamicInventory|InventorySyncHistory|NotificationDelivery|ReleaseMigration)'
GOWORK=off go test ./tests/contracts -count=1
