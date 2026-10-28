---
doc_id: DOC-QA-140-001
title: Guide organisation-cycle run on app.wholeops.in (Stage 140) - process record
type: record
status: draft
owner: qa-owner
approvers: [product-owner, finance-owner, operations-owner]
audience: [business-owners, product, implementation, finance, operations, QA]
applies_to: app.wholeops.in release 0.18.0 (commit 9a008cf), run 2026-10-27/28; local rehearsal on the same commit
authority: historical
confidentiality: internal
last_verified: 2026-10-28
review_by: 2026-11-28
supersedes: none
superseded_by: none
verification_scope: API calls the screens make, run as admin (Super Admin) by Guide on the Guide Test Store; a local rehearsal for each step first; Cashier rights checked locally only; no real customer, courier, marketplace or message provider touched
---

# Guide organisation-cycle run on app.wholeops.in

What was done, in which order, as which user, and what each step proved. The improvements it
found are in `docs/micro_checklist.md`, Stage 140, section **Guide enhancement** (140.12), module
by module.

## 1. How the run was done

- **Who did it.** Guide, the ERP inspector (`~/dev-tools/guide`), signed in on app as `admin`
  (Super Admin). It makes the same API calls the screens make, so the ERP's own permissions,
  validation, numbering and audit trail apply.
- **Where.** Only the **Guide Test Store (GUIDE-TS)**, Guide's own items (INSPECT-ITEM-1) and
  vendor (INSPECT-V1), and records named GUIDE / INSPECT. No real customer, store, employee,
  courier or marketplace was used. app has no WhatsApp / SMS / email sender, so nothing could
  message anyone; the test customer's phone starts with 11 (never a real mobile).
- **Order.** Each cycle was first rehearsed on a local copy of the same release (0.18.0) until
  only real findings were left. Then the list of records it would create on app was shown to the
  user, who approved it ("Yes, all as listed", 2026-10-28). Each live run could write only to the
  API paths that cycle uses.
- **Roles played.** The steps below are labelled with the person who would do them in a real
  business: **BA** (sets the system up), **Cashier / store** (bills the customer), **Customer**
  (waits at the counter), **Ops** (OMS / warehouse), **Merchandiser** (PIM), **CRM manager**,
  **Accounts** (reconciles and closes), **Audit** (checks every report ties).
- **Re-running it.** `pwsh ~/dev-tools/guide/Guide.ps1 -Suite orgcycle` (local) or
  `-Site app -Suite orgcycle -AllowWrites -WriteOnly '<paths>'` after the user's OK. Parts:
  `-Only sales,oms,wave,pim,crm,crmsend,acct`.

## 2. BA: setting the app up

What a business analyst had to create before the cycles could run. Created on app by this run:

| Area | Created | Note |
|---|---|---|
| Warehouse | Zone **GUIDE-ZA** (pick face), rack **GUIDE-R1** (bin BI00001: zone GUIDE-ZA, aisle 01, rack R1), 4 units put away | A bin is refused until its zone exists on the Zone master. |
| Shipping | Courier service area **GUIDE** (all pincodes) | Internal courier: the booking gives the ERP's own AWB number, no courier is called. |
| PIM | Attributes **GUIDE-FABRIC** (Cotton / Silk / Linen) and **GUIDE-SLEEVE** (Half / Full), set on INSPECT-ITEM-1 | |
| Channels | **GUIDE-SHOPIFY**, **GUIDE-AMAZON**, **GUIDE-MYNTRA** (switched off, no credentials), 10 field maps, 3 export templates | Built by hand: the ERP ships no channel templates. |
| CRM | Customer **C00005** "GUIDE Customer" (tag guide140-r83m7c), WhatsApp + SMS consent, segment, WhatsApp and SMS templates, campaign draft **CMP-1793138354296334258-7tam34xu** "do not send" | The campaign was never submitted. |

Already on app from earlier Guide runs: store GUIDE-TS, items INSPECT-ITEM-1 / -RM-1 / -FG-1,
vendor INSPECT-V1, bins INSP-B1 / INSP-B2.

## 3. Customer and cashier: every kind of sale

