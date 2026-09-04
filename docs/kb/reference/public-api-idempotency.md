---
title: Retrying safely (idempotency)
section: Reference
order: 41
summary: Why every mutating call needs an Idempotency-Key, and exactly what the server does with it.
audience: integrator
public: true
last_verified: 2026-09-03
---

# Retrying safely (idempotency)

A network timeout does not tell you whether the request arrived. Retrying
without protection is how one customer order becomes two. Every mutating public
endpoint therefore **requires** an `Idempotency-Key` header - it is not
optional, and a request without one is refused before anything happens. This is
enforced generically, for every method other than `GET`, by the same public API
middleware every call already passes through - not per-endpoint, so it applies
automatically the moment a mutating endpoint ships.

> [!NOTE]
> As of this writing the public API v1 surface itself is **read-only**
> (`items`, `items/{code}`, `inventory`, `orders/{id}/status` - all `GET`).
> The example below shows the contract a mutating call must follow once one
> exists; it is illustrative, not a currently callable endpoint.

## Choosing a key

Generate a fresh unique value per logical operation - a UUID is ideal. Reuse the
**same** key for every retry of that operation, and never for a different one.

```
POST /api/public/v1/orders HTTP/1.1
Authorization: Bearer erp_v1_...
X-Tenant-ID: your-tenant
Idempotency-Key: 6f1c9c8e-9f7a-4a1c-9a3e-2b6c1f0d5e44
Content-Type: application/json

{"channel_order_id":"AMZ-1001", "lines":[...]}
```

## What the server does

| Situation | Response |
|---|---|
| First request with this key | Runs normally |
| Identical retry, first attempt finished | Replays the stored response, with `Idempotency-Replayed: true` |
| Identical retry, first attempt still running | `409` with `Retry-After` - wait and retry |
| Same key, **different** request body | `409` - this is a client bug, not a retry |
| First attempt returned `5xx` | The key is released; your retry is a genuine second attempt |

That last row matters: a server error is never cached. If the server failed, you
are entitled to try again and get a real answer.

> [!NOTE]
> Keys expire after the tenant's configured retention window (24 hours by
> default). After that the same key value is treated as a brand new request, so
> do not rely on a key from last week to deduplicate today's call.

## Practical advice

- Derive the key from your own order identifier, not from a timestamp. Then a
  crash-and-restart retry naturally reuses the same key.
- Store the key alongside your record of the operation, so a retry after a
  process restart can find it.
- Do not include the key in a hash of the request. The server already compares
  the request itself, and a key that changes with the body defeats the purpose.
