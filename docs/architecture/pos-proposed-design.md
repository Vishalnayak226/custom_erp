---
doc_id: DOC-ARCH-POS-PROPOSED
title: POS proposed design
type: reference
status: draft
owner: engineering-owner
approvers: [engineering-owner, store-process-owner]
audience: [engineering, product]
applies_to: proposed POS extensions; nothing here is current behaviour
authority: proposed-design
confidentiality: internal
last_verified: 2026-11-15
review_by: 2027-02-13
supersedes: none
superseded_by: none
---

# POS proposed design

**Proposed, not built.** This page holds the parts of the original POS
specification that the product does not have. What the till does today is in
the [POS handbook](../kb/module-handbooks/pos-operations.md); supported scope
is in the [capability catalog](../generated/capability-catalog.md). The
original specification, with its design rationale, is kept as history in
[archive/legacy/pos-architecture.md](../archive/legacy/pos-architecture.md).
Its own "mostly not yet built" note is out of date: most of it has since been
built, differently in places.

## Where each part of the original specification stands

Checked against the source on 2026-11-15 (48.4.5).

| Part | Status | Current fact lives in |
|---|---|---|
| POS Profile | Built, smaller: optional on a session; holds default payment mode, number series and opening float. No per-profile payment-mode list, default walk-in customer or discount cap. | POS handbook, *Before the first sale* |
| Cash opening and closing | Built as the `POSSession`: the expected cash is the X-report's figure, an over-tolerance close needs a reason and can go for approval, and the variance posts to 5295. | POS handbook, *Till lifecycle* |
| Discount limits | Built differently: offers carry their own caps, and a large manual discount sends the cart for a manager's approval. | POS handbook, *Discounts and offers* |
| Offline billing | Built differently: a queue of pending sales in the browser's `localStorage`, replayed with the same cart number (idempotent), then held until a manager validates it. No local catalog or price cache (IndexedDB). | POS handbook, *Offline mode* |
| Checkout document | Built differently: a sale is a `POSCart` completed by `POST /api/v1/checkout`, not a `SalesInvoice` saved through the generic document API. | [Current architecture](current-architecture.md) |
| Split tender | Built (73.1.6): one bill paid in several modes, each printed under the total. | POS handbook, *Ringing up a sale* |
| Barcode scanning | Built, keyboard-driven (Enter, arrow keys). The `Ctrl+D` / `Alt+G` keys in the original are not. | POS handbook, *Ringing up a sale* |
| Restaurant checkout | **Proposed** - below | - |
| Service checkout | **Proposed** - below | - |

## Proposed: restaurant checkout

For a client that serves food at tables. None of it exists.

- **Table and guests on the cart:** a table (or seat) reference and a guest
  count on the open cart, so several carts can be open at once, one per table.
- **Kitchen order tickets (KOT):** sending a cart's new lines to a kitchen or
  bar printer before the bill is paid, using the existing print routing.
- **Split bill by guest or by line:** one table's order settled as several
  bills. Today's split is by payment mode only.

**Needs deciding before any build:** whether more than one open cart per till
is allowed (today one session, one cart at a time), where a table lives (a
Location sub-record or a new doctype), and how a KOT reprint and a voided
kitchen line are controlled. Record the answers as an ADR in
[decisions/](decisions/decision-register.md) first, as 48.4.4 requires for any
schema change.

## Proposed: service checkout

For a client that sells time (a salon, a repair desk). None of it exists. The
Appointment Calendar is for dock doors and does not cover this.

- **Booked slot on the sale:** a sale line tied to a time slot and the staff
  member who gave the service.
- **Staff commission:** a commission figure per service line, reported per
  staff member.

**Needs deciding before any build:** whether the slot is booked in the ERP or
only recorded at the till, and whether commission is paid through payroll (the
HR module) or only reported.

## Proposed: industry block on the sale

The original kept a free-form `industry_context` block on the sale
(`type`, `table_number`, `number_of_guests`, `kot_id`). If the restaurant or
service designs above are approved, their fields go on the cart as ordinary
fields through the doctype metadata, not in a separate untyped block, so
validation, reports and the data dictionary see them.
