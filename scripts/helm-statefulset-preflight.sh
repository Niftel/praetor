#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 RELEASE NAMESPACE CHART [helm template flags ...]" >&2
  exit 2
}

(( $# >= 3 )) || usage
release="$1"
namespace="$2"
chart="$3"
shift 3

for command in helm kubectl jq; do
  command -v "$command" >/dev/null 2>&1 || { echo "error: $command is required" >&2; exit 1; }
done

normalize_spec='{
  serviceName,
  podManagementPolicy: (.podManagementPolicy // "OrderedReady"),
  selector,
  volumeClaimTemplates: [(.volumeClaimTemplates // [])[] | {
    name: .metadata.name,
    accessModes: (.spec.accessModes | sort),
    storageClassName: (.spec.storageClassName // null),
    volumeMode: (.spec.volumeMode // "Filesystem"),
    storage: .spec.resources.requests.storage
  }]
}'

failed=0
for component in executor nats postgresql; do
  name="$release-$component"
  if ! live="$(kubectl get statefulset "$name" -n "$namespace" -o json 2>/dev/null)"; then
    continue
  fi

  desired_objects="$(helm template "$release" "$chart" -n "$namespace" "$@" \
    --show-only "templates/$component.yaml" | kubectl create --dry-run=client -f - -o json)"
  desired="$(jq -s 'map(select(.kind == "StatefulSet")) | first' <<<"$desired_objects")"
  [[ "$desired" != null ]] || { echo "error: chart did not render StatefulSet $name" >&2; exit 1; }
  live_immutable="$(jq -S -c ".spec | $normalize_spec" <<<"$live")"
  desired_immutable="$(jq -S -c ".spec | $normalize_spec" <<<"$desired")"
  if [[ "$live_immutable" != "$desired_immutable" ]]; then
    echo "error: upgrade would change immutable fields on StatefulSet $namespace/$name" >&2
    diff -u \
      <(jq -S ".spec | $normalize_spec" <<<"$live") \
      <(jq -S ".spec | $normalize_spec" <<<"$desired") >&2 || true
    failed=1
  fi
done

if (( failed != 0 )); then
  cat >&2 <<'EOF'
error: Helm was not run; no release resources were mutated.
Changing a StatefulSet volumeClaimTemplate does not resize or shrink its existing PVCs.
Keep the installed immutable values, or follow the documented data-preserving StatefulSet migration procedure.
EOF
  exit 1
fi

echo "StatefulSet upgrade preflight passed"
