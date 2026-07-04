---
sidebar_position: 1
title: Observability (Metrics)
---

# Observability

Every Praetor service exposes Prometheus metrics at **`/metrics`** (OpenMetrics text format). Services with an HTTP server (api, ingestion) serve it on their port; loop-only services (scheduler, consumer, executor, reconciler, packbuilder) serve it on **`:2112`**.

## Local stack: Prometheus + Grafana

The compose stack includes Prometheus (scrapes all services in-network) and Grafana with a pre-provisioned **Praetor Overview** dashboard:

- **Grafana** — `http://localhost:3005` (default `admin`/`admin`, override with `GRAFANA_USER`/`GRAFANA_PASSWORD`)
- **Prometheus** — `http://localhost:9090`

## Key metrics

| Metric | Meaning |
|---|---|
| `praetor_scheduler_jobs_dispatched_total` | jobs turned into runs |
| `praetor_scheduler_queue_depth` | jobs pending/queued (not yet running) |
| `praetor_scheduler_runs_reconciling_total` / `_runs_lost_total` | stale runs handed to the reconciler / declared lost |
| `praetor_reconciler_outcomes_total{outcome}` | recovered_successful / recovered_failed / lost / still_running |
| `praetor_consumer_events_projected_total`, `_terminal_transitions_total{status}` | event throughput + terminal transitions |
| `praetor_executor_bootstraps_total{mode}` / `_bootstrap_failures_total{mode}` | pack bootstraps by mode (remote/local/inventory_sync) |
| `praetor_ingestion_events_total` | events accepted |
| `praetor_api_http_requests_total{route,method,status}` + `_request_duration_seconds` | API traffic by **route pattern** (bounded cardinality) |

Plus Go runtime + process metrics for free.

## Integrating other platforms

The Prometheus exposition format is the industry lingua franca, so effectively any backend works:

- **Native scrape** — Prometheus, Grafana Agent/Alloy, VictoriaMetrics, Grafana Cloud, Google/Azure/AWS managed Prometheus.
- **Via an agent/collector** — Datadog (OpenMetrics check), New Relic, Elastic (Metricbeat), InfluxDB (Telegraf), or the **OpenTelemetry Collector** `prometheus` receiver → OTLP to anything.

It's **pull-based**: a scraper must reach the endpoints (in-network by service name, or publish the ports). Prometheus can then `remote_write` to any long-term/SaaS backend. `/metrics` is unauthenticated (like `/ping`) — keep it behind your network or front it with auth if exposed.
