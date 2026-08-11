# Public API v1 Contract

Status: **foundation in progress (Stage 38, started 2026-08-11).** This document is the compatibility
policy required by 38.1. No `/api/public/v1` business route is registered yet. The credential
lifecycle is built first so an internal user session is never mistaken for an integration identity.

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
written to the existing audit log. These keys are deliberately **not accepted anywhere yet**; 38.1's
curated handlers and 38.3's per-credential limiter must land before the first public business route.
