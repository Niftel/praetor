---
sidebar_position: 2
title: Inbound Webhooks
---

# Inbound Webhooks

Praetor can launch work from a remote event (a git push, a CI callback, any external system). Webhook endpoints are **public** — there is no user auth. Authorization is a **per-object shared secret**, verified per provider.

## How a webhook is registered

There is no separate registration table, listener, or callback handshake. A webhook is just state **on the resource itself** (a job template, workflow template, or Execution Pack):

- `webhook_enabled` — turns the webhook on,
- `webhook_service` — which provider scheme to verify (`github` / `gitlab` / `generic`),
- `webhook_key` — the shared secret.

Enable it on the resource (job template form → *Webhook*, or the workflow/pack equivalent). When you enable a webhook without supplying a key, the API **mints one automatically** (a 24-byte random secret); an existing key is preserved on update. The inbound URL is then fixed — it's derived from the resource's `id` and the `service`, so there's nothing else to register on Praetor's side.

Registering end-to-end:

1. Enable the webhook on the template and note the secret (the UI shows the exact URL + secret).
2. Point your git host / external system at `…/webhooks/<resource>/{id}/{service}`.
3. Configure that provider to send the secret in the scheme its `{service}` expects (below).

## Endpoints

| Endpoint | Effect |
|---|---|
| `POST /api/v1/webhooks/job-templates/{id}/{service}` | Launch a job template |
| `POST /api/v1/webhooks/workflow-templates/{id}/{service}` | Launch a workflow run |
| `POST /api/v1/webhooks/execution-packs/{id}/{service}` | Re-queue a git-backed [Execution Pack](../concepts/execution-packs.md) rebuild |
| `POST /api/v1/webhooks/workflow-job-nodes/{id}/callback` | Release a waiting `webhook_in` [workflow](../concepts/workflows.md) node |

## Verification

`{service}` selects how the secret is checked. All comparisons are constant-time, and the request body is read up to a 1 MB cap.

| `{service}` | How the secret is presented |
|---|---|
| `github` | `X-Hub-Signature-256: sha256=<hmac>` — HMAC-SHA256 of the raw body keyed with the secret |
| `gitlab` | `X-Gitlab-Token: <secret>` |
| `generic` | `X-Praetor-Token: <secret>` header, or `?token=<secret>` query param |

The node-callback endpoint is the exception: it is authorized by the node's **per-run `event_token`** (via `X-Praetor-Token` or `?token=`), not the object's `webhook_key`.

:::note Unknown and unauthorized look identical
A request to a non-existent resource, a resource with the webhook disabled, or one that fails verification all return **`404 Not Found`** (verification failures return `401`). This is deliberate — you can't probe which resources have webhooks enabled.
:::

## What the launched run receives

For job/workflow launches, the payload is injected as `extra_vars`, so the run can act on the pushed ref/commit:

- `webhook_payload` — the full decoded JSON body,
- `webhook_ref` — the payload's `ref` (e.g. `refs/heads/main`),
- `webhook_commit` — the payload's `after` or `checkout_sha`.

A workflow launch snapshots the template's nodes/edges into a new run (so later template edits don't change an in-flight run); a job launch inserts a `pending` job the scheduler then claims — the same path as a manual launch.

## Concurrency

Webhooks can fire in bursts. If the target template/workflow doesn't `allow_simultaneous`, a webhook that arrives while a prior run is still active is **skipped** — HTTP `202` with `{"status":"skipped"}` — rather than queuing an overlapping run.

## Examples

Generic (secret in a query param or header):

```bash
curl -X POST "https://api.praetor.localhost/api/v1/webhooks/job-templates/4/generic?token=YOUR_SECRET" \
  -H "Content-Type: application/json" \
  -d '{"ref":"refs/heads/main","after":"abc123"}'
# -> 202 {"job_id": 147, "status": "pending"}
```

GitHub (point a repo webhook at the `github` URL; GitHub signs with the secret):

```
Payload URL:  https://api.praetor.localhost/api/v1/webhooks/job-templates/4/github
Content type: application/json
Secret:       YOUR_SECRET
```

Releasing a waiting workflow node (per-run token, optional failure result):

```bash
curl -X POST "https://api.praetor.localhost/api/v1/webhooks/workflow-job-nodes/88/callback" \
  -H "X-Praetor-Token: NODE_EVENT_TOKEN" \
  -d '{"status":"failed"}'   # omit body (or status) to release down the success edges
```