| Step | Who | What happened on app | Proved |
|---|---|---|---|
| Find the customer by phone | Cashier | 17 ms | found |
| Cash bill BGTS2627000002 (1 x 299) | Cashier / Customer | answered in 184 ms | GL balances, Dr 1100 cash = 299 |
| UPI bill BGTS2627000003 | Cashier / Customer | 153 ms | GL balances, Dr 1102 UPI clearing = 299 |
| Advance CAGTS262700001: 500 cash | Cashier | 61 ms | balance 500, GL balances |
| Bill BGTS2627000004 paid 100 from the advance | Cashier | 151 ms | balance 400 |
| Rest of the advance (400) refunded | Cashier | | balance 0 |
| Credit invoice SI/GTS/2627/00001 (2 x 100 + 5% GST = 210), posted | Accounts | | Dr 1300 210, Cr 4100 200, Cr GST 10. **Stock did not move** (finding 140.12.4). |
| Receipts 100 then 110 | Accounts | | outstanding 110, then 0 / Paid. **Both went to 1100 cash** (140.12.5). |
| Credit invoice SI/GTS/2627/00002 (105) + credit note CN/HO/2627/0001 (52.50) | Accounts | | GL balances, outstanding 52.50 |
| Return of 1 unit from SI/GTS/2627/00001 | Cashier | | **Valued at 0 instead of 105**; stock +1 although the invoice never took it out (140.12.3). |
| Till closed at the counted drawer 598 | Cashier / Store Manager | expected 498, variance 100, sent for approval; approved by admin | **Cash advances are left out of the close's expected cash** (140.12.1); the 100 variance never reached the GL (140.12.11). |

**Billing speed.** Every till call answered in 61-184 ms on app (lookup 17 ms). The customer does
not wait on the system.

## 4. Ops: OMS order to doorstep and back (with WMS)

| Step | What happened on app | Proved |
|---|---|---|
| Website order SO-1793138172765898509-7tam34xu (2 x 299) | reserved 2, no task yet; the same order sent twice stayed one order | waits for Accept |
| Accept (OMS console) | accepted in 82 ms, fulfilment task made | |
| Rack allocation | pick list: 1 from INSP-B1, 1 from INSP-B2 | bins chosen by the system |
| Pick by bin, pack, OMS bill OB/GTS/2627/00001 | bill 598 (taxable 569.52 + IGST 28.48) | after picking, **2 more units show as "waiting for put-away"** (140.12.19) |
| Book, label, manifest, handover | label prints; on hand 14 -> 12, reservation released | |
| ERP side | Sales invoice SINV-1793138232488439946-7tam34xu posted by the handover: Dr 1300 598, Cr 4100 569.52, Cr IGST 28.48 (Pune order from a Karnataka store); cost of goods Dr 5100 / Cr 1200 200 | OMS -> ERP reconciles |
| Sales return through OMS RR-1793138232991447365-7tam34xu | approve, receive, QC Sellable, refund 299 processed; on hand 12 -> 13 | GL balances. The Returns screen's own check said **"No original bill was found"** for this order (140.12.16). The unit waits for put-away (not back in a rack). |

**Wave picking** (local only): a wave-picked order cannot be packed or billed - the task stays
Pending (140.12.15).

## 5. Merchandiser: PIM and marketplace templates

- Attributes Fabric and Sleeve were created and set on the test item.
- Three channel templates were built from them, each exporting a CSV in the marketplace's own
  column names, checked header by header and on the test item's row (Fabric = Cotton):
  - **Shopify**: Handle, Title, Body (HTML), Variant SKU, Variant Price, Metafield: custom.fabric,
    Metafield: custom.sleeve, Image count. Shopify needs the SKU twice; a template refuses one
    field in two columns, so Variant SKU comes through a field map (140.12.23).
  - **Amazon**: item_sku, item_name, product_description, maximum_retail_price, fabric_type,
    sleeve_type.
  - **Myntra**: Vendor SKU Code, Product Display Name, MRP, Fabric, Sleeve Length, Product Details.
- Each feed exported **all 27 items** on app, not just the ones meant for that channel, as CSV
  only (140.12.24). Readiness of the test item: Shopify 72.7, Amazon 70, Myntra 70.
- Nothing was published: the channels are switched off and have no credentials.

## 6. CRM manager: customer patterns, segment, campaign, WhatsApp / SMS

- Management reports all ran in 38-54 ms: CRM customer summary, RFM lifecycle, RFM segmentation,
  customer lifetime value, cohort retention, repeat purchase, inactivity / churn, segmentation,
  campaign ROI, customer 360.
- Segment "tag + agrees to WhatsApp offers": exactly the one test customer; the explain view
  shows why; the members report lists them.
- Templates: SMS ready (DLT id given); WhatsApp not ready until a Meta-approved template name is
  entered.
