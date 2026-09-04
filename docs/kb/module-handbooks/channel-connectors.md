---
title: Channel Connectors
section: Module Handbooks
order: 45
summary: Push product content out to Shopify, BigCommerce, Magento, Amazon, Flipkart and a dozen private-contract marketplaces through one shared readiness/queue/retry pipeline, and pull their orders back in through the same connector.
audience: category manager, admin, integrator
last_verified: 2026-09-03
screens: [pim, oms, doctype-table]
---

# Channel Connectors

A **channel connector** is the piece of code that knows how to talk to one
specific outside platform — Shopify's GraphQL API, BigCommerce's REST
catalogue, Magento's REST products endpoint, or a private marketplace
contract like Myntra's. Every connector implements the same small interface
(`PublishProduct` plus a declared `RateLimit`), so the catalogue-publish
pipeline that queues, retries and logs a publish attempt never has to know
which platform it's actually talking to. This ERP ships two families built
on that interface:

- **The PIM catalogue connectors** — Shopify, BigCommerce, Magento/Adobe
  Commerce — push one Item's approved content out to a storefront. This is
  the older, catalogue-only half.
- **The omnichannel connector SDK** — Amazon, Flipkart, WooCommerce, and a
  set of private-contract marketplaces (Myntra, Meesho, Ajio, Nykaa) and
  quick-commerce platforms (Blinkit, Zepto, Swiggy Instamart) — adds pulling
  orders in and pushing inventory/order-status out, on top of the same
  catalogue publish. Registering one of these connectors registers it into
  *both* the omnichannel registry and the older publish registry
  (`registerOmnichannelConnector` calls `registerConnector` internally), so
  an Amazon or Flipkart item queues and retries through the exact same
  pipeline a Shopify item does.

## The shared publish pipeline: readiness, queue, worker, log

Every catalogue publish — whichever platform — goes through the same five
steps, all in `engines/pim_publish.go`:

1. **Readiness check** (`CheckPublishReadiness`). An item must be 100%
   complete for the channel's `default_locale` (per `CalculateCompleteness`,
   which already accounts for that channel's own mandatory field mappings),
   have its ERP `category` mapped to the channel via a **ChannelCategoryMap**
   row, and pass every **ChannelValidationRule** configured for the channel
   (`Min Images`, `Max Title Length`, `Required Tag` — a channel with no
   rules configured is unaffected). Any failure is returned as a
   human-readable missing-fields list, not a publish attempt.
