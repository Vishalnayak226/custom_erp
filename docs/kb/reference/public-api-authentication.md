---
title: Authenticating against the public API
section: Reference
order: 40
summary: How an integration gets a module key, how it signs in, and what every response tells you about your budget.
audience: integrator
screens: [api-access]
public: true
last_verified: 2026-11-16
doc_id: DOC-B0B4E9D9C8
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-12
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: reference
module: platform
task: Authenticating against the public API
prerequisites: Signed-in account with permission for the described task; observe the article prerequisites
---

# Authenticating against the public API

The public API is a curated, versioned subset of the application under
`/api/public/v1/`. It is separate from the application's internal API, which is
not a supported integration surface and is not covered by any compatibility
promise.

## Getting a key

Someone with the *Share module API* right (today only an Admin or Super
Admin) makes a key under **Settings → API access → New API access**. They
tick the modules the key may reach, each at **View** or **Change**. The key is
shown **once**, or sent to you as a one-use link valid for 48 hours. Only a
one-way digest is stored. If the key is lost, ask for it to be rotated.

A key acts as its **owner**, the person who made it. Every call runs with the
owner's own rights, capped by the modules ticked on the key, so a key never
does more than its owner can. If the owner is made inactive, the key stops
until an Admin takes it over.

`GET /api/public/v1/me` tells you what your key can do: its modules, document
types, whether it can approve, its stores, its expiry and its limits.

Keys made before module keys existed carry fixed scopes instead, and keep
working:

| Scope | Grants |
|---|---|
| `items:read` | Curated product identity |
| `inventory:read` | Available-to-sell reads |
| `orders:read` | Order status and tracking |
| `orders:write` | Order creation and mutation, once such endpoints exist |
| `notifications:write` | Report a notification's delivery status back |
| `pim:read` | Product content, groups and readiness, once such endpoints exist |
| `pim:write` | Explicit PIM mutations, once such endpoints exist |
| `webhooks:manage` | Webhook subscription lifecycle, once it exists |

## Every request

```
GET /api/public/v1/me HTTP/1.1
Authorization: Bearer erp_v2_<company>_<prefix>_<secret>
```

A new key (`erp_v2_`) carries the company code, so no other header is needed.
An older key (`erp_v1_`) also sends `X-Tenant-ID: your-company`.

An OAuth client gets its bearer token from `POST /api/public/v1/oauth/token`
(form-encoded, `grant_type=client_credentials`, plus `X-Tenant-ID`) and sends it
the same way. The token lasts 15 minutes.

A mutating request also requires `Idempotency-Key` -
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
| 401 | Not authenticated, or the key was revoked or has expired | Check the key (and `X-Tenant-ID` for an old key). The message never says which part failed - that is deliberate. |
| 403 | The key may not do this | The `fix` field says what to ask for, for example *needs View on Inventory*. The key's owner can widen it with **Fix** on the API access screen. |
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

A refusal by a module key also carries `fix` (what to change) and `docs_url`
(where the code is explained).

Quote the `correlation_id` in a support request - it identifies the exact call in
the server's own traffic log.
