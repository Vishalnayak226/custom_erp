---
title: Courier Integrations
section: Module Handbooks
order: 46
summary: Book a shipment, check serviceability, print a label, and track it to delivery or RTO — the manual carrier-agnostic flow every warehouse uses today, and the real Delhivery/Shiprocket API layer built alongside it but not yet reachable from a screen.
audience: warehouse operator, admin
last_verified: 2026-09-03
screens: [marketplace, oms, doctype-table]
---

# Courier Integrations

A **courier integration** covers four things: checking which carriers can
even deliver to a pincode (serviceability/rates), booking a shipment and
getting back an AWB (airway bill) number, scheduling a pickup and printing a
label, and tracking the shipment through to Delivered or RTO (return to
origin). This ERP has **two separate systems** doing pieces of this, built
at different stages, and it matters which one a screen or a document is
using:

- **The manual booking flow** (Stage 26.12.4) — `CourierServiceArea` master
  data plus `LogisticsBooking` documents, manifests, and hand-entered
  tracking updates. This is what the **Marketplace** screen's Logistics
  Bookings panel actually drives, and it is real and fully usable — but it
  does not call a courier's live API. Its own code comment says so plainly:
  *"the AWB number is a locally-generated placeholder — a real courier API
  call is deliberately out of scope here."* Serviceability comes from a
  configured priority list, not a live rate check.
- **The courier adapter engine** (Stage 35.5) — a real `CourierAdapter` per
  provider (Delhivery, Shiprocket today), encrypted credentials, and actual
  outbound calls for AWB allocation, pickup scheduling, cancellation, rate
  shopping, and signed webhook-driven tracking ingestion, plus an NDR
  (non-delivery report) case workflow and a vector-barcode PDF label. This
  layer is code-complete and tested (`engines/courier_stage35_test.go`) —
  see the gap noted below for what's missing to actually use it day to day.

## The shared model (`engines/courier.go`)

Every provider implements one `CourierAdapter` interface:
`AllocateAWB`, `SchedulePickup`, `CancelShipment`, `Rates`, and
`ParseTrackingWebhook`. Operational code never branches on provider name —
it calls whichever adapter `courierAdapter(provider)` resolves, and the five
functions below are the real entry points, all in `engines/courier.go`:

- **`AllocateCourierAWB`** — claims the booking (an atomic UPDATE so two
  concurrent allocation attempts for the same booking can't both mint a
  real, billable AWB), calls the adapter, and only writes `provider_awb`
  once the provider has actually accepted the shipment. A failed call
  releases the claim, leaving the booking retryable.
- **`ScheduleCourierPickup`** / **`CancelCourierShipment`** — need an
  existing `provider_awb` on the booking first.
- **`ShopCourierRates`** — calls every requested provider's `Rates`,
  isolates a single provider's failure from the others, and returns only
  serviceable quotes sorted by charge then ETA.
- **`IngestCourierTrackingWebhook`** — idempotent per provider event id,
  maps the provider's own status vocabulary onto the shared
  `In-Transit`/`Delivered`/`NDR`/`RTO` states via `normalizeCourierStatus`,
  and routes a delivery-attempt failure into an **NDRCase** rather than
  jumping straight to RTO.

## Credential storage

Courier credentials reuse the exact same encrypted store channel connectors
use — `SaveCourierCredential` calls the same `SaveChannelCredential` under
the synthetic channel code `courier:<provider>` (e.g. `courier:delhivery`).
Same AES-256-GCM encryption at rest, same HR/Admin-only gate
(`engines.IsSuperAdmin`), via:

```
POST /api/v1/marketplace/couriers/{provider}/credentials
GET  /api/v1/marketplace/couriers/{provider}/credentials
```

## Delhivery

Credential fields: `api_token` and `pickup_name` (both required — AWB
creation is refused without a configured pickup location name). AWB
creation `POST`s form-encoded JSON to `/api/cmu/create.json`; pickup
scheduling and cancellation are separate calls; rate shopping reads
Delhivery's `kinko` invoice-charges endpoint, and COD vs. prepaid changes
which query parameters are sent. Inbound tracking webhooks are parsed from
Delhivery's nested `Shipment.Status` JSON shape.

