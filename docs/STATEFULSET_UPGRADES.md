# StatefulSet upgrades

Praetor treats the identity and volume claim templates of its PostgreSQL, NATS, and executor StatefulSets as immutable installation decisions. A normal Helm upgrade may change pod templates, images, replica counts, and the explicitly supported mutable fields, but it must not change a StatefulSet service name, selector, pod management policy, or volume claim template.

Run `scripts/helm-statefulset-preflight.sh` with the same release, namespace, chart, values, and `--set` flags that will be passed to Helm. The local-cluster updater and product-validation bootstrap do this automatically. If an immutable field differs, the script exits before Helm runs and prints the live and requested values.

## Storage changes

Never reduce a requested volume size. Kubernetes cannot shrink a PVC, and changing the size in a StatefulSet volume claim template does not resize existing claims.

For an intentional expansion:

1. Back up PostgreSQL and NATS and verify the backups can be restored.
2. Confirm the StorageClass has `allowVolumeExpansion: true`.
3. Pause automation launches and drain active executor work.
4. Expand each existing PVC directly and wait for the filesystem resize to finish.
5. Keep the chart's volume claim template at its installed value unless a separately tested, release-specific migration recreates the StatefulSet with orphaned PVCs.
6. Run the preflight again, perform the Helm upgrade, and verify database, stream, executor-pack, job, and SSH data.

Recreating a StatefulSet is not part of the automatic upgrade path. It requires an approved maintenance procedure that orphans rather than deletes PVCs, proves every replacement pod binds the expected claim, and includes a tested rollback. Do not use a blanket `helm upgrade --force`; it can replace unrelated resources and creates an avoidable data-loss risk.