- Campaign draft: pre-send check says **"No active WhatsApp sender"**. On app there is no WhatsApp,
  SMS or email sender at all (140.12.26). Locally, with a test (mock) sender, the same campaign
  was submitted, approved and sent: matched 1, sent 1, delivered 1.

## 7. Accounts and audit: does everything reconcile?

Each reconciling report was read before and after the run; a report-vs-ledger gap that moves
means the run's own documents do not reconcile.

| Report | Before (report / ledger) | After (report / ledger) | Verdict |
|---|---|---|---|
| Sales register | 299 / 299 | 2,109 / 2,109 | tied |
| AR ageing | 0 / 0 | 650.50 / 650.50 | tied |
| GST document tax detail, GSTR-1 B2B/B2C, GSTR-1 HSN, HSN tax summary | 14.24 / 14.24 | 100.44 / 100.44 | tied |
| Stock valuation summary | 6,852.41 / 7,452.41 | 6,552.41 / 7,152.41 | moved together; the old 600 gap is 132.22 (production cost, fixed, waiting for release) |

The sales register grew by 1,810 = 3 till bills (897) + two credit invoices (210 + 105) + the OMS
invoice (598).

**Every report, after all runs** (Guide `reports` suite on app, read only, 2026-10-28 03:36 IST):
all **421** reports in the catalog ran for Super Admin with no error, the slowest in 330 ms (User
Activity Log, 1,099 rows). The trial balance balances: 63,523.25 on each side over 66 accounts.
22 reports' control totals tie to the GL; the only two that do not are Inventory Valuation and
Stock Valuation Summary, by the known 600 (132.22).

Also checked:
- The customer ledger shows the credit invoices but not the advance or the till bills (140.12.7).
- A Cashier (locally) can post a sales invoice to the GL and take its receipt, and can read the
  whole trial balance through the report although the finance screen refuses them (140.12.9,
  140.12.10).

## 8. Other modules

Run on app with Guide's existing suites (`modules -Only hr`, `flows2 -Only preturn,vpay,wms,quality,service`).

| Module | Who | What happened on app | Proved |
|---|---|---|---|
| HR | HR / Accounts | INSPECT employee, salary structure (15,000 + 6,000 + 2,000), a day's attendance; 1 casual leave applied and approved (12 -> 11); loan LOAN/HO/26-27/00001 of 5,000 disbursed; payroll Payslip/00001 posted; Payslip/00002 with ESI 0.75% | gross 23,000, PF 1,800, PT 200, TDS 2,300, loan EMI 1,000, net 17,700; loan 5,000 -> 4,000; ESI 112.50 to the paise; every payslip's GL balances |
| Procurement | Purchase / Accounts | 5 bought on a PO and GRN; purchase return PRT/HO/26-27/00003 of 2 with debit note DN/GTS/26-27/00003; the GRN's own bill VI-1793138700289195880-7tam34xu (525) matched and paid; a second bill for the same goods held as MismatchHold | return 200, GL balances (5150, 2100, 1200, 1500); payment GL balances (2100, 1500, 1100). The purchase return took the HO number series for a GUIDE-TS GRN (140.12.8). |
| Warehouse | Ops | put-away into INSP-B1, move to INSP-B2; over-put-away refused (INVENT-0971, on purpose) | on hand unchanged by bin moves |
| Quality | QC | failed incoming inspection -> stock held -> NCR NCR-1793138761109951071-7tam34xu investigated, action planned, closed | held +1, sellable -1, on hand unchanged |
| Service | Service desk | ticket SVC-1793138761507554486-7tam34xu assigned -> in progress -> resolved -> closed (resolving a draft refused, on purpose) | |

Not run on app: production (until 132.22 is released each run widens the stock-vs-GL gap by 300),
assets and bank reconciliation (Guide runs those on the local copy only). They passed on the local
copy the same day.

## 9. Already-known items this run touched

- **132.22** finished goods came in at no cost: the 600 stock-vs-GL gap on app (fixed, waiting for
  release).
- **135.19** a second partial completion's ledger line was lost: INSPECT-FG-1 shows 6 in stock,
  4 in its ledger on app (fixed).
- **132.19** prod dates by UTC: the run was at 03:24-03:50 IST, so the purchase return, debit note
  and vendor bill made then are stamped 2026-10-27 22:05 - the previous day in every day book and
  date-range report (decision still open).
- **Stage 106.6** lists follow the working store: admin's own setting on app, so the Sales Invoice,
  OMS Bill and Purchase Return lists showed only HO's records (none); each GUIDE-TS record opened
  by its number. By design - "All stores" in the top bar shows them.
