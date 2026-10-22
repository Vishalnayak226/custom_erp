---
doc_id: DOC-BULK-PURCHASE-UPLOAD-PLAN
title: Bulk Purchase Upload Plan
type: reference
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
audience: [maintainers]
applies_to: Stage 129
authority: transition-copy
confidentiality: internal
last_verified: 2026-10-22
review_by: 2026-11-22
supersedes: none
superseded_by: none
verification_scope: plan only
---

# Bulk Purchase Upload Plan

The client fills one Excel file. One upload in the app creates its purchase orders, and the goods receipts too when the bills are filled in, in one click. First client: minn (imitation jewellery). Tracker: `micro_checklist.md` Stage 129. The configuration it relies on comes from the [Setup Workbook](setup-workbook-plan-2026-10-21.md).

## Decisions

| # | Decision | Why |
|---|---|---|
| D1 | **One file, one row per design × colour × size.** Sheets: Vendors, Purchase, Bills, plus Start Here, Example, Lists and a protected Codes sheet (HSN, states). Layout is the Setup Workbook's: header row, `#` hint row, data. | The user asked for an easy, broad template that covers PO and GRN. |
| D2 | **PO and GRN in one go** (the user, 2026-10-22). Apply creates the POs, submits them, and approves them as the uploader when the approval rules allow it. Rows carrying a vendor invoice no are then received, verified, price-checked and posted, one GRN per bill. | One click from the client's file to stock and stickers. |
| D3 | **Rates include GST** (the user). The POs are saved with GST mode Inclusive, whatever the org default. | The rate is what the vendor's bill shows per piece, and the bill check adds up to the bill total. |
| D4 | **MRP is required** on every row (the user). The GRN posts with that MRP. | Nothing waits at the MRP step. |
| D5 | **Create everything** (the user): new vendors (name + state; GSTIN optional), categories, product types, HSN codes, item names (HSN + GST from the row), attribute values, designs and combinations. **A store must already exist.** | The client should not need anything set up first except their stores. |
| D6 | **HSN is a fixed list** (chapter 71: 7113, 7114, 7116, 7117 and their 6-digit sub-headings), all GST 3%; GST is filled from the HSN, never typed. | GST cannot be entered wrong (the user, 2026-10-22). |
| D7 | **Vendor GSTIN is optional** (the user: "might not know vendor gst"). State is required: it decides CGST + SGST or IGST. A GSTIN that is typed must be 15 characters and match the state. | The client often does not know it. |
| D8 | **Re-uploading is safe.** A PO is found again by vendor + PO Ref (stamped on the PO as `upload_ref`); a found PO is not changed and its rows are skipped. A bill is found again by vendor + invoice no (the GRN's own duplicate check). New rows under a used PO Ref are refused: they need a new PO Ref. | The client sends the same file again when the goods arrive. |
| D9 | **Every write goes through the screen's own handler** with the uploader's identity (`swCaller`, Stage 128): record saves, design attribute, approval submit/decide, GRN steps. | Same checks, numbering, audit and permissions as on screen; no second copy of any rule. |
| D10 | **Preview before Apply; Apply runs in the background**; each run is an ImportJob (`BulkPurchase`) with its file and result, like the Setup Workbook. No schema change. | The user sees the effect before anything is written. |
| D12 | **Design No is the client's own label** (the user, 2026-10-22: *"ERP will make its own design number... 1,1,1 means one design... We have to adopt"*). Optional; the same label groups this file's rows into one design, a blank one is a design of its own, and Wholeops numbers every design. A label never matches across files; only a Wholeops design number (D00010) reorders an existing design. Re-uploading the same file maps an existing PO's rows to its lines in order. | The client's codes are not unique over time, and the ERP owns the design number. |
| D13 | **Photos travel with the file** (the user, 2026-10-22: *"file name & I will give excel & both image file system will adopt"*). A Photo column holds the client's own file names; on upload the user also picks the photos; after Apply the browser sends each through the PO Images upload to its row's Combination ID, and a design's first photo to the design. Same storage as the Images page (image drive / R2, or the server). | The client cannot know Wholeops numbers before the upload, so photos cannot be named after them. |
| D11 | **Where:** Procurement → Purchase Orders list → **Upload from Excel**, its own page (list-first rule). Needs PurchaseOrder create; the GRN part needs the Goods Receipt rights and steps. | Next to the POs it makes. |

## Order of work

| Order | Item | Done when |
|---|---|---|
| 1 | 129.1 Client template | The client can fill it with drop-downs, red empty required boxes and row checks; the Example sheet checks OK. |
| 2 | 129.2 Importer: POs | A filled file previews (new / existing / problem per row) and applies: vendors, masters, designs, POs approved. Re-applying changes nothing. |
| 3 | 129.3 GRN in the same upload | Rows with a bill are received and posted with the sheet's MRP; stock and barcodes exist; a short delivery leaves the PO open. |
| 4 | 129.4 Screen | Download template, choose file, preview, apply with progress, past uploads, in the browser. |
| 5 | 129.5 Docs, tests, checks | Guide sections, Go tests, route capabilities, live check on a scratch server and on app after release. |

## Phases

### 129.1 Client template

- `MINN_Bulk_Purchase_Upload_v1.2.xlsx` (built from `build_template.py`, kept with the stage's working files; the app serves the same file as its template).
- Drop-downs: Vendor (Vendors sheet) and Store and HSN and State are strict; product type, category and attributes accept a new value with a warning. Required boxes turn red on a started row. Column A checks each row: missing values, a design with two product types, two HSNs for one product type + category, received qty without a bill, a bill not on the Bills sheet, MRP below the rate, the bill total against its rows (₹1), GSTIN length and state.

### 129.2 Importer: POs

- `internal/server/bulk_purchase.go`: read the three sheets (`engines.ReadXLSX`, header words like the Setup Workbook), plan, apply in this order: vendors → categories, product types, HSN codes, item names → attribute values → designs and combinations (as the PO screen's Add design does: one value on every row of a design = design attribute, several = combination axis) → POs (Draft, Inclusive, `upload_ref`) → submit → approve.
- Preview groups rows per PO and says per row: new design / existing design, new combination / existing one, and per PO: new / already made (PO number).
- Run records: `engines/setup_workbook.go` run functions take the record kind, so both uploads share them.

### 129.3 GRN in the same upload

- Per bill on the Bills sheet: its rows' POs must be Approved. Receive (`POST /api/v1/doc/GRN`, flow steps, invoice no / date / total, each line's received qty and MRP), Verify, Price check, then post every child with the MRPs. Any step the engine refuses stops that bill with its message; the others go on.
- More received than ordered goes to the existing excess approval; that bill then waits on screen.

### 129.4 Screen

- `public/view-bulk-purchase.js` (lazy, no cold-core bytes): template, upload, preview tables (Vendors, POs, Bills), apply with progress, results with links to the POs and GRNs, past uploads.

### 129.5 Docs, tests, checks

- User Guide section (Procurement) and the PO setup guide pointer; the brain map.
- Tests: parse the template; plan a sample (new vendor, new item name, two designs, two POs, one bill); apply on a scratch DB, re-apply is a no-op; a bill whose total is off is refused by Verify; an unknown store is a problem.
- Route capabilities and the attack surface; live check on a scratch server and in the browser; after release, a sample file on app.

## Not adopted

| Option | Why not |
|---|---|
| The generic CSV import for POs | It writes records directly and skips the PO save path (GST, place of supply, totals, status). |
| One sheet with invoice columns on every row | The invoice date and total would repeat on every row and could disagree; a Bills sheet holds them once. |
| GST typed per row | Every jewellery HSN is 3%; a typed rate can only be wrong. |
| The GRN quick path (Post all) | It refuses first-time items and short deliveries, which bulk uploads mostly are; the steps are run one by one instead. |

## Guardrails

- Never deletes; never changes an existing PO, vendor or item name from the file (an item name's HSN that differs is reported, not changed).
- No schema change, no new dependency, no build step.
- Production uploads are done by the user in the app; Claude prepares the file.