## Shiprocket

Credential fields: either a static `access_token`, or `email` + `password`
(the connector performs the login call itself and uses the returned token).
Optional `courier_id` pins a specific carrier at AWB-assignment time instead
of letting Shiprocket auto-select one. **A real limitation, not a bug**:
`AllocateAWB` requires a `remote_shipment_id` already present on the
booking — Shiprocket's own flow is order-creation-then-AWB-assignment as two
separate steps, and this codebase implements only the second step. Nothing
here calls Shiprocket's order-creation endpoint to obtain that id, so an
AWB allocation against a Shiprocket-provider booking fails with *"shiprocket
AWB allocation requires remote_shipment_id from its order-creation
response"* unless something else already wrote `remote_shipment_id` onto
the `LogisticsBooking`.

## Webhook verification and the manifest/RTO flow

Both providers' inbound tracking webhooks are HMAC-SHA256 verified
(`VerifyCourierWebhook`) against a per-provider `webhook_secret` credential
field, checked against the `X-Courier-Signature` header at
`POST /api/v1/integration/courier/{provider}/tracking`. A verified event
that resolves to `Delivered` calls `RecordDeliveryEvent`; one that resolves
to a delivery failure opens or updates an `NDRCase` via `RecordNDR`; `RTO`
calls `RecordRTO` — the same three functions the manual flow's own
**Mark In-Transit** / **Mark Delivered** / **Report RTO** buttons call by
hand (see below), so a booking is not locked into "manual" or "automatic"
forever — whichever one reports a status first wins.

## Day to day: the Marketplace screen's manual flow

**Marketplace → Logistics Bookings** (`renderMarketplaceView`) is what a
warehouse actually uses today:

1. **Book** a shipment: Order ID, optional Fulfillment Task, Destination
   Pincode, Carrier (leave blank to auto-select), optional Tracking Number,
   Shipping Charge. A blank Carrier with a Destination Pincode auto-selects
   the highest-priority **CourierServiceArea** row whose `pincode_prefix`
   matches (**Setup → Advanced → Courier Service Area** — it's flagged
   `setup_advanced` so it files behind Setup's Advanced divider rather than
   the everyday master-data list; fields are `courier`, `pincode_prefix`,
   `priority`, `status`) — a prefix match, not a real geo/zone lookup. No
   `CourierServiceArea` configured at all leaves
   the feature usable with a manually-typed carrier. A blank carrier with no
   pincode and nothing configured to auto-select against is refused
   (`WMSLOG-0137`, "logistics partner required"); a pincode with no
   serviceable row configured for it is refused as `WMSLOG-0138`
   ("AWB generation failed").
2. **Manifests** group every AWB-assigned booking for one courier at one
   location; **Hand Over Manifest** dispatches every fulfillment task in it
   and flips an order to Shipped once all its tasks have shipped.
