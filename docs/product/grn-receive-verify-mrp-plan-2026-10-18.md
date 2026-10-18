---
doc_id: DOC-PRODUCT-STAGE94-GRN-RECEIVE-VERIFY-MRP-PLAN
title: Stage 94 plan - Goods receipt in steps (receive, verify, price check, MRP, pay)
type: reference
status: draft
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md Stage 94
authority: proposed-design
confidentiality: internal
last_verified: 2026-10-18
review_by: 2026-11-18
supersedes: none
superseded_by: none
---

# Stage 94 plan: Goods receipt in steps

**As of 2026-10-18. Proposed, waiting on the user's decisions (section 1).** Status is tracked as **Stage 94** in `docs/micro_checklist.md`.

Inputs: the user's 2026-10-18 New goods receipt screenshot and four messages:
1. Vendor first, then the vendor's pending POs. Load the chosen PO and key received qty against PO qty, plus the invoice price and invoice qty. A verification step then sets the GRN price (revised after calling the vendor) and the GRN qty (what is really in hand, more or less). Next comes the MRP screen. Existing items are compared with the previous purchase price, giving a *Same PP* / *Diff PP* group (renamable) as child GRNs. Both go through MRP validation: an existing MRP is pre-filled, otherwise the field is left empty to fill. Saving posts the GRN.
2. *"I need to type total invoice amount and credit note debit note will generate against our inv amt cal."*
3. *"All should be save in table to extract in report and edit is always allowed if user do mistake in any stage."*
4. *"Need to capture payment also as accounting. Not mandatory but process should be laid as std and easy way."*
5. On the PO: a setting for **Always new design number** or **Inherit from existing**. Pick the vendor, then type a design number or the whole product matrix. With *Always new*, a new design is created. Otherwise the design is pulled from the database if it exists, and a new design and its combination ids are created if not.
   - Already built and live as Stage 62.6: **Configuration → Procurement → Design number when a PO adds an item name's design**, *New every time* / *Reuse when it matches*. A typed design number always loads that design, and reuse creates only the combinations a design lacks.
   - The one gap: the match ignores the PO's vendor (D10, 94.13).

What exists today, and is built on rather than rebuilt:
- One-step GRN (`Post Receipt`), which posts at once.
- Multi-PO per vendor, with PO rate and invoice rate per line.
- Automatic debit and credit notes for invoice-vs-PO price (Stage 62, `engines/grn_pricing.go`).
- A batch per GRN line (61.1) and barcode at SKU or at GRN (57.7).
- Fields asked at the GRN (89.4.3), rejected and damaged quantities with a purchase return (57.15 / 73.2.4), and the ASN load.
- Vendor bill with 3-way match, full or part payment, and TDS (`engines/vendor_invoice.go`, `partial_settlement.go`, `tds.go`).
- Stickers.

MRP today is one number per SKU (`Item.mrp`), which POS reads.

## 1. Decisions

