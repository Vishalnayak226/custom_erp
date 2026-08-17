---
title: Authenticating against the public API
section: Reference
order: 40
summary: How an integration gets a key, what a key may do, and what every response tells you about your budget.
audience: integrator
public: true
last_verified: 2026-08-12
---

# Authenticating against the public API

The public API is a curated, versioned subset of the application under
`/api/public/v1/`. It is separate from the application's internal API, which is
not a supported integration surface and is not covered by any compatibility
promise.

## Getting a key

A Super Admin issues one from the administration surface. The plaintext key is
shown **once**, at creation, and only a one-way digest is stored - if it is
lost, rotate the credential rather than trying to recover it.

A key names its scopes explicitly when it is issued. There is no wildcard scope,
and a key never inherits a person's role.

| Scope | Grants |
|---|---|
| `items:read` | Curated product identity |
| `inventory:read` | Available-to-sell reads |
| `orders:read` | Order status and tracking |
| `orders:write` | Order creation and mutation, once such endpoints exist |
| `pim:read` | Product content, groups and readiness |
| `pim:write` | Explicit PIM mutations |
| `webhooks:manage` | Webhook subscription lifecycle, once it exists |

## Every request

```
GET /api/public/v1/items?limit=50 HTTP/1.1
Authorization: Bearer erp_v1_<prefix>_<secret>
X-Tenant-ID: your-tenant
```

Both headers are required. A mutating request also requires `Idempotency-Key` -
see [Retrying safely](public-api-idempotency.md).

## Every response

```
X-Correlation-ID: 3f2a...
X-RateLimit-Limit: 120
X-RateLimit-Remaining: 118
X-Quota-Limit: 50000
X-Quota-Remaining: 49873
```

These are present on success and on rejection alike, so you can pace your client
from the headers rather than discovering your limits by being refused.

## When you are refused

| Status | Meaning | What to do |
|---|---|---|
| 401 | Not authenticated | Check the key and `X-Tenant-ID`. The message never says which part failed - that is deliberate. |
| 403 | Authenticated, wrong scope | The message names the scope you need. Ask for a key that holds it. |
| 404 | No such resource, or no such endpoint | Check the path against the endpoint list. |
| 422 | A parameter was present but invalid | The message names the parameter. |
| 429 | Rate limit or daily quota | Read `Retry-After`. The message distinguishes the two - a per-minute burst is a short wait, an exhausted daily quota is not. |

> [!WARNING]
> Do not retry a 429 immediately in a tight loop. `Retry-After` on a quota
> rejection can be hours, and hammering it only fills your own traffic log.

Every error body carries the same envelope:

```json
{
  "error": "This API credential does not hold the \"items:read\" scope required by this endpoint.",
  "code": "GLOBAL-0011",
  "correlation_id": "415fcc38-0f88-6dbc-2540-bd76ecb26736",
  "retryable": false
}
```

Quote the `correlation_id` in a support request - it identifies the exact call in
the server's own traffic log.
