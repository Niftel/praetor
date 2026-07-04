---
sidebar_position: 2
title: Inbound Webhooks
---

# Inbound Webhooks

Praetor can launch work from a remote event. Webhook endpoints are **public** (no user auth) and verified instead by a per-object shared secret, so a git host or external system can trigger a run.

## Endpoints

| Endpoint | Effect |
|---|---|
| `POST /api/v1/webhooks/job-templates/{id}/{service}` | Launch a job template |
| `POST /api/v1/webhooks/workflow-templates/{id}/{service}` | Launch a workflow |
| `POST /api/v1/webhooks/execution-packs/{id}/{service}` | Rebuild an [Execution Pack](../concepts/execution-packs.md) |

`{service}` is `github`, `gitlab`, or `generic`:

- **github** — verified via the `X-Hub-Signature-256` HMAC of the body with the object's secret.
- **gitlab** — verified via the `X-Gitlab-Token` header.
- **generic** — verified via a `?token=` query param (or token header).

The webhook payload is injected as `extra_vars` (with convenience `webhook_ref` / `webhook_commit`), so the launched job can act on the pushed ref/commit.

## Concurrency

Webhooks can fire in bursts. If the target template/workflow doesn't `allow_simultaneous`, a webhook that arrives while a prior run is still active is **skipped** (HTTP `202` with a "skipped" reason) rather than queuing an overlapping run.

## Example

```bash
curl -X POST "https://api.praetor.localhost/api/v1/webhooks/job-templates/4/generic?token=YOUR_SECRET" \
  -H "Content-Type: application/json" \
  -d '{"ref":"refs/heads/main","after":"abc123"}'
```

Set the secret on the template (job template form → webhook, or the workflow/pack equivalent); the UI shows the exact URL to register with your git host.
