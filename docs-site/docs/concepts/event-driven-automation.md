---
sidebar_position: 6
title: Event-Driven Automation
---

# Event-Driven Automation (self-healing)

Praetor can react to real-time events — alerts, monitoring signals, external systems — and automatically launch remediation, closing the loop between observability and action. It's Praetor's take on Event-Driven Ansible: **event sources → rules (conditions) → actions**, in a push model.

The three pieces:

1. **Event source** — an authenticated intake channel. Any system (Alertmanager, monitoring, another app) `POST`s a JSON event to `/api/v1/events/{source}`, authorized by the source's shared token.
2. **Rule** — a condition evaluated by the [grule](https://github.com/hyperjumptech/grule-rule-engine) rule engine against each incoming event. On a match, it launches a job/workflow template.
3. **Action with context** — the launched run receives the event as `extra_vars.eda_event`, and (optionally) a value pulled from the event becomes the run's `--limit`, so remediation targets **only the affected host**.

## Rules and conditions

A rule's `condition` is a **GRL boolean expression** over the incoming event, exposed as the `Event` fact with typed, path-based accessors:

| Accessor | Returns | Example |
|---|---|---|
| `Event.Str("path")` | string | `Event.Str("labels.alertname")` |
| `Event.Num("path")` | number | `Event.Num("value") > 90` |
| `Event.Bool("path")` | bool | `Event.Bool("firing")` |
| `Event.Has("path")` | present? | `Event.Has("labels.instance")` |

Paths are dotted and may index arrays: `Event.Str("alerts.0.status")`. Combine with `&&`, `||`, `!`. Conditions are compiled and validated when the rule is created (a bad expression is rejected).

```
Event.Str("labels.alertname") == "ApacheDown" && Event.Str("status") == "firing"
```

## Targeting the affected host

Set a rule's `limit_field` to a dotted path in the event (e.g. `labels.instance`). Its value becomes the launched run's Ansible `--limit`, so a remediation playbook written as `hosts: all` only touches the host the event was about. The full event is also available to the play as `eda_event`.

## Concurrency

If the target template doesn't allow simultaneous runs, an event that arrives while a remediation for that template is already active is **skipped** — so an alert storm can't pile up overlapping heals.

## Worked example: apache self-heal

1. **Remediation project** — an SCM project `restore-apache` whose playbook (idempotently) ensures `httpd` is running.
2. **Template** — `restore-apache` job template (project + inventory + [Machine credential](./credentials.md)).
3. **Source** — `POST /api/v1/event-sources` `{"name":"alertmanager","organization_id":1}` → returns a token.
4. **Rule** — `POST /api/v1/event-rules`:
   ```json
   {
     "name": "heal-apache",
     "organization_id": 1,
     "source_id": 1,
     "condition": "Event.Str(\"labels.alertname\") == \"ApacheDown\" && Event.Str(\"status\") == \"firing\"",
     "unified_job_template_id": 21,
     "limit_field": "labels.instance"
   }
   ```
5. **Fire it** — point Alertmanager (or curl) at the source:
   ```bash
   curl -X POST "https://api.praetor.localhost/api/v1/events/alertmanager?token=SRC_TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"status":"firing","labels":{"alertname":"ApacheDown","instance":"web1"}}'
   # -> {"received":true,"matched":1,"launched":[150]}
   ```

The rule matches, `restore-apache` runs **limited to `web1`**, and httpd comes back up. Fire it again while healthy and the play is a no-op (`changed=0`).

## API

| Endpoint | Auth | Purpose |
|---|---|---|
| `POST /api/v1/events/{source}` | source token (public) | ingest an event; evaluate rules; launch matches |
| `GET/POST /api/v1/event-sources`, `DELETE /{id}` | superuser | manage sources (token returned once on create) |
| `GET/POST /api/v1/event-rules`, `DELETE /{id}` | superuser | manage rules |

Every accepted event is recorded in `event_receipts` (payload, rules matched, jobs launched) for debugging the rulebook.

## Scope

This is a **push** model — sources deliver events to Praetor. It covers webhook-style sources (Alertmanager, CI, custom emitters) directly. Polling sources (watch a URL/log) and stateful multi-event correlation (Drools-style "event A then B within N minutes") are not implemented; a single rule matches a single event. For a polling-style health signal you can also schedule a healthcheck playbook and trigger on its outcome (see [job templates](./inventories-and-templates.md)).
