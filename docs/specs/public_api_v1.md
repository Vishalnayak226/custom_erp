# Public API v1 Contract

Status: **open, read-only (Stage 38, 2026-08-12).** This document is the compatibility policy
required by 38.1. The credential lifecycle (38.2), the safety spine (38.3 rate limits and quotas,
38.5 idempotency, 38.9 traffic log) and the first four read-only endpoints are built; the machine
contract is generated to [`openapi_public_v1.json`](openapi_public_v1.json) by 38.8.

No mutating public endpoint exists yet. That is a sequencing decision, not an omission: the
idempotency spine every write depends on is already in place under the same middleware, so a write
endpoint joins the surface by being curated, not by being made safe first.

## Boundary

- Public endpoints will live only under `/api/public/v1/`. Existing `/api/v1/` routes remain the
  private application API and are not made public by this work.
- Every endpoint is curated and registered deliberately. The internal route count is not an API
  surface and no generic "all doctypes" public endpoint will be offered.
- An integration authenticates with an opaque `erp_v1_...` API key plus its tenant identifier. It
  never submits a username/password, borrows a browser JWT, or inherits a human role.
- A key authorizes only its explicit scopes. There is no wildcard or implicit Super Admin scope.

The first scope catalog is:

| Scope | Intended stable surface |
|---|---|
| `items:read` | Curated item identity and sellable product data |
| `inventory:read` | Availability reads, with tenant/location boundaries |
| `orders:read` | Curated sales-order status and detail reads |
| `orders:write` | Idempotent order creation/mutation once 38.5 exists |
| `pim:read` | Product content, groups and readiness reads |
| `pim:write` | Explicit PIM mutations once idempotency is available |
| `webhooks:manage` | Public webhook-subscription lifecycle once 38.4 exists |

These names are defined in `engines/public_api_credentials.go`; issuing an unknown or empty scope is
rejected. Defining a scope does **not** mean its endpoint has shipped.

## Compatibility policy

Within `v1`:

1. Existing fields and successful response meanings are not removed or renamed.
2. New optional response fields may be added. Clients must ignore fields they do not understand.
3. New required request fields, enum changes that invalidate an accepted value, and semantic changes
   require a new API version or an announced deprecation/migration window.
4. Timestamps are RFC3339 UTC. Identifiers are opaque strings even when a current implementation
   happens to use a UUID or business code.
5. Mutating endpoints must require an idempotency key before they can join the public surface (38.5).
6. Errors use the platform error envelope and correlation ID; authentication failures never reveal
   whether a key prefix, tenant, revoked key or expired key was the part that matched.

## Credential lifecycle now available to administrators

After `db/migrations_stage38_2_api_credentials.sql` is applied, Super Admin human sessions can call:

| Method | Private administration endpoint | Purpose |
|---|---|---|
| `POST` | `/api/v1/admin/api-credentials` | Issue a scoped key; plaintext is returned once |
| `GET` | `/api/v1/admin/api-credentials` | List metadata and the supported scope catalog, never hashes/secrets |
| `POST` | `/api/v1/admin/api-credentials/{id}/rotate` | Atomically issue a replacement and revoke the old key |
| `DELETE` | `/api/v1/admin/api-credentials/{id}` | Revoke immediately |

Only a SHA-256 digest of 256-bit random key material is stored. Issuance, rotation and revocation are
written to the existing audit log.

Stage 38.3/38.8/38.9 add three more, on the same Super Admin gate:

| Method | Private administration endpoint | Purpose |
|---|---|---|
| `PUT` | `/api/v1/admin/api-credentials/{id}/limits` | Pin one key's per-minute and per-day budgets; `null` clears an override |
| `GET` | `/api/v1/admin/api-credentials/{id}/traffic` | Recent calls for one key, plus its effective limits |
| `GET` | `/api/v1/admin/api-traffic` | Recent calls across every key |
| `GET` | `/api/v1/admin/public-api/openapi.json` | The generated OpenAPI document |

An integration key can never read or change its own limits - that is why these are on the private
surface rather than the public one.

## Endpoints available now

| Method | Endpoint | Scope | Returns |
|---|---|---|---|
| `GET` | `/api/public/v1/items` | `items:read` | Paged product list, `updated_since` for incremental polling |
| `GET` | `/api/public/v1/items/{code}` | `items:read` | One product |
| `GET` | `/api/public/v1/inventory?sku=` | `inventory:read` | Available-to-sell per location |
| `GET` | `/api/public/v1/orders/{id}/status` | `orders:read` | Order status, per-line status, shipments |

Each response is a **curated projection**, not a document dump: the generic internal document API
returns every stored field, so publishing that shape would turn every future internal field into an
accidental public contract. The item projection carries no cost, margin or supplier data, and the
inventory projection publishes availability without the held-back buckets behind it.

## Request and response conventions

Every request carries:

| Header | Required | Meaning |
|---|---|---|
| `Authorization: Bearer erp_v1_...` | always | The API credential. Never a user session token. |
| `X-Tenant-ID` | always | The tenant the credential belongs to. |
| `Idempotency-Key` | mutating methods | Makes a retry safe. Required, not optional. |

Every response carries `X-Correlation-ID`, plus the budget headers `X-RateLimit-Limit`,
`X-RateLimit-Remaining`, `X-Quota-Limit` and `X-Quota-Remaining` - on success and on rejection alike,
so a client can pace itself instead of discovering its limits by being refused. A `429` also carries
`Retry-After`, and its message distinguishes the per-minute burst limit from the daily quota, because
"wait a second" and "wait until midnight" call for different client behaviour.

Idempotency semantics: the first request with a given key runs; an identical retry replays the
stored response with `Idempotency-Replayed: true`; the same key with a *different* body is a `409`
rather than a replay; a still-running duplicate is a `409` with `Retry-After`. A `5xx` is never
stored, so retrying after a server error is a genuine second attempt. Keys expire after
`platform.public_api_idempotency_retention_hours` (default 24).

Unknown paths under `/api/public/v1/` answer a JSON `404`, never the SPA's HTML.
