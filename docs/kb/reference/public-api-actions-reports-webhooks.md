---
title: Actions, reports and webhooks through the API
section: Reference
order: 42
summary: Run a reviewed screen action, pull a report as JSON or CSV, and get told when a document changes.
audience: integrator
screens: [api-access]
public: true
last_verified: 2026-11-17
doc_id: DOC-357E9EDA67
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-15
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: reference
module: platform
task: Actions, reports and webhooks through the API
prerequisites: A module key from Settings → API access (see Authenticating against the public API)
---

# Actions, reports and webhooks through the API

Records cover most work: see
[Reading and writing records through the API](public-api-records.md). This page
covers the three other things a module key can do. Each one runs as the key's
owner and only in the key's modules.

## Actions

Some screen buttons are not a plain save, for example putting an order on hold.
A short, reviewed list of them is open to keys:

```
GET  /api/public/v1/actions
POST /api/public/v1/actions/{module}/{action}
```

The list answers `{"data":[{module, action, level, summary, body, method, path}]}`
for the actions your key can run; `body` shows what to send. Today:

| Module | Action | Does |
|---|---|---|
| `oms` | `order-hold`, `order-release-hold` | Put an order on hold, or release it |
| `oms` | `order-cancel` | Cancel an order |
| `oms` | `order-release-to-fulfillment` | Release an order to the warehouse |
| `inventory` | `piece-hold-place`, `piece-hold-release` | Hold or release a piece of stock |

An action needs an `Idempotency-Key` header. One that is not on the list
answers 404 **APIKEY-0516**; one in a module the key lacks answers 403
**APIKEY-0503**. Money, POS, period, finance and manufacturing actions are not
open to keys.

## Reports

```
GET /api/public/v1/reports
GET /api/public/v1/reports/{id}?format=json&page=1&page_size=200
```

The first lists the reports in your key's modules that its owner may run, with
each report's columns and filters. The second runs one. Pass the report's own
filters as parameters. `format=csv` gives a CSV file. `page_size` goes up
to 1000. Columns the owner may not see are left out. An unknown report answers
404 **APIKEY-0517**.

## Webhooks

The key's owner adds a receiver on the key's **Webhooks** tab under
**Settings → API access**: an https address and the events to send (none
ticked means all of them). The signing secret is shown once.

Events are named `<module>.<DocType>.<event>`, where the event is
`created`, `updated`, `submitted`, `cancelled` or `approved`, for example
`procurement.PurchaseOrder.submitted`. The event is sent whether the change
was made on the document or by a module's own action, such as an order
cancelled on the OMS screen. A key gets events only for document types it
can read. Each delivery is a `POST` with this body:

```json
{
  "event": "procurement.PurchaseOrder.submitted",
  "payload": {
    "event": "procurement.PurchaseOrder.submitted",
    "tenant_code": "your-company",
    "module": "procurement",
    "doctype": "PurchaseOrder",
    "id": "PUR-ORD-000042",
    "status": "Submitted",
    "occurred_at": "2026-11-17T09:30:00Z",
    "record_url": "/api/public/v1/records/PurchaseOrder/PUR-ORD-000042"
  }
}
```

The event carries no fields: read the record at `record_url` with your key.

**Check the signature.** Each delivery has `X-Webhook-Timestamp` and
`X-Webhook-Signature-256: t=<timestamp>,v1=<hex>`. The `v1` value is
HMAC-SHA256, keyed with your signing secret, of `<timestamp>.<raw body>`.
Refuse a delivery whose signature does not match, or whose timestamp is more
than a few minutes old. `X-Webhook-Event` names the event and
`X-Webhook-Delivery` identifies the delivery, so you can drop a repeat.

Answer with any 2xx to accept it. Any other answer, a network error or a timeout is
retried with backoff. Every try shows under **Deliveries** on the key, where
**Resend** sends one again.
