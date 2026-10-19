---
title: Procurement
section: Module Handbooks
order: 30
summary: Turn a need into stock on the shelf — raise a requisition, shop it round vendors with an optional RFQ, commit to a fully tax-classified Purchase Order, and post the Goods Receipt that actually moves the stock.
audience: procurement officer, store manager, admin
last_verified: 2026-09-03
screens: [purchase-orders, purchase-requisitions, grn, rfq, purchase-returns, module:procurement, module:inbound, vendor-invoices, reports, approvals, configuration]
doc_id: DOC-7252AA9770
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: how-to
module: procurement
task: Procurement
prerequisites: Signed-in account with permission for the described task; observe the article prerequisites
---

# Procurement

A purchase in this ERP is four documents, not one: a **Purchase Requisition**
records that someone needs something, an optional **RFQ** collects and
compares vendor quotes, a **Purchase Order** commits to buying at a price, and
a **GRN** (Goods Receipt Note) is the only one of the four that actually moves
stock. Tax classification — HSN code, GST rate, whether the price already
includes GST, inter-state vs intra-state — is resolved server-side from the
Item and Vendor masters, never typed by hand on the PO screen, because a
second place to type the same classification is exactly how a PO and its GRN
end up disagreeing at the third decimal place. This handbook covers the full
chain plus vendor onboarding and PO amendment, going deeper than the
walkthrough in the earlier purchase-order walkthrough.

Most of what makes a PO usable today came out of one rebuild, **Stage 40.1
(2026-08-10)**. Before it, a PO recorded a vendor, a warehouse and one
hand-typed "Total Amount" — `createDraftPurchaseOrder` literally posted
`items: '[]'` regardless of what was in the cart, so there was no record of
what was being bought, nothing for the GST engine to classify, nothing for a
GRN to match against, and nothing to send a vendor. Everything from
*Purchase Order composer* onward describes that rebuilt screen.

## Before the first PO

### Vendor onboarding

