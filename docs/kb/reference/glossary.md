---
title: Glossary
section: Reference
order: 1
summary: Every term the application uses that is not self-explanatory, in plain language.
audience: everyone
last_verified: 2026-08-17
---

# Glossary

Terms as this application uses them. Where an industry meaning differs, the
application's meaning is what is written here.

For short forms - GRN, ASN, SKU, LPN and the rest - see
[Abbreviations](abbreviations.md).

## The shape of the data

**Record type.** A kind of record: Purchase Order, Item, Vendor. Defined as
configuration rather than written into the software, which is why an
administrator can add one.

**Master.** A record that exists in its own right and changes rarely - a
product, a supplier, a shop, a bin. You set masters up once.

**Transaction.** A record of something that happened at a point in time, which
refers to masters: a sale, a receipt, a transfer. Transactions accumulate; you
never "set them up".

**Document.** Any transaction record that moves through states and may need
approval.

**Field.** One piece of data on a record. **Fieldname** is its internal key;
**label** is what you see, and an administrator can change the label without
touching the data.

## Who can do what

**Role.** What kind of user you are - Cashier, Store Manager, Super Admin,
Supplier, or one your administrator created. It decides what you can see and do.

**RBAC.** Role-Based Access Control: permission follows the role, not the
person. The practical consequence is that access is changed by changing a role,
and reviewed by reading four role definitions rather than four hundred user
records.

**Fails closed.** A record type with no permission row for a role is denied to
that role. A missing grant is a denial, never a default allow.

**Tenant.** Your business's own private copy of the system. Other businesses on
the same installation cannot see your data - each tenant's records are held
separately in the database, not merely filtered in the application.

**Entitlement.** What your business has licensed. A module missing from
*everyone's* sidebar is usually an entitlement; one missing from *your* sidebar
is usually a permission.

## Approvals and control

**Maker-checker.** The rule that an important action needs a second person to
agree. The person who raises a document can never approve it, and that is not
configurable.

**Approval rule.** Which document types need approval, above what threshold, and
from whom.

**Re-approval.** Editing an approved document sends it back for approval. This
is deliberate: an approval is of a specific version, not of a document forever.

**Audit trail.** The record of who did what, when, to which record. Present on
every record and readable in full under **Settings » Activity Log**.

## Stock

**Available to sell.** What is genuinely free to promise a customer - physical
stock minus what is already reserved for other orders. This, not a raw count, is
the number that matters.

**Reservation.** Stock committed to a specific order. It exists from allocation
until the goods are picked.

**Allocation.** Deciding which location's stock fulfils which order line.

**Bin.** A specific place in a warehouse. **Bin condition** records a bin as
damaged, quarantined or blocked, so stock that exists but must not be sold is
visibly separated.

**Putaway.** Moving received stock into a bin. Until it is done, stock is in the
system but not in a place, and picking cannot route anyone to it.

**Replenishment.** Moving stock from bulk storage into pick faces before a
picker finds the face empty.

**Cycle count.** A rolling count of part of the warehouse, rather than a full
annual stop.

**Handling unit.** A carton or pallet identified as one thing, so it can be
moved or shipped without scanning each item inside.

## Orders and fulfilment

**Hold.** A stop on an order, or on one line of it, with a recorded reason code.
Holding a line releases the stock it was holding back into the pool.

**Expedite.** A priority flag honoured by the queues themselves - the order
sorts to the top of both the order console and the picking worklist, not just
displayed with a badge.

**Split.** Fulfilling some lines of an order separately from the rest. It stays
one order with one id and one invoice chain.

**SLA breach.** A task older than the threshold set for its type.

**Source.** Where an order came from - a channel, middleware, or entered by
hand - shown with that system's own order id, so you can confirm a marketplace
order actually arrived without asking anyone to check a database.

## Money

**GL / ledger.** The accounting record of every amount moving in or out.
*Double-entry* means every transaction has matching debits and credits that must
balance.

**Trial balance.** The check that they do. **Finance / GL** reports it as
*Balanced trial ledger* when debits equal credits as of a date.

**GST.** The sales tax added automatically, resolved from the product's HSN code
and the places of supply. You never type a tax rate onto a transaction.

**Tax treatment.** Why something is not taxed: Taxable, Exempt, Nil-Rated or
Zero-Rated. Used instead of entering a zero rate, which on its own is rejected
as indistinguishable from an unfilled field.

**Intra-state / inter-state.** Whether supplier and customer are in the same
state, which decides the form the tax takes. Derived from the GSTIN's first two
digits rather than asked.

**Three-way match.** Agreement between what was ordered, what was received and
what was invoiced. Where they disagree is the only part worth a human's time.

**FX gain or loss.** The difference between what a foreign-currency invoice was
booked at and what the money was actually worth when it arrived. Recorded
automatically.

## When things go wrong

**Error code.** The identifier on every refusal, such as `GLOBAL-0001`. It names
the exact condition rather than a category. See [Reading an error
code](error-codes.md).

**Correlation id.** A tracking code identifying one request in the server's
logs. Quoting it turns "something failed yesterday" into a specific line an
administrator can read.

**Idempotency key.** A safeguard so that the same request arriving twice - a
network retry, a double-click - takes effect only once.

## Security

**MFA / TOTP.** A second sign-in check using a time-based code from an
authenticator app.

**Recovery codes.** Ten single-use codes shown once when MFA is set up. Only a
fingerprint of each is stored, so nobody - including your administrator - can
look one up for you later.

**Rate limit.** A cap on how fast requests of some kind are accepted. Five
failed sign-ins in a minute is a rate limit, not a lockout; a minute's wait
clears it.

**API key.** A credential issued to an integration rather than a person. It
carries explicit scopes, can be rotated and revoked, and is shown in full
exactly once.
