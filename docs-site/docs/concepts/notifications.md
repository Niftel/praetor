---
sidebar_position: 6
title: Notifications
---

# Notifications

Praetor stores reusable notification targets at organization scope. Organization notification administrators can manage them under **Settings → Notifications**, send a test delivery, and then attach a target to job or workflow events.

## Secret handling

Backend configuration is encrypted before it is stored. Secret fields, including webhook URLs, are write-only: list and test responses contain target identity and delivery status but never return the stored configuration.

Test delivery uses the same backend and decryption path as lifecycle delivery. Praetor returns a bounded success or failure result and does not include the destination or provider response body in API errors or application logs.

## Destination policy

Public HTTP notification destinations must use HTTPS. Praetor blocks loopback, link-local, and private network destinations by default to prevent notification configuration from becoming a server-side request forgery path.

Internal notification receivers must be explicitly allowlisted by exact hostname:

```yaml
api:
  extraEnv:
    - name: PRAETOR_NOTIFICATION_ALLOWED_HOSTS
      value: notifications.internal.example
consumer:
  extraEnv:
    - name: PRAETOR_NOTIFICATION_ALLOWED_HOSTS
      value: notifications.internal.example
scheduler:
  extraEnv:
    - name: PRAETOR_NOTIFICATION_ALLOWED_HOSTS
      value: notifications.internal.example
```

Use a comma-separated value for multiple hosts. Configure the same allowlist on the API, consumer, and scheduler: the API validates and tests targets, while the consumer and scheduler deliver lifecycle and approval notifications.

Allowlisting an internal hostname permits its private addresses and HTTP endpoints, so keep the list narrow and use TLS wherever the receiver supports it.