A vendor is a **Master** doctype (**Procurement → Vendors**, the same
`doctype-table` screen every Master record uses with `currentDoctype =
'Vendor'`) — created and edited on the generic create/edit form, with no
bespoke onboarding wizard. By default only **Super Admin** has
create/update/delete on `Vendor` (`role_permissions`); a Store Manager can
read a vendor (for example through the PO composer's vendor typeahead) but
cannot add or edit one.

The fields, in display order:

| Field | Notes |
|---|---|
| **Vendor Code** | Mandatory. |
| **Vendor Name** | Mandatory. |
| **GSTIN** | Optional, but does double duty — see below. Format-checked against a real 15-character GSTIN pattern (`MASTER-0049` if it fails) and auto-uppercased as you type. |
| **Bank Account Number** | Optional. Format-checked, 9-18 digits (`MASTER-0052`). |
| **Bank IFSC** | Optional. Format-checked and auto-uppercased. |
| **Contact Phone** / **Contact Email** | Optional. |
| **Status** | Active / Inactive. |
| **Address** | Added in Stage 40.1 — what the printed PO puts in the vendor's "To" block. Not recorded anywhere before that. |
| **State** | Added in Stage 40.1 — the fallback the inter-state calculation uses when the GSTIN alone doesn't settle it (§ GST on a Purchase Order, below). |

**Fill in GSTIN or State before raising a PO against a new vendor.** Neither
is mandatory to save the Vendor record, but the PO screen cannot work out
CGST+SGST vs IGST without one of them, and a vendor with neither shows an
amber "could not work the supply type out" banner on every PO raised against
it until it's fixed.

### Item prerequisites

Every PO line resolves its tax treatment from the **Item master** —
**HSN Code** and **GST Rate** — never from anything typed on the PO itself.
An item missing either flags only its own line red rather than failing the
whole document, so one bad SKU doesn't block the rest of the order; fix it
under **Setup → Items** and the line clears on the next preview.

## The chain: Requisition → RFQ → PO → GRN

**An RFQ is optional.** `ConvertRequisitionToOrder` targets **either** RFQ or
PurchaseOrder directly, and nothing gates raising a PO on an RFQ having
existed first (confirmed in Stage 40.1.8 — no code change was needed because
this was already true). If you already know who you're buying from, the
normal case, skip straight to a Purchase Order. Use an RFQ only when you
want to compare vendor quotes before committing.

Purchase Requisition and Purchase Order share one approval table
(`approval_rules`), so the same threshold governs both stages:

| Doctype | Amount | Required approver |
|---|---|---|
| PurchaseRequisition / PurchaseOrder | ₹0 – ₹49,999 | Store Manager |
| PurchaseRequisition / PurchaseOrder | ₹50,000+ | Super Admin |

Super Admin can also always approve, as the catch-all admin role
(`DecideApproval`). If a Store Manager tries to decide a PO at or above the
Super Admin threshold, the decision is refused with `PURCHA-0083`, not
silently allowed.

### 1. Purchase Requisition

**Procurement → Purchase Requisitions** is a plain `doctype-table` screen —
the requisition's schema is flat enough (no line items) that the generic
form/table already covers create and edit, with two row actions layered on
for what a plain form can't do:

- **code** — the requisition number, issued from the `PR` numbering series
  on save (location defaults to `HO` if none is given).
- **description** — free text. The first time a wording is saved, it's
  recorded as a reusable `PurchaseRequisitionDescription` (a SHA-256-derived,
  tenant-local id, so two concurrent saves of the same phrase converge on one
  master record) — future requisitions can pick it from a suggestion list
  instead of retyping it.
- **quantity**, **department**, **total_amount** (Estimated Amount) — all
  mandatory.
- **status** — `Draft` → `Pending Approval` → `Approved` (or `Rejected`) →
  `Converted`.

A **Store Manager** can create and update a `PurchaseRequisition` (not
delete). Row actions:

- **Submit for Approval** (Draft only) — posts to the same generic
  `POST /api/v1/approval/submit` every approval-gated doctype in this system
  uses (`{doctype: "PurchaseRequisition", document_id}`).
- **RFQ** / **PO** (Approved only) — converts the requisition. Both prompt
  for a store code and a financial year (the requisition itself doesn't
  carry either), then call `POST /api/v1/procurement/convert-requisition`
  (`ConvertRequisitionToOrder`), which draws a real number from the target's
  series and flips the requisition to `Converted` in the same transaction.

**A requisition converts exactly once.** `ConvertRequisitionToOrder`
row-locks the requisition and checks its status is `Approved`; a requisition
already `Converted` is refused with `RFQ-0251` rather than silently spawning
a second RFQ or PO — the row shows which document it became
(`converted_to_doctype` / `converted_to_id`) if you need to trace it. A
target doctype whose numbering series was never configured fails with
`ADMINC-0030` instead of drawing a duplicate number — contact an admin to add
the missing `prefix_configs` row.

### 2. RFQ and vendor quote comparison

**Procurement → RFQ / Quotes** is a bespoke screen (`rfq`) layered on the
same generic `RFQ`/`VendorQuote` doc API Vendor/Customer use, adding the
comparison view and the winner-selection action the generic endpoint doesn't
provide on its own.

- **New RFQ** — number auto-issued from the `RFQ` series, plus
  **description**, **quantity**, **target date**. Status starts `Draft`.
- **View Quotes** on an RFQ opens its comparison panel. **Submit Quote**
  records a `VendorQuote` (quote number, vendor, quoted price, lead time
  days) with status `Submitted`. `GetVendorQuotesForRFQ` returns them sorted
  by **quoted price ascending**, so the cheapest is always the first row.
- **Select as Winner** (`POST /api/v1/rfq/select-quote`, `SelectWinningQuote`)
  is one transaction that marks the chosen quote `Selected`, every other
  quote against the same RFQ `Rejected`, and the RFQ itself `Closed` — a
  partial selection (a winner marked but the RFQ left open) can't happen.

> [!NOTE]
> **Selecting a winning quote does not create a Purchase Order.** There is no
> "convert this quote to a PO" action anywhere in the app or the engine layer
> (confirmed by grep — no caller of `SelectWinningQuote`'s result exists
> beyond closing the RFQ, and `ConvertRequisitionToOrder` can't run a second
> time on the same requisition once it already produced the RFQ, per
> `RFQ-0251` above). After selecting a winner, raise the Purchase Order by
> hand on the composer, typing the vendor and lines the winning quote already
> specified. This is a real gap in the day-to-day flow, not a missing button
> on an otherwise-complete round trip — worth budgeting the extra step for.

### 3. Purchase Order composer

**Procurement → Purchase Order** (`purchase-orders`) is the rebuilt
composer. The four header fields:

| Field | Meaning |
|---|---|
| **Vendor** | Typeahead against `Vendor`. |
| **Location (billing entity)** | Which of your locations is buying — decides who the vendor invoices and half of the GST derivation. |
| **Target Warehouse (ship to)** | Where the goods should physically land. Often the same as Location; doesn't have to be. |
| **GST treatment of purchase price** | Per-PO override of the tenant default — see below. |

**Items** is a real line grid (`sku` / `qty` / `rate` / optional `mrp`) — the
`items` field is a `JSONTable`, the same column-spec pattern Stage 30.5.3
introduced for other line-item doctypes. HSN, GST %, Taxable, Tax and Line
Total are **read-only derived columns**, filled in from
`POST /api/v1/procurement/purchase-order/preview` (`PreviewPurchaseOrder`) on
a 250ms debounce as you type — nothing about tax is computed in JavaScript,
so what you see before saving is what the saved document will actually
store. A line that can't be priced (usually a missing HSN or GST rate) shows
a red row with its own error text instead of failing the whole preview; the
save button stays live but a save is refused (`Blocking: true`) until every
line clears.

**You don't type a PO number.** It shows "Auto (PO series)", greyed out, and
is issued on save — e.g. `PO/HO/26-27/000042` — the `PO` prefix series, same
numbering convention as every other transaction in this system.

**Only Super Admin can raise a brand-new PO from the composer.**
`role_permissions` gives Store Manager `read`+`update` on `PurchaseOrder`,
not `create` — `handlePreviewPurchaseOrder` deliberately checks create *or*
update so a Store Manager mid-amendment isn't 403'd, but the **Create Draft**
save itself still needs `create`. A Store Manager can still get a PO into
existence without it, though: **convert an Approved requisition straight to
PurchaseOrder** (§1 above) — `handleConvertRequisition` carries no
doctype-permission check of its own beyond the `procurement` module gate.

### GST on a Purchase Order: inclusive vs exclusive, and inter-state

**Whether the Purchase Price already includes GST** is a tenant setting,
`procurement.po_gst_mode` (**Settings → Configuration**, "Purchase Order
price GST treatment"), defaulting to **Exclusive**. Each PO can override it
(the **GST treatment of purchase price** dropdown; "Tenant default" resolves
to whatever the tenant actually has set, shown once the first preview
returns).

- **Exclusive** — the Purchase Price is the taxable value and GST is added
  on top. ₹450 at 5% comes to ₹472.50.
- **Inclusive** — the price already contains GST. ₹450 at 5% stays ₹450, of
  which ₹21.43 is tax.

This is deliberately a **separate function** from the sale-side GST
calculation: `ComputeGSTForLinesMode(tenantID, lines, interstate, mode)` adds
the inclusive/exclusive convention as an explicit parameter, while
`ComputeGSTForLines` (POS checkout, sales invoices, returns) keeps calling it
with `GSTModeInclusive` and stays byte-identical to before — so nothing about
existing sale-side tax changed by building this.

**Inter-state vs intra-state** is derived, not ticked. `ResolvePlaceOfSupply`
compares the state behind two masters:

- The **buying side**: the Location's `legal_entity` → that `LegalEntity`'s
  GSTIN (first two digits) → falling back to its `state` field.
- The **vendor side**: the Vendor's own GSTIN (first two digits) → falling
  back to its `state` field.

Same state → **CGST + SGST**. Different states → **IGST**. The banner above
the line grid states which and why (e.g. *"Inter-state (IGST) — vendor in
Karnataka (29), billing entity in Maharashtra (27)"*). If either side's state
can't be established, the banner turns amber and names exactly what's
missing rather than silently defaulting to intra-state — **Override** lets
you tick Inter-state by hand for that one PO, and the override sticks across
later saves rather than being silently recomputed away.

### Printing and sending

**Print** (`GET /api/v1/procurement/purchase-order/{id}/print`,
`BuildPurchaseOrderPrint`) assembles an A4 sheet server-side: both parties'
name/address/GSTIN, the HSN/tax table, the grand total, and the amount in
words (Indian digit grouping). It **re-prices from the stored lines** rather
than trusting a stored breakdown, so a PO edited by an API caller that
skipped the recompute can't print a total that disagrees with its own lines.
**MRP is stripped server-side** — twice over, in the print/send handlers and
again in the engine response — because the buying side's expected retail
price is not the vendor's business, and leaving it in the payload would put
it one "view source" away from the vendor on any forwarded copy.

> [!NOTE]
> There is no QZ Tray silent-print builder for a PO — `handlers_qz_print.go`'s
> `job_type` switch covers Shipping Label / Sticker / Receipt / Invoice, not
> Purchase Order, so **Print** deliberately goes straight to the browser's
> print dialog rather than attempting a silent print it already knows will
> fail. Separate piece of work if silent A4 PO printing is ever wanted.

**Send to Vendor** (`POST /api/v1/procurement/purchase-order/{id}/send`,
`MarkPurchaseOrderSent`) stamps `sent_to_vendor_at`, fires the
`PurchaseOrderIssued` event through the existing notification engine, and
returns a ready-made subject/body so the browser can open a `mailto:` link
to the vendor's contact email as a fallback when no notification channel is
configured — which is the normal state of a tenant that hasn't set one up,
and is never a dead end. Sending a **Draft** PO is refused outright; submit
it for approval first.

### Design and Combination ID photos (Images)

**Images** on a PO's row (every PO with lines except a cancelled one) opens
the PO's photo page: each design on the PO with the combinations ordered
under it, then any line whose item has no design. Add photos under each
one, or drop a folder of files on the box at the top — each file goes to
the design or combination it is **named after** (full number with `/` as
`-` or `_`, or just the part after the last `/` such as `000006-Red-S.jpg`
when only one design on the PO ends that way; a suffix like `-2` or
` back` is allowed; the longest match wins). A file matching nothing is
listed in red with an **Add it to…** picker — never dropped. The first
photo is the **main** one; **Make main** swaps, **Remove** retires a photo
without deleting it. The Design (**Setup → Product Family**) and Item forms
show the same photos in an **Images** section. Step by step: User Guide
§6.11.

Photos are ordinary `ProductMedia` records (`engines/owner_images.go`), so
PIM, channel publish and the printed documents' item picture use them. They
are stored on the client's own **image drive** — an S3-compatible bucket
set in **Configuration → Integrations → Image drive** (Admin Guide §B.3.0) —
or on the ERP server when none is switched on. Uploading needs the right to
create purchase orders (`GET/POST /api/v1/procurement/purchase-order/{id}/images`,
`.../images/main`, `.../images/remove`); the record forms use
`/api/v1/images` with the record's own read / update permission.

### PO amendment

**Amend** (any non-`Closed` PO) loads the PO back into the same composer,
lines included — before Stage 40.1.7 this was a chain of four
`prompt()` dialogs that could not touch the line items at all; it can now.
**Save Amendment** posts back to `/api/v1/doc/PurchaseOrder/{id}`, sending
the version it loaded (`expected_version`) so a concurrent edit by someone
else is caught rather than silently overwritten — the composer's own message
if that happens is *"someone else may have edited this record, refresh and
try again."*

Amending an **Approved** PO shows a confirmation first — *"This PO is
Approved. Amending it will reset it to Pending Approval for re-approval.
Continue?"* — because changing an already-approved commitment has to go back
through the same approval gate it came from.

> [!NOTE]
> **Amending an Approved PO's line items or total prompts for a reason,
> right after the re-approval confirmation.** `validatePurchaseOrderEditRules`
> (`engines/transactional_validation.go`) requires a non-empty
> `amendment_reason` on any edit to an `Approved` PO that changes `items` or
> `total_amount` (`PURCHA-0085`); the composer collects it at that point and
> sends it along with the save. This was a real gap found and fixed
> 2026-09-03 (the composer's `savePurchaseOrder()` previously never
> collected or sent this field at all, so every such amendment failed) - the
> prompt fires whenever the PO was Approved, not only when items/total
> actually changed, since sending the reason on an edit the server doesn't
> require it for is harmless and simpler than replicating the server's own
> before/after diff client-side.

Two other rules bound an amendment regardless of the gap above: a PO that is
**fully received** cannot be amended at all (`PO-0252` — raise an adjustment
or return instead), and if an admin has set a **Purchase Order edit window**
(`procurement.po_edit_window_days`, **Settings → Configuration**, default 0 =
no limit), an edit past that many days from creation is refused with a plain
message naming the PO's age and the configured window.

### 4. Goods Receipt (GRN)

**Procurement → Goods Receipt** (`grn`) is where stock actually moves — a
Purchase Order by itself never changes the stock count, only a posted GRN
does. Since Stage 94 a receipt is worked in **steps**, each on its own page
with one main button and a Back (User Guide §6.10 is the full walk; Admin
Guide §B.3.6.1 the settings, rights and accounting):

1. **Receive.** Pick the **Vendor**; tick their open POs and **Load ticked
   POs** (or add a PO by number, or **Load Items from ASN**). Key what
   arrived in **Received** (a scan of an item already on the receipt counts
   one more), the bill's **Invoice Qty** and **Invoice Rate** where they
   differ, and the vendor's invoice **number, date and total** (all three
   required). **Save receipt** posts nothing.
2. **Verify.** GRN qty (what is really in hand) and GRN price (after the
   vendor call); rejected / damaged with reasons; goods beyond the PO →
   **Keep** (approval) or **Return**. The bar shows the debit or credit
   note the difference will raise. The invoice total must agree with the
   invoice lines within ₹1.
3. **Price check.** Each line against its last purchase price → child GRNs
   **/A New item**, **/B Same price**, **/C Price changed** (renamable,
   lines movable).
4. **MRP and post.** Per child: MRP (current pre-filled, suggested from the
   markup, margin shown) → **Save and post**. Stock goes in at the GRN
   price, the lot carries the MRP. The last child raises the note and makes
   the vendor bill. **Print stickers** per child.
5. **Pay (optional).** Part or full, Bank / UPI / Cheque / Cash, UTR, date,
   TDS. Or leave it in Payables. **Correct** fixes a posted receipt by
   posting only the difference.

When everything matches, **Post all** runs steps 2-4 at once. The list opens
on tabs — **To verify**, **To price**, **Ready to pay**, **Done** — with
counts and the oldest receipt's age.

Cross-checks against the referenced PO, all real and enforced at save time
(on a receipt in steps, `PURCHA-0087` and `PURCHA-0088` are asked at
Verify as Keep / Return instead of refused at Receive):

| Code | When it fires |
|---|---|
| `PURCHA-0082` | The PO is still `Pending Approval` — nothing can be received against an unapproved order. |
| `PURCHA-0086` | Same, but the PO's pending state is itself an *amendment* awaiting re-approval. |
| `PURCHA-0084` | The PO is already fully received — no further GRNs are allowed against it. |
| `PURCHA-0088` | A received SKU isn't on the PO at all (unless a `ReceiptValidationRule` for this vendor explicitly allows unexpected items — see below). |
| `PURCHA-0087` | Received quantity (this GRN plus everything already received) would exceed the ordered quantity plus tolerance. |
| `GOODSR-0089` | Accepted quantity exceeds received quantity on a line. |
| `GOODSR-0090` | A rejected quantity was entered with no rejection reason. |
| `GOODSR-0096` | A damaged quantity was entered with no damage reason. |
| `GOODSR-0097` | Rejected + damaged quantity on a line exceeds what was actually received. |
| `GRN-0253` | Cancelling a GRN that a `VendorInvoice` already references. |
| `GLOBAL-0002` | A batch's expiry date on the receipt line is not after its manufacture date. |
| `GOODSR-0940` | Receipt in steps saved without the vendor invoice number, date or total. |
| `GOODSR-0941` | The same vendor invoice number is already on another receipt. |
| `GOODSR-0942` | Verify: the invoice total differs from the invoice lines by more than ₹1 (the message names the likely line). |
| `GOODSR-0943` | Verify: goods beyond the PO with neither Keep nor Return chosen. |
| `GOODSR-0944` | A step done out of order, on a posted receipt, or on a one-step receipt. |
| `GOODSR-0945` | A line posted without an MRP. |
| `GOODSR-0946` | A child posted while keeping goods beyond the PO still waits for approval. |
| `GOODSR-0947` | The user's role is not listed for that step (Configuration → Procurement). |
| `GOODSR-0950` | Pay now with a wrong amount, mode or reference, or nothing due. |
| `GOODSR-0951` | Post all when the receipt does not match everywhere. |
| `GOODSR-0952` | A negative or invalid quantity or price on a step. |

**Over-receipt tolerance and unexpected items are configurable per vendor**
(or tenant-wide), via a `ReceiptValidationRule` master record
(**Setup → Master Data → Receipt Validation Rule**): an optional **Vendor**
link (blank = tenant default), **Over-Receipt Tolerance %** (default 0), and
**Allow Items Not On The PO** (Yes/No, default No). No rule configured
reproduces exactly today's strict behaviour — 0% tolerance, unexpected items
blocked — so this is opt-in per vendor, not a blanket loosening.

Batch/lot and serial number capture at receipt, FEFO picking, and the five
traceability reports are their own subject — see
[Batch, Serial & Expiry Traceability](traceability-batch-serial.md), which
covers the GRN Workbench's batch/serial rows in full. If an admin has set a
**GRN edit window** (`procurement.grn_edit_window_days`, same Configuration
screen, default 0), editing a GRN past that many days from creation is
refused the same way a PO's edit window is.

## Sending goods back: Purchase Return

**Procurement → Purchase Return** (`purchase-returns`) sends goods back to the
supplier against the goods receipt they arrived on, so the supplier, PO,
location, lot and price are already known.

1. Click **+ New Purchase Return** and choose the **Goods Receipt (GRN)**. Each
   line shows what can still go back: **Accepted** stock (with its unit cost,
   because the supplier owes you for it) and **Rejected** / **Damaged** stock
   that QC set aside at receipt (no cost - the supplier was never owed for it).
2. Enter the **Reason for Return** and a **Return qty** on each line going back,
   no more than **Can return**.
3. **Save Return** saves a Draft. Nothing moves yet.
4. Click **Post** on the list. The stock leaves, and for accepted units a
   **Debit Note** to the supplier is raised and posted, so what you owe drops.
   If the supplier's bill was already entered, the GST claimed on those units
   is taken back in the same step.

If approval is required for returns, the row shows **Submit for approval**
instead, and someone else approves it in **Approvals** before it can be
posted. Returning accepted goods after the **Purchase return window** your
administrator set is refused; rejected and damaged goods can still go back.
Error codes: `PURCHA-0116` (no reason), `PURCHA-0117` (more than can be
returned), `PURCHA-0118` (no valid GRN), `PURCHA-0119` (already returned or
posted), `PURCHA-0120` (Debit Note raised but not posted - post it from
**Financial Accounting → Debit / Credit Notes**).

## After the GRN: vendor invoice and payment

Matching the vendor's actual invoice against the PO and GRN (`Match3Way`,
**Procurement → Vendor Invoice**, `vendor-invoices`) and paying it — with or
without TDS withheld — is the natural next step after receipt, but it's a
large enough topic (3-way match tolerance, `MismatchHold`/Override-and-Pay,
Payment Proposals batching multiple invoices) that it belongs to its own
handbook rather than being folded in here. The tolerance that decides
auto-match vs hold is `procurement.vendor_invoice_tolerance_percent`
(**Settings → Configuration**, default 2%).

## Reports

Reached from **Reports** in the sidebar; all export/schedule like any other
registered report.

| Report | Report id | Answers |
|---|---|---|
| **GRN Register** | `grn-register` | Every GRN — id, PO reference, item line count, status, date. |
| **GRN Register - every line, every step** | `grn-line-register` | One row per GRN line: PO, received, invoice and GRN qty and price, rejected, damaged, old and new MRP, batch, step, dates. |
| **Invoice vs GRN Differences** | `grn-invoice-differences` | Invoiced against received-and-verified by vendor and month, with the debit / credit notes raised. |
| **Purchase Price Changes** | `grn-price-changes` | Items received at a price different from the last purchase: previous price and vendor, new price, %, old and new MRP. |
| **Goods Receipts Waiting** | `grn-waiting` | Receipts not yet verified, priced, posted or paid, oldest first. |
| **GRN Corrections Log** | `grn-corrections` | Every change at every step: was, now, who, when, why. |
| **PO vs GRN Price Variance** | `grn-po-variance` | Received price against the PO price per line, per unit and in value. |
| **GRN Payments** | `grn-payments` | Each posted receipt: invoiced, what we owe, paid, due, last payment. |
| **Goods In Receiving** (Inventory) | `grn-in-receiving` | Arrived but not yet posted - not sellable - by store and SKU. |
| **RFQ Comparison Export** | `rfq-comparison` | Every quote against one RFQ (given its id), quote id/vendor/price/status, for a side-by-side you can export. |
| **Vendor Ledger** | `vendor-ledger` | Every PO for a vendor (or all vendors), PO id/number/total/status/date. |
| **Payables Ageing** | `payables-ageing` | Outstanding PO value by age bucket, with drill-down into the transactions behind each bucket. |

## Error codes reference

| Code | Meaning |
|---|---|
| `RFQ-0251` | The requisition was already converted to an RFQ or PO — it can't convert a second time. |
| `ADMINC-0030` | No numbering series (`prefix_configs`) is configured for the target document type. |
| `PURCHA-0082` | GRN blocked: the PO is Pending Approval. |
| `PURCHA-0083` | Approval decided by someone below the required approver role for this PO's amount. |
| `PURCHA-0084` | GRN blocked: the PO is already fully received. |
| `PURCHA-0085` | Editing an Approved PO's items/total needs a non-empty `amendment_reason` — the composer collects this automatically when amending an Approved PO. |
| `PURCHA-0086` | GRN blocked: the PO's amendment is itself pending re-approval. |
| `PURCHA-0087` | Received quantity would exceed the ordered quantity plus configured tolerance. |
| `PURCHA-0088` | The received SKU isn't part of the referenced PO. |
| `PO-0252` | A fully-received PO cannot be amended. |
| `GOODSR-0089` | Accepted quantity exceeds received quantity on a GRN line. |
| `GOODSR-0090` | Rejected quantity entered with no rejection reason. |
| `GOODSR-0096` | Damaged quantity entered with no damage reason. |
| `GOODSR-0097` | Rejected + damaged quantity exceeds received quantity on a line. |
| `GRN-0253` | GRN cancellation blocked — a Vendor Invoice already references it. |
| `GLOBAL-0002` | A receipt line's expiry date is not after its manufacture date. |
| `MASTER-0049` | Vendor GSTIN isn't a valid 15-character GSTIN. |
| `MASTER-0052` | Vendor bank account number isn't 9-18 digits. |
| `PIM-0950` | Image drive settings incomplete or malformed — the message names the field. |
| `PIM-0951` | The image drive refused or could not be reached — the message gives the reason (bucket, key, secret, permission, region). |
| `PIM-0952` | A photo is on the image drive but the drive's settings were removed. |

## Troubleshooting

**A PO line is red and won't price.** The Item is missing its HSN code or
GST rate on the master. Fix it under **Setup → Items**; the line clears on
the next preview (250ms after your last edit).

**"Could not work the supply type out" banner, amber.** Either the vendor or
the buying Location's Legal Entity has no GSTIN and no State recorded. Fix
the master (better — every future PO gets it right automatically) or tick
**Override** and set Inter-state by hand for this one PO.

**"amendment reason is required to change an Approved purchase order" on
Save Amendment.** This is `PURCHA-0085` — the composer should prompt for a
reason automatically right after the re-approval confirmation when amending
an Approved PO. Seeing this error without that prompt appearing suggests
the confirmation dialog was dismissed or the prompt was cancelled - retry
the amendment and supply a reason when asked.

**Amending or receiving against a PO fails with "pending approval"
(`PURCHA-0082`/`PURCHA-0086`).** The PO (or its in-flight amendment) hasn't
been approved yet — check the **Approvals** screen for who it's routed to.

**A GRN line is rejected as "not part of PO" (`PURCHA-0088`).** Either the
SKU is genuinely wrong, or this vendor legitimately ships items not on the
original order — if the latter, an admin can set **Allow Items Not On The
PO = Yes** on a `ReceiptValidationRule` for that vendor.

**A GRN line is rejected for exceeding the PO quantity (`PURCHA-0087`).**
Received quantity (this GRN plus everything already received against the
same PO) is over the ordered amount plus tolerance. If small over-shipments
are normal for this vendor, set an **Over-Receipt Tolerance %** on a
`ReceiptValidationRule` rather than splitting the receipt to dodge the check.

**Selected a winning RFQ quote, but there's no Purchase Order.** There isn't
one yet, and nothing creates it automatically — see the note under §2
above. Raise the PO by hand on the composer using the winning vendor and
price.

**A Store Manager can't see a "Create Draft" option, or it 403s.** Store
Manager has `update`, not `create`, on `PurchaseOrder` by default. Raise the
initial ask as a Purchase Requisition instead (Store Manager can create
those) and convert it once Approved — conversion has no separate
create-permission gate.

**Vendor save rejected as an invalid GSTIN or bank account
(`MASTER-0049`/`MASTER-0052`).** These are real format checks, not typos in
the message — a GSTIN must be the standard 15-character pattern, a bank
account number 9-18 digits. Both fields auto-uppercase and are format-hinted
as you type.

## What is not here yet

**No automatic RFQ-quote-to-PO conversion.** Selecting a winning vendor
quote closes the RFQ but creates nothing downstream — see the note under §2.
This is a real gap in an otherwise complete round trip, not a missing
button on a workflow nobody uses.

**No silent/QZ printing for a Purchase Order.** Print always opens the
browser's print dialog on the A4 sheet; there is no silent-print job type for
it the way there is for a receipt or shipping label.

**Vendor invoice matching, TDS payment and Payment Proposals** exist and
work (`Match3Way`, `PayVendorInvoiceWithTDS`, the Payment Proposals screen)
but are the natural subject of their own handbook rather than covered here —
see § After the GRN above.