2. **Queue** (`QueuePublish`). Inserts a `pim_publish_queue` row with status
   `Queued`. Idempotent: the payload is hashed (item + channel + approved
   title/short/long description for the channel's locale), and re-queuing an
   unchanged item/channel against an existing Queued/Processing/Published job
   returns that job instead of creating a duplicate.
3. **Background worker** (`StartPublishQueueWorker` / `processPublishQueue`).
   A ticker picks up to 10 `Queued` rows per tenant schema per tick, resolves
   the right connector by the channel's `platform` field
   (`resolveConnector`), and calls it.
4. **Connector call**, rate-limited two ways before the platform is ever
   touched: a per-channel token bucket (`allowConnectorCall`, sized by the
   connector's own declared `RateLimit()`) and a per-platform circuit breaker
   inside the shared outbound helper `doConnectorRequest` (opens after 5
   consecutive failures, cools down 30s, then lets one trial call through). A
   budget-exhausted or breaker-open call leaves the job `Queued` for the next
   tick rather than failing it.
5. **Log and retry**. Every attempt writes a `pim_publish_log` row
   (`external_id` on success, `error_message`/`error_code` on failure). A
   real failure increments `pim_publish_queue.retry_count`; a rate-limit/
   breaker condition (`CONN-0225`) does not — it's treated as "try again next
   tick," not a failed attempt.

This is the pipeline behind the **Channel Publishing** section of an item's
detail panel in the **PIM Workbench** (`renderPIMDetailPanel` — PIM →
Workbench → select an item): a Channel dropdown plus **Preview** and
**Publish** buttons. Preview calls `GET /api/v1/pim/publish-preview`,
Publish calls `POST /api/v1/pim/publish`, and the item's publish history
underneath reads from `GET /api/v1/pim/publish-log`.

## Credential storage

Every connector's credentials (an access token, a shop domain, a store
hash — whatever fields that platform needs) are encrypted at rest with
AES-256-GCM (`encryptChannelCredential`) and can only be written by an
HR/Admin session (`engines.IsSuperAdmin`). Two save endpoints exist,
depending on which registry the platform lives in:

- `POST /api/v1/pim/channels/{code}/credentials` — Shopify, BigCommerce,
  Magento/Adobe Commerce.
- `POST /api/v1/marketplace/channels/{channel}/credentials` — Amazon,
  Flipkart, WooCommerce, and every private-contract channel. This path
  additionally runs the connector's own `ValidateCredentials` check before
  saving, so a channel missing a required field is rejected at save time
  instead of failing on the first real publish.

Both routes read back through the same `getChannelCredential`/
`LoadConnectorCredential` accessor a connector call uses, so there is one
credential store behind either save path, not two.

> [!WARNING]
> **Known gap, current as of this writing.** Credential storage, validation
> and every connector call are real, tested code — but no screen in the app
> calls either credentials endpoint. The only credential-configuration
> screen that exists today is **Settings → Configuration → Integrations**, and it
> covers exactly two unrelated integrations (Pine Labs terminals,
> Unicommerce middleware) — it has no entry for Shopify, BigCommerce,
> Magento, Amazon, Flipkart, WooCommerce or any private-contract channel.
> Until that screen (or an equivalent) is added, a channel's `access_token`/
> `shop_domain`/`store_hash`/etc. have to be set with a direct authenticated
> API call by an admin or integrator — see `docs/operations/connector_live_verification.md`
> for the exact procedure this codebase already uses to do that for its own
> verification passes. The same gap applies to `POST /api/v1/pim/channels/{code}/pull-state`
> (reads a product's live state back from the platform to detect drift) —
> real and tested, but nothing in the app triggers it either.

## Shopify

Uses the Admin **GraphQL** API (`productSet` mutation), not the legacy REST
product endpoints. Credential fields: `shop_domain` (e.g.
`mystore.myshopify.com`) and `access_token` (a private/custom-app Admin API
token — there is no OAuth authorize-redirect flow, since this ERP runs its
own store rather than being distributed to other merchants). Media upload is
a real 3-step staged-upload flow (Shopify does not accept inline binary in
GraphQL): request a signed target per image, `PUT` the bytes, then attach
the staged resource to the product — a media failure logs but does not fail
the publish, since the product itself already saved. `RateLimit()` returns a
conservative static floor (20/minute); the live GraphQL cost/throttle status
is parsed and logged on every call for visibility, though the bucket itself
does not yet adapt to it. Each ERP Item publishes as its own standalone
Shopify product — grouping an ERP parent+variant family into one Shopify
product with real Shopify variants is explicitly out of scope for this pass.

## BigCommerce

Uses the REST v3 Catalog API. Credential fields: `access_token` and
`store_hash` (from the store's admin URL). Image upload is a single direct
multipart `POST` — no staged-upload dance the way Shopify needs. BigCommerce
order intake (`ImportBigCommerceOrder`) reads a webhook's bare order id back
over REST v2 (BigCommerce's webhook body carries no order content, unlike
Shopify's), and native webhook signatures are verified as hex HMAC-SHA256
(`VerifyBigCommerceWebhook`) — subscribing to BigCommerce's own webhook API
is a one-time setup action against BigCommerce itself, not something this
connector automates. `RateLimit()` is 100 requests/30s, a safe floor under
even BigCommerce's lowest documented plan tier.

## Magento / Adobe Commerce

One implementation covers both editions (Adobe Commerce is Magento
underneath), differing only in `auth_mode` (`"OpenSource"` or
`"AdobeCommerce"`). Credential fields: `base_url` (host only, no scheme),
`access_token` (a Magento Integration token, or an already-issued Adobe IMS
token — this connector does not implement IMS's own OAuth token-refresh
flow, so the token is expected to already be valid), `auth_mode`, and
optional `store_view_code` (defaults to `"default"`). Media is base64-
encoded inline in the JSON body, unlike either of the other two. Because
Magento Open Source has no native webhooks, `StartMagentoPollWorker` polls
every `auth_mode: OpenSource` channel's `/orders` endpoint on an interval and
imports whatever changed since the last poll (idempotent on `increment_id`);
Adobe Commerce channels are excluded from polling since they get real
webhooks instead. `RateLimit()` is a generic 30/minute floor — Magento does
not document a fixed cost-based budget the way Shopify/BigCommerce do.

## Amazon (Selling Partner API)

Registered through the omnichannel SDK (`amazonSPConnector`), so it also
pulls orders and pushes inventory/status, not just catalogue. Credential
fields: `seller_id`, `marketplace_id`, and either a static `access_token` or
a `refresh_token` + `lwa_client_id` + `lwa_client_secret` triple (the
connector performs the LWA refresh-token exchange itself when no static
token is given). `PullOrders` filters out `Canceled`/`Pending`/
`Unfulfillable` orders before they ever reach the ERP, so a cancelled Amazon
order can't be imported and picked. `RateLimit()` is a strict 5/second,
reflecting Amazon's own documented SP-API throttling.

## Flipkart

Also an omnichannel SDK connector (`flipkartConnector`). Credential fields:
either a static `access_token`, or `app_id` + `app_secret` (the connector
performs the client-credentials OAuth exchange itself). Optional `location_id`
scopes order pulls and inventory pushes to one fulfillment location; optional
`product_id` is required for `PublishProduct`. Order pull reads Flipkart's
shipment-filter API (pre-dispatch states only) rather than a generic orders
endpoint. `RateLimit()` is 20/minute.

## Private-contract channels (Myntra, Meesho, Ajio, Nykaa, Blinkit, Zepto, Swiggy Instamart) and WooCommerce

These platforms' real APIs are negotiated per-integration rather than
public, so `partnerRESTConnector` is one generic implementation whose four
operation paths are supplied as credential fields rather than hard-coded:
`base_url`, `access_token`, and `orders_path`/`inventory_path`/
`catalogue_path`/`status_path` — all four are required at save time
(`ValidateCredentials` rejects a channel missing any of them), since there
is no fallback default the way Magento's `store_view_code` has one. Myntra/
Meesho/Ajio/Nykaa register as `kind: marketplace`; Blinkit/Zepto/Swiggy
Instamart register as `kind: quick_commerce` (additionally
`RequiresLocation`/`NoSplit`, since a quick-commerce order fulfills from one
dark store, not split across locations). `RateLimit()` is a shared 30/minute
floor across all seven.

WooCommerce (`wooCommerceConnector`) is the one connector here with a truly
public, self-hosted API — REST v3 at `/wp-json/wc/v3`, Basic-auth credential
fields `consumer_key`/`consumer_secret` — registered as `kind: webstore`
rather than `marketplace`. `RateLimit()` is 100/minute.

## Operating channels day to day: the OMS Workbench

**Order Management** (`renderOMSWorkbenchView`) is where a connector's
ongoing health is watched and acted on, once credentials exist:

- **Channel connectors** panel — one row per configured channel, reading
  `GET /api/v1/marketplace/connectors/health`: last sync status/time, sync
  lag, 24-hour failure count, and open SKU-mapping exceptions. A
  **Pull orders** button appears for a channel whose connector declares
  `PullOrders`; a **Push ATS** button appears for one declaring
  `PushInventory` — both call
  `POST /api/v1/marketplace/channels/{channel}/{pull-orders|sync-inventory}`.
- **Unmapped SKU exceptions** panel — a channel SKU an inbound order
  referenced with no ERP item mapped to it yet; map it to a SKU (and
  optionally an external product id / location) via
  `POST /api/v1/marketplace/sku-mappings`, resolving the exception.

## The 26.4.8 error dictionary

Before this dictionary existed, every connector failure that wasn't a
missing-credential (`CONN-0224`) collapsed into the same generic
`CONN-0226` ("Channel publish failed"). `classifyConnectorError`
(`engines/connector.go`) now matches the platform's own error wording —
Shopify's GraphQL `userErrors`/top-level `errors[]` messages, BigCommerce's
error `title`, Magento's error `message` — against a fixed, ordered list of
substrings, case-insensitive, first match wins:

| Platform wording contains | Code | Meaning |
|---|---|---|
| "already been taken" | `CONN-0228` | Duplicate SKU on channel |
| "already exists" | `CONN-0228` | Duplicate SKU on channel |
| "duplicate" | `CONN-0228` | Duplicate SKU on channel |
| "can't be blank" | `CONN-0227` | Channel field mapping missing |
| "cannot be blank" | `CONN-0227` | Channel field mapping missing |
| "must not be blank" | `CONN-0227` | Channel field mapping missing |
| "is required" | `CONN-0227` | Channel field mapping missing |
| "rate limit" | `CONN-0225` | Channel API rate limited |
| "throttle" | `CONN-0225` | Channel API rate limited |
| "too many requests" | `CONN-0225` | Channel API rate limited |
| *(no match)* | `CONN-0226` | Channel publish failed (unchanged fallback) |

Two things worth knowing about this table: order matters (a specific
duplicate/blank-field pattern is checked before the generic throttle
catch-all, so a message combining both classifies as the more specific
one), and `CONN-0227`'s catalog description ("Channel field mapping
missing") originally meant *this ERP's own* field-map configuration — the
dictionary now also routes a platform's own "this field is required"
rejection there too, since it's the same practical symptom (a required
value never reached the platform) even though the cause can now be either
side. `CONN-0225` is also raised directly, with no message matching needed,
whenever the outbound rate limiter or circuit breaker itself blocks a call
before it's even attempted (`engines/connector_http.go`). Omnichannel SDK
connectors (Amazon/Flipkart/partner channels/WooCommerce) additionally map a
`401`/`403` HTTP status to `CONN-0224` and `429` to `CONN-0225` directly by
status code, before falling through to the same wording-based table
(`DefaultConnectorErrorCode`).

Every catalog code's own user-facing text (from
`internal/server/error_catalog_generated.go`):

| Code | Scenario | User-facing message |
|---|---|---|
| `CONN-0224` | Live connector credentials missing | "Channel credentials are missing. Please configure credentials before publishing." |
| `CONN-0225` | Channel API rate limited | "Channel is rate limiting requests. Publishing will retry automatically." |
| `CONN-0226` | Channel publish failed | "Product could not be published to {channel}. Please review the error details." |
| `CONN-0227` | Channel field mapping missing | "Channel field mapping is missing for required field {field name}." |
| `CONN-0228` | Duplicate SKU on channel | "SKU already exists on the selected channel. Please map to existing listing or change SKU." |

## Troubleshooting

**Publish is refused before it even queues, listing missing fields.** That's
`CheckPublishReadiness` — the item isn't 100% complete for the channel's
default locale, its category has no `ChannelCategoryMap` row for this
channel, or it fails a configured `ChannelValidationRule`. Fix the listed
field(s)/mapping and retry; nothing was queued, so there's no job to retry.

**A publish job sits `Queued` and never moves.** Either the per-channel
outbound budget is exhausted for this tick (`allowConnectorCall`) or that
platform's circuit breaker is open after repeated failures — both are
designed to leave the job `Queued`, not `Failed`, and clear on their own
(the token bucket refills every window; the breaker retries after a 30s
cooldown). Check `pim_publish_log` for the most recent attempt's
`error_code`; a stream of `CONN-0225` entries confirms this rather than a
stuck worker.

**`CONN-0224` — "Channel credentials are missing."** The channel has no
credentials saved, or a required field is blank. Per the known gap above,
this has to be set with a direct API call today — see
`docs/operations/connector_live_verification.md` for field names per
platform.

**`CONN-0228` — a SKU "already exists" on the channel.** The platform
already has a product for this SKU/handle from an earlier manual listing or
an earlier publish attempt under a different flow. Map to the existing
listing on the platform side, or change the SKU, before retrying.

**A Shopify/BigCommerce/Magento product published but has no image.** Media
upload failures are logged but deliberately non-fatal — the product record
itself is the primary outcome. Check the server log for the platform's own
media-rejection message (wrong MIME type, file too large) rather than
assuming the whole publish failed.

**An Amazon/Flipkart/marketplace order never showed up in Order Management.**
Confirm the channel's connector declares `PullOrders` (its connector health
row only shows a **Pull orders** button when it does) and that button has actually
been run — pulling is on-demand from that button, not automatic on a timer,
except for Magento Open Source's poll worker.

**A marketplace order is stuck as an "Unmapped SKU exception."** The
channel's SKU on the order has no `ChannelSKUMapping` to an ERP item yet.
Map it from the exceptions panel; the next pull or sync resolves it.

## What is not here yet

**Configuring a connector's credentials, and reading back a product's live
drift state, are both real, tested server actions with no app screen.**
Everything downstream of "credentials already exist" — readiness, queue,
worker, connector call, retry, the OMS health/exceptions panels — is fully
wired and usable from the app. Getting a channel from zero to its first real
publish still needs an admin or integrator to call
`POST /api/v1/pim/channels/{code}/credentials` (or the `/marketplace/`
equivalent) directly. Building a screen for this — even reusing the
existing **Settings → Configuration → Integrations** pattern already used for Pine
Labs and Unicommerce — is the natural next step, flagged in
`docs/micro_checklist.md`.

Shopify variant grouping (an ERP parent+variant family becoming one Shopify
product with real Shopify-side variants), Magento/Adobe Commerce's own IMS
OAuth token refresh, and a fully adaptive (rather than static-floor) rate
budget for Shopify are all stated, deliberate scope cuts in the connector
code itself, not gaps found during this pass.

See also [Courier Integrations](courier-integrations.md) for the shipping
side of a channel order once it's in the ERP.
