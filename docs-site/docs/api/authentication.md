---
sidebar_position: 1
title: Authentication
---

# Authentication

The API is at **`https://api.praetor.localhost/api/v1`** (via Traefik). Every request carries a bearer token — either a login **JWT** or a **personal access token**.

## Personal access tokens (recommended for scripts/CI)

A **PAT** authenticates as its owning user (inherits that user's RBAC). Create one in the UI (**API Tokens**) or via the API; the plaintext (`prtr_pat_…`) is shown **once** — only its SHA-256 hash is stored.

```bash
# Create (returns the secret once)
curl -X POST https://api.praetor.localhost/api/v1/tokens \
  -H "Authorization: Bearer $JWT" -H "Content-Type: application/json" \
  -d '{"name":"ci-pipeline","expires_at":null}'
# -> {"id":1,"name":"ci-pipeline","token":"prtr_pat_XXXX...","expires_at":null,...}

# Use it
export PRAETOR_TOKEN=prtr_pat_XXXX...
curl https://api.praetor.localhost/api/v1/job-templates \
  -H "Authorization: Bearer $PRAETOR_TOKEN"
```

- **List** your tokens: `GET /api/v1/tokens` (metadata only — never the secret).
- **Revoke:** `DELETE /api/v1/tokens/{id}` (own tokens; superusers can revoke any). A revoked token is rejected immediately.
- **Expiry:** set `expires_at` (RFC3339); an expired token is rejected.

## Login (JWT)

`POST /api/v1/auth/login` with username/password (LDAP-backed) returns a signed JWT for interactive/browser use. Login is rate-limited per IP.

## A few conventions

- `GET /api/v1/ping` — unauthenticated liveness check.
- Machine endpoints the host-runner calls (`/runs/{id}/events`, `/logs`, `/heartbeat`, `/facts`) live on the **ingestion** service.
- Reads and writes are scoped by RBAC to what the token's user can see; superusers/auditors see all.
