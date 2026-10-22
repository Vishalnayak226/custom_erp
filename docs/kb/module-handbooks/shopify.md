---
title: Shopify
section: Module Handbooks
order: 46
summary: Connect a Shopify shop - products with their variants, prices and stock per location go out from the ERP; whole orders, changes, refunds and payouts come in; shipping, cancellations and RTO go back.
audience: admin, OMS staff, store manager
last_verified: 2026-10-22
screens: [oms, doctype:channel, doctype:channelsyncrun]
doc_id: DOC-578C0E348B
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2026-11-21
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: how-to
module: channel-connectors
task: Shopify
prerequisites: An administrator for the setup; a Shopify custom app with the scopes listed below
---

# Shopify

The ERP owns **products, prices and stock**; Shopify owns **online orders and
their payments**. Prices and stock changed in Shopify admin are overwritten at
the next send.

## Connect (administrator)

1. In Shopify: **Settings → Apps and sales channels → Develop apps → Create an
   app**. Give it `read_products`, `write_products`, `read_inventory`,
   `write_inventory`, `read_locations`, `read_orders`, `write_orders`,
   `read_customers`, `write_merchant_managed_fulfillment_orders` (and
   `read_shopify_payments_payouts` for payouts). Install it; copy the **Admin
   API access token** and the **API secret key**.
2. In the ERP: **Setup ▸ PIM ▸ Channel → + New** with Platform **Shopify**,
   save, then the row's **Shopify** button.
3. **Connect**: shop address, token, secret → **Save** → **Test connection**
   (it names any missing access).
4. **Stores**: map each Shopify location to a store; set the safety buffer and
   the price list; **Save**.
5. **Webhooks → Register**.
6. **Publish designs not on Shopify yet** (or **Link existing Shopify
   products** if the shop already sells them).

## What happens by itself

- Every sync interval (15 minutes by default): new and changed orders, changed
  prices, changed stock per location. Once a day: all stock, payouts.
- An order comes in whole - number, discounts, shipping, GST, payment, COD,
  customer by mobile number - once only.
- Edited or cancelled in Shopify before picking starts: applied. After: it
  waits in **OMS → Channel changes**; do it by hand, then **Done**.
- Dispatched here: Shopify gets the fulfilment with carrier and tracking.
  Cancelled here: cancelled there. RTO here: tagged there.
- Refunded in Shopify after invoicing: a Customer Return is raised here.
- Shopify Payments payouts: settlement lines, reconciled with the orders; fees
  to the fees account.

## If something is wrong

- **The Shopify connection is not ready for this** (CONN-0994): save the shop
  address and token, test the connection, map a location.
- Orders missing: **Register again**, then **Pull orders now**.
- **Invalid webhook signature** in the log: the webhook secret is not the
  app's API secret key.
- Shopify shows a different stock: **Stock mismatch report**, tick, **Send
  now**.

Full steps: Admin Guide B.3.20; working the orders: User Guide §9A.14.
