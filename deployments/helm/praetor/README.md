# Praetor Helm chart — UNMAINTAINED / UNSUPPORTED

> ⚠️ **Do not use this chart.** It is stale and has never been validated against a
> real Kubernetes cluster. It is kept only as a starting point for a future,
> properly-authored chart. The **source of truth for the deployment topology is
> [`docker-compose.yml`](../../../docker-compose.yml)**.

## Why it's unsupported

The platform's topology has moved well past what this chart models. As of this
writing the chart is missing or wrong in at least these ways:

- **Missing services:** `ingestion`, `reconciler`, `buildkitd` (#46),
  `docker-socket-proxy` (#25), `gitea` (pack registry / SCM), `ldap`, `traefik`,
  `docs`, `prometheus`, `grafana`.
- **References removed/renamed components** and predates the current service split.
- **No persistence** for the state that now requires it: localhost-run WAL +
  extracted packs on the executor (#45), the pack registry volume, JetStream
  file store.
- **Stale/absent config** for things added since: per-run ingestion auth
  (`PRAETOR_INTERNAL_TOKEN`, #43), bounded JetStream/log store + opt-out retention
  (#17/#27), env-sourced LDAP bind password (#34), host-runner **v0.7.0**, the
  by-reference manifest fetch path (#13/#48).

## Building a real chart

A production chart is a genuine project, not a patch of this one — it needs
StatefulSets/PVCs for the stateful pieces, secrets management, the ingress story,
and validation against an actual cluster. Until that exists, deploy with
docker-compose.