| # | Decision | Recommended | Alternative |
|---|---|---|---|
| D1 | **What stock is valued at, and what the notes settle** | Stock at **GRN price** (the verified price). Note = **typed invoice total − our total** (GRN qty × GRN price + GST). Invoice higher → **Debit Note**; lower → **Vendor Credit Note**. The note's lines say where it comes from: price or qty, line by line. PO price stays as a reference, with a PO-vs-GRN variance report. Applies to new GRNs only; old ones keep Stage 62's basis | Keep Stage 62: stock nets to PO price, and the GRN price is ignored |
| D2 | **Where MRP lives** | **On the receipt's batch.** Item MRP shows the latest. POS charges the MRP of the batch scanned. With one barcode per SKU and two MRPs in the store, POS asks *"₹999 or ₹1,099?"*. Old stock is never repriced: it is illegal to sell above the printed MRP | One MRP per SKU, where a new MRP replaces the old one for all stock (old stock needs re-stickering) |
| D3 | **How the lines are grouped after verification** | **Three child GRNs:** *New item* (never bought: MRP empty to fill), *Same price*, *Price changed* (▲/▼ with old price and %). Names are editable and lines can be moved between groups. **Each child posts on its own**, so same-price goods reach the shelf today while changed prices wait for MRP | Two groups, with new items going under *Price changed* |
| D4 | **"Previous purchase price"** | The last posted GRN price of this SKU **from any vendor**, shown with vendor and date. "Same" means equal to the paisa; a tolerance setting is available | Same vendor only |
| D5 | **Typed invoice total disagrees with the invoice lines** | Above ₹1 round-off, Verify asks for the lines to be fixed first, because GST cannot be split on a lump sum. The difference panel shows which line is likely wrong | Raise the unexplained part as one lump note at the main GST rate |
| D6 | **More than the PO, or an item not on any PO** | Accept up to a tolerance (setting, default 0%). Above it, the line asks **Keep** (an approval request through the existing approval engine) or **Return to vendor** | Always refuse excess |
| D7 | **Who does which step** | Each step is a permission. A small shop gives all of them to one person. When everything matches, a **one-click quick path** runs all steps | Fixed roles per step |
| D8 | **Fixing a mistake after posting** | **Correct** on a posted GRN reverses and re-posts **only the difference** (stock, accounts, notes), with every change logged. Stock already sold cannot be taken back: it offers a purchase return. A note in a GST month already filed gets an adjusting note, not an edit | Corrections only through a purchase return / manual note |
| D9 | **Payment** | Optional. Posting **creates the vendor bill** from what was typed at Receive, with no re-keying. **Pay now** on the posted GRN covers amount (part allowed), mode (Cash / Bank / UPI / Cheque), reference / UTR, date, and TDS when the vendor has a TDS section. Unpaid bills stay in Payables and payment proposals | Payment only from Finance → Payables |
| D10 | **PO "Reuse when it matches": whose designs it searches** | **Only the PO vendor's designs** (`ProductFamily.vendor`): the same design name from another vendor is a different article. A typed design number still loads any design | Search all vendors' designs (today's behaviour) |

## 2. Order of work

| Phase | What | Depends on | Touches production data? |
|---|---|---|---|
| 94.1 | Line table, steps, correction log, settings | D1, D3, D7 | Additive tables/columns only |
| 94.2 | Step 1 Receive | 94.1 | No |
| 94.3 | Step 2 Verify, with the invoice-total note preview | 94.2, D1, D5, D6 | No |
| 94.4 | Step 3 Price check and child GRNs | 94.3, D3, D4 | No |
| 94.5 | Step 4 MRP and posting per child; notes; vendor bill; stickers | 94.4, D1, D2, D9 | Posts stock and GL (same as today's GRN) |
| 94.6 | MRP per batch at the POS | 94.5, D2 | No |
| 94.7 | Step 5 Pay vendor (optional) | 94.5, D9 | Posts payment (existing engine) |
| 94.8 | Correct at any step, including after posting | 94.5, D8 | Reverse-and-repost differences |
| 94.9 | Reports | 94.1 | No |
| 94.10 | Quick path and settings | 94.2-94.5, D7 | No |
| 94.11 | Guides, KB, browser walk | all | No |
| 94.12 | Release | all | Old GRNs untouched |
| 94.13 | PO design reuse scoped to the PO's vendor | D10 | No |

## 3. Phases

The screen is one stepper: **① Receive → ② Verify → ③ Price check → ④ MRP → Posted → ⑤ Pay (optional)**. Each step asks one thing, has one main button, and has a Back button. Colours: green means it matches, amber means it differs (allowed), red means it blocks. The GRN list opens first (list-first rule) as an inbox with tabs **To verify · To price · Ready to pay · Done**, each with a count and the age of the oldest. Goods that are not yet posted show as *In receiving* stock, which is not sellable.

### 94.1 Line table, steps, correction log, settings

**Why:** reports must read every step's figures without parsing JSON (message 3), and the previous purchase price needs a history to look up.

**Build:**
- `grn_line`: one row per GRN line, holding every step's numbers. That covers PO, PO qty, received qty, invoice qty, invoice price, GRN qty, GRN price and GST rate. It also covers rejected / damaged with reasons, previous price (with its GRN, vendor and date), group, child GRN, old MRP, new MRP, batch, and who and when for each step.
- The GRN document stays the source. Every save rewrites its rows from one place, the GRN save hook.
- `grn_line_change`: one row per field changed (GRN, line, step, field, old, new, who, when, reason). This complements the existing document audit trail and is the report's source.
- GRN steps: *Received → Verified → Priced (per child) → Posted*, with the parent posted when its last child posts.
- Settings: excess tolerance %, same-price tolerance, blind count (receiver doesn't see PO qty), and who does which step.

**Done when:** a GRN saved at any step has its rows in `grn_line`, and a changed field has a row in `grn_line_change`.

### 94.2 Step 1 Receive

**Your action:** none.

**Build:**
- Pick the **Vendor** first. The vendor's **open POs** show as a tick-list (PO no, date, items, open qty, value). Tick one or more, then **Load**. A typed PO still works.
- Per line: PO qty (open), **Received qty** (typed or scanned, one beep per unit), **Invoice qty** and **Invoice price** (ex-GST). Both invoice fields default to received qty and PO price, so only the lines where the bill differs need touching.
- Header: **Vendor invoice no, date, Invoice total (incl. GST)**, all mandatory. The same vendor + invoice no twice is refused. The invoice photo or PDF is attached using the existing attachments.
- **Save** posts nothing.

**Done when:** a receipt saves as *Received* with no stock or GL movement, and appears under *To verify*.

### 94.3 Step 2 Verify

**Build:**
- Per line: **GRN qty** (what is really in hand; defaults to received) and **GRN price** (after the vendor call; defaults to invoice price). Rejected / damaged qty with reason (existing).
- Each line shows its difference vs invoice and vs PO.
- **Totals bar:** *Invoice total (typed)* | *Our total (GRN qty × GRN price + GST)* | *Difference → "Debit note ₹X will be raised"* or *"Credit note ₹X"*, broken into price differences, qty differences and round-off (D1, D5).
- Excess over PO or an item not on any PO: Keep (approval) or Return (D6).
- GST check: the invoice's GST rate vs the item's HSN rate, and IGST vs CGST + SGST from the vendor's state.

**Done when:** Verify saves, shows the exact note it will raise, and refuses an unexplained difference above round-off.

### 94.4 Step 3 Price check and child GRNs

**Build:**
- Each line's GRN price is compared with the previous purchase price (D4) and sorted into *New item / Same price / Price changed* (D3).
- Each group is a child (`GRN-0012/A`, `/B`, `/C`), named after its group, with an editable name.
- Lines can be moved between children.
- One vendor invoice stays on the parent; children never split the invoice.

**Done when:** the three groups appear with correct membership, can be renamed, and a line can be moved.

### 94.5 Step 4 MRP and posting

**Build:**
- Per child line: cost, **current MRP** (pre-filled when one exists, otherwise empty), **new MRP**, live margin %, and a **suggested MRP** (cost × the category's markup, rounded to a price ending such as ₹x99). *Use suggested* applies it to all.
- Warnings: MRP below cost (red), and MRP below the current MRP (amber).
- **Save & Post** posts that child: stock in at GRN price, batches (61.1) carrying the MRP (D2), barcodes (57.7), GL.
- When the last child posts:
  - The debit or credit note is raised on invoice total vs our total (D1), using Stage 62's note posting with GST per line.
  - The **vendor bill** is created from the Receive data and matched (D9).
- **Print stickers** is offered per child.

**Done when:** each child posts independently with correct stock, cost and MRP. The note equals the shown difference, and the vendor bill equals the invoice total.

### 94.6 MRP per batch at the POS (if D2 = batch)

**Build:**
- POS charges the scanned batch's MRP.
- A per-SKU barcode with two MRPs in stock at that store gets a one-tap MRP choice.
- Returns use the sold batch's MRP.

**Done when:** two receipts of one SKU at two MRPs sell at their own MRPs.

### 94.7 Step 5 Pay vendor (optional)

**Build:**
- **Pay now** on a posted GRN shows the net payable: invoice total − debit note + credit note − already paid.
- Fields: amount (part allowed), mode, reference / UTR, date, and TDS (auto when the vendor carries a section).
- Uses `PayVendorInvoiceAmount` / TDS. A cash payment posts against the store's cash account; bank, UPI and cheque post against the chosen bank account.
- Skipping leaves the bill in Payables.

**Done when:** a part payment and a full payment both post, and the GRN shows *Paid / Part paid / Due*.

### 94.8 Correct at any step

**Build:**
- Before posting, every step stays editable (Back, change, save), and each change is logged (94.1).
- After posting, **Correct** applies D8: it shows the effect first ("stock −2, note +₹180"), then reverses and re-posts only the difference.
- It refuses only where reversal is impossible: stock already sold, a GST month already filed, or a bill already paid beyond the new amount. In each case it names the alternative.

**Done when:** a qty, price or MRP mistake can be fixed at every step and after posting, with the log showing old → new.

### 94.9 Reports (ReportDefinition framework)

- **GRN register:** one row per line, with every step's numbers.
- **Invoice vs GRN differences** by vendor and month, with notes raised.
- **Purchase price changes:** old PP → new PP, %, old MRP → new MRP.
- **GRNs waiting**, by step and age.
- **Corrections log.**
- **PO vs GRN price variance** (buyer view).
- **GRN payments:** paid / due.

### 94.10 Quick path and settings

- **Everything matches** (received = invoice = PO qty, invoice price = PO price, invoice total = lines, all prices *Same*, all MRPs present) shows one button, **Post all**.
- Settings from 94.1 are asked on Configuration → Purchase.

### 94.11 Guides

- USER_GUIDE, ADMIN_GUIDE and the KB procurement handbook, each step as a numbered walk.
- A browser walk of every path: quick path, price changed, new item, excess, correction after posting, and part payment.

### 94.12 Release

- Existing GRNs and their notes are untouched.
- The new flow applies to GRNs created after release.
- The ASN load stays as a source on Step 1.

### 94.13 PO design reuse scoped to the vendor (D10)

**Build:**
- `FindMatchingDesign` also requires the design's vendor to equal the PO's vendor. A design with no vendor still matches, so old designs keep working.
- Check that a matrix row equal to an existing combination reuses that SKU and is not refused as a duplicate.
- Rename the setting's choices to the user's words: *Always new design number* / *Inherit from existing*.

**Done when:** the same design name from two vendors gives two designs, and a repeat order from one vendor reuses its design and SKUs.

## 4. Not adopted

| Idea | Why not |
|---|---|
| Two fully separate GRNs per invoice | One vendor invoice must stay one GRN for GST, the 3-way match and payment. Children under one parent give the same split |
| Changing the MRP of stock already on the shelf | Selling above the printed MRP is illegal. Old stock keeps its MRP (D2) |
| Deleting or silently editing a posted GRN | Audit-trail rules require every change logged. **Correct** (D8) gives the same freedom safely |
| Reading the invoice photo automatically (OCR) | Needs an outside service. Revisit after this stage |

## 5. Guardrails

- Additive schema only: new tables and columns, nothing reworked. Old GRNs keep their data and basis.
- Reuse what exists:
  - list-first screens (`txn-screen.js`)
  - the approval engine (excess)
  - Stage 62 note posting
  - batches and barcode policy
  - stickers
  - vendor bill / part payment / TDS
  - `ReportDefinition`
  - `writeAPIError`
- No number spinners, and no subtitles under screen titles.
