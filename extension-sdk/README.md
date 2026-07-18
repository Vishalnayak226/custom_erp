# Extension SDK — Custom Development Contract

This directory is the **entire** contract a hired 3rd-party developer or
outside agency needs to build a custom extension for one client's instance
of this ERP. It contains no core source code, no other client's data, and no
credentials beyond what a specific engagement issues.

**Before a real handoff**: copy this directory out into its own separate git
repository (it has zero dependency on anything else in this codebase) and
hand *that* to the developer, plus a scoped extension token (see below) and
a webhook URL for their own server. Do not give a 3rd-party developer access
to this repository.

## How it works

1. An admin registers a **hook**: `POST /api/v1/admin/extension/hooks`
   `{"hook_point": "document.before_save", "doctype": "PurchaseOrder", "target_url": "https://developer-server/hook", "timeout_ms": 3000}`.
   The response includes a `secret` — shown once, never retrievable again.
   Give this secret to the developer along with their own endpoint URL (they
   already have that part, obviously — it's their server).
2. Whenever a document of that doctype is about to be saved (or has just
   been saved, for `after_save`), this ERP calls the developer's
   `target_url` with a signed JSON payload (see below).
3. If the developer's endpoint needs to read anything else back from the
   ERP (e.g. related records), an admin issues them a **scoped token**:
   `POST /api/v1/admin/extension/token` `{"scope_doctype": "PurchaseOrder", "ttl_minutes": 60}`.
   That token authenticates `GET /api/v1/doc/{doctype}` requests for
   **exactly** the doctype it was scoped to, for **exactly** that tenant,
   **read-only** — nothing else. It is not a login, has no role, and cannot
   be used for any other endpoint.

## Hook points

| hook_point | When | If your endpoint fails or times out |
|---|---|---|
| `document.before_save` | Just before a document is written | **The save is blocked.** Your endpoint is in the critical path — a validation or pricing hook that doesn't run must not let an unreviewed value through. |
| `document.after_save` | Just after a document is written | Logged, save already committed. Your endpoint cannot roll anything back — use this for notifications/sync, not validation. |

## Request your endpoint receives

```json
{
  "hook_point": "document.before_save",
  "doctype": "PurchaseOrder",
  "document_id": "PO-2026-0001",
  "tenant_id": "acme-corp",
  "data": { "...": "the full document payload being saved" }
}
```

Headers:
- `Content-Type: application/json`
- `X-Signature: sha256=<hex hmac>` — HMAC-SHA256 of the raw request body,
  keyed with the `secret` you were given when the hook was registered.
  **Verify this before trusting the payload** — see the example below.

## Verifying the signature (any language)

```
expected = hex(hmac_sha256(secret, raw_request_body))
if expected != request.headers["X-Signature"].replace("sha256=", ""):
    reject as unauthenticated
```

## What your endpoint should return

- `before_save`: any `2xx` status allows the save to proceed. Any other
  status (or a timeout, or no response) **blocks it** — the caller gets a
  `502` with your rejection surfaced as the error.
- `after_save`: your response is logged but otherwise ignored. Respond
  `2xx` quickly; there is no retry.

## What you will never receive

- This repository's source code.
- Any other client/tenant's data, token, or webhook secret.
- A full user session token (your token has no `role`, cannot log in
  anywhere in the UI, and works on exactly one doctype, read-only).
- Write access via the scoped token — if your extension needs to write
  data back, that happens through your own hook response contract with the
  admin who commissioned the work, not through the scoped token.

## Calling back into the API with your scoped token

```
GET /api/v1/doc/PurchaseOrder
Authorization: Bearer <the token from POST /api/v1/admin/extension/token>
```

Any other doctype, or any non-GET method, returns `403`.