3. Per booking: **Print Label** (auto-picks the printer whose Default For is
   Shipping Label via QZ Tray, falling back to the on-screen **Label** view
   if QZ Tray isn't running), **Mark In-Transit**/**Mark Delivered**
   (`RecordDeliveryEvent`, `WMSLOG-0139` on failure), and **Report RTO**
   (`RecordRTO`) once a booking is Handed Over.

None of this — booking, label, tracking marks — calls a real courier API.
The AWB shown is generated locally; "Carrier" is free text or an
auto-selected `CourierServiceArea` row, not a live Delhivery/Shiprocket
account.

## Rate shopping and reverse pickup (built, not wired)

Two more real, tested pieces of the Stage 35.5 engine have no screen at all
today:

- **`GET /api/v1/marketplace/couriers/rates`** — calls `ShopCourierRates`
  across the requested providers (`providers=delhivery,shiprocket` by
  default) for a given origin/destination pincode, weight and COD amount,
  and returns real quotes sorted cheapest-then-fastest.
- **`POST /api/v1/returns/{id}/reverse-pickup`** — `ScheduleReturnReversePickup`
  books a real reverse-pickup AWB and schedules the pickup for an Approved
  Customer Return, reusing the exact same `AllocateCourierAWB`/
  `ScheduleCourierPickup` calls a forward shipment uses.

> [!WARNING]
> **Known gap, current as of this writing.** The entire Stage 35.5 courier
> adapter engine — credential storage, real Delhivery/Shiprocket AWB
> allocation, pickup scheduling, cancellation, rate shopping, the NDR case
> workflow, the vector Code128 PDF label, and the return reverse-pickup
> endpoint — is real and tested, but **no screen in the app calls any of
> it**. Confirmed by checking every one of its endpoints against `app.js`:
> `POST/GET /api/v1/marketplace/couriers/{provider}/credentials`, `/awb`,
> `/pickup`, `/cancel`, `GET /api/v1/marketplace/couriers/rates`,
> `POST /api/v1/marketplace/ndr/{id}/resolve`,
> `GET /api/v1/marketplace/logistics/label.pdf`, and
> `POST /api/v1/returns/{id}/reverse-pickup` have zero callers in the
> frontend. `NDRCase` also has no Setup-menu entry the way `CourierServiceArea`
> does, so an open NDR case can't currently be browsed from the app either.
> Until a screen is built, every one of these needs a direct authenticated
> API call by an admin or integrator. This is the courier-side counterpart
> of the credential-configuration gap in
> [Channel Connectors](channel-connectors.md).

## The `oms_integration` feature flag

Every route under `/api/v1/marketplace/*` — couriers included — runs
through `featureGate("oms_integration", ...)`. A tenant without that
feature enabled gets a `403` on all of them (a generic "Feature 'oms_integration'
is disabled for this tenant" message), which looks identical to a
permissions problem. If every button on the Marketplace/OMS screens fails
the same way, check with an admin whether the feature is enabled before
assuming something is broken — the same first-check pattern the WMS module
uses for `moduleGate("wms", ...)`.

## Troubleshooting

**A booking's Carrier field auto-fills but Destination Pincode was left
blank.** Auto-select only runs when a pincode is given; with neither a
pincode nor a typed carrier, booking is refused outright
(`WMSLOG-0137`). Type a carrier, or fill in the pincode.

**"AWB generation failed" (`WMSLOG-0138`) on Book.** No **CourierServiceArea**
row's `pincode_prefix` matches the destination pincode you entered (or none
are configured with a matching prefix). Either add/adjust a
CourierServiceArea row, or type a carrier manually to bypass auto-select.

**Delivery status update failed (`WMSLOG-0139`).** `RecordDeliveryEvent`
rejected the transition — usually a booking already in a terminal state
(`Delivered`/`RTO`) being marked again. Check the booking's current status
before retrying.

**A Shiprocket AWB allocation fails with "requires remote_shipment_id."**
This is the real limitation described above — this codebase's Shiprocket
adapter only implements AWB assignment against an order Shiprocket already
knows about, not order creation itself. Not fixable from the app today;
needs `remote_shipment_id` supplied by whatever process created the
Shiprocket-side order.

**A courier tracking webhook is rejected as unauthorized.** `VerifyCourierWebhook`
checks the `X-Courier-Signature` header as HMAC-SHA256 against that
provider's `webhook_secret` credential field. Either the secret configured
here doesn't match what's registered on the courier's side, or no
`webhook_secret` was ever set for that provider.

**Print Label does nothing visible.** It's trying QZ Tray first (a
thermal-printer bridge) and falling back to the on-screen **Label** view
only if that's not running — if neither shows anything, use the **Label**
button directly to confirm the label itself renders before troubleshooting
QZ Tray.

## What is not here yet

**Two systems, one wired, one not.** The manual booking/manifest/RTO flow
on the Marketplace screen is complete and is what actually ships product
today — but it never calls a real courier account, so its AWB numbers,
rates and carrier selection are all local approximations. The real
Delhivery/Shiprocket integration (live AWB, live rate shopping, NDR
workflow, reverse pickup, the proper barcode label) is built and tested but
has no screen — see the warning above. Bringing the two together — the
Marketplace booking screen calling the real adapter instead of generating a
placeholder AWB, once credentials exist — is the natural next step and is
flagged in `docs/micro_checklist.md`.

See also [Channel Connectors](channel-connectors.md) for how an order
reaches the ERP in the first place before it ever needs a courier.
