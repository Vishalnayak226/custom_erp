---
doc_id: DOC-PRODUCT-STAGE85-POS-STORE-BILLING-PLAN
title: Stage 85 plan - POS billing for a real store, store access, store stock locations, customer advance
type: reference
status: approved
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md Stage 85
authority: proposed-design
confidentiality: internal
last_verified: 2026-10-18
review_by: 2026-11-18
supersedes: none
superseded_by: none
---

# Stage 85 plan: POS billing for a real store

**As of 2026-10-18. Approved by the user on 2026-10-18 ("Add all in plan and it should be pipeline/build/deployed").** Status is tracked as **Stage 85** in `docs/micro_checklist.md`.

Inputs: the user's 2026-10-18 POS screenshot and message, plus the earlier requests this release finally builds:
- Stage 77 (2026-10-17): customer capture, high-value rules, tenders, salesmen, store returns, scan without Enter, stock locations, GST bill. Only its decisions (77.0) had been recorded; nothing was built.
- 83.2: the store switcher at the top, waiting on the user's decision.
- 74.2.1: Customer Advance, waiting on DEC-03/04/05/13.
- 76.10: the server-issued POS bill number.

The 840909b release shipped Stage 73's POS work instead (split tender 73.1.6, hold / resume 73.2.5, exchange 73.2.6, till cash 73.2.7). Those are built on here, not rebuilt.

## 1. Decisions

| # | Decision (user, 2026-10-18 unless dated) | What it means for the build |
|---|---|---|
| D1 | **Phone is asked on every bill; one phone = one customer.** A customer who refuses gets **Walk-in** (the store's default customer, 77.0) | The till opens on the phone box. A known number pulls the customer; a new number opens the customer form. The server refuses a second customer with the same phone |
| D2 | **Customer details at the till:** Address Line 1/2, City, State, PIN, DOB, DOA, gender, PAN, GSTIN (77, 2026-10-17) | Name and phone first; the rest in a **More details** section. A GSTIN makes the customer B2B |
| D3 | **Advance = an Advance tab on the till** (Sale / Return / Advance) | 74.2.1 is built with the BRD defaults, held as settings: an advance for goods carries no GST at receipt (Notification 66/2017-CT), an advance for services does (DEC-03); same customer and company only (DEC-04); refund to the original tender, no expiry (DEC-05); stable customer id, phone changes logged (DEC-13). **The CA confirms the tax setting before go-live** |
| D4 | **Store access is set per role first, then per user.** The admin allocates stores in a Stores section on the User form | A role carries All stores / Selected stores / Home store only. A user inherits it or overrides it. "All stores" includes every store added later, automatically |
| D5 | **The working store sits at the top of the screen** (83.2) | The switcher lists only the user's allowed stores. The server re-issues the session for the new store. Switching is refused while a till session is open. The POS store comes from the top, not a form field |
| D6 | **Store stock locations belong to the ERP, not the WMS.** The user controls which are sellable | A Stock Location master (Main, Inward, In-Transit, Damage, QC Hold, Purchase Return) mapped onto today's stock buckets, so no stock data moves. Enabled per store, with defaults for new stores and a default from/to per transaction. A client without WMS has it all; the WMS reads it |
| D7 | **Customers with a GSTIN get a GST tax invoice** with its own print format; B2C keeps the receipt | 77.15 on the Stage 71 print pack, with the server-issued bill number (76.10, at most 16 characters) |
| D8 | **One release for everything** | Built and verified phase by phase, then shipped together |

## 2. Order of work

| Phase | What | Depends on | Touches production data? |
|---|---|---|---|
| 85.A | Store access and the working store at the top | none | Additive columns / tables; existing users keep their store |
| 85.B | Store stock locations, sellable under the user's control | 85.A (store list) | Additive master; no stock row moves |
| 85.C | Customer at the till: phone first, unique, full details, high-value rules | none | Additive fields; existing duplicate phones are listed, never merged automatically |
| 85.D | Bills: server bill number, GST B2B invoice, Customer Advance | 85.C | Additive doctype, series and accounts |
| 85.E | The rest of Stage 77 | 85.A-85.D | Additive |
| 85.F | Guides, tests, live pass, release | all | The deploy |

## 3. Phases

### 85.A Store access and the working store

**Why.** A user has one store (`users.location_code`), signed into the session. An HO admin billing at Jayanagar sees *HO* on the chip while the POS box says Jayanagr. The session line printed the raw id `Location/HQ/2026/000002` because it read the box before its name had loaded.

- **85.A1** Role store access: All stores / Selected stores / Home store only, plus the list of stores. Role screen section.
- **85.A2** User Stores section: Inherit from role / All stores / Selected stores, and the home store. One resolver (`UserAllowedStores`) that every check uses. "All stores" is evaluated live, so a new store needs no edit.
- **85.A3** Working-store switcher in the top bar for users with more than one store; a plain store name otherwise. Switching re-issues the session token on the server after checking 85.A2. It is refused while a POS session is open at the current store.
- **85.A4** The transaction chip and new documents use the working store. Lists and reports open on it, with an All stores choice for multi-store users.
- **85.A5** POS: the store comes from the top bar. The session line and the chip show the store **name**, never an id.

*Done when:* an HO admin switches to Jayanagar and the chip, the till, new documents' numbers and the default list filter all say Jayanagar. A cashier with one store sees no switcher. A store added later appears for every All stores user without an edit.

### 85.B Store stock locations

**Why.** Stage 53 made "sellable" a flag on the whole store. Inside a store, stock sits in fixed bucket columns (`inventory_availability`: available, qc_hold, damaged, blocked, hold_qty, in_transit) with nothing the user can name or control.

- **85.B1** Stock Location master: code, name, the bucket it maps to, **Sellable Yes/No**, an "applies to" (store / warehouse / both) and Active. Seeded with Main (sellable), Inward, In-Transit, Damage, QC Hold and Purchase Return (warehouse only).
- **85.B2** Per-store enablement (Location form section), with defaults applied to every new store.
- **85.B3** Sellable decides availability. `computeATS` (the one choke point) counts the buckets whose stock location is sellable at that store. The default set gives today's numbers exactly.
- **85.B4** Default from/to per transaction: POS and TO dispatch from Main; GRN and TI into Inward; Damage into Damage; Purchase Return from Purchase Return. Transactions added later are listed automatically.
- **85.B5** The till shows *Store · Location* and, per line, sellable vs not, by named location.
- **85.B6** WMS reads it: a bin carries the stock location it belongs to, and the WMS takes "sellable" from the ERP master, not from its own bin conditions.

*Done when:* the user marks a location not sellable and the till, the availability API and the online channels stop offering that stock, with or without the WMS module.

### 85.C Customer at the till

- **85.C1** Customer fields: Address Line 1/2, City, State, PIN, DOA, gender, PAN, GSTIN (country-correct, 76.4), B2B when a GSTIN is set. DOB already exists.
- **85.C2** One phone = one customer: the number is normalised (country code) and a duplicate is refused at the save choke point. Existing duplicates are listed for an admin to merge (`merged_into`); none is changed automatically.
- **85.C3** Phone-first till: the bill opens on the phone box; a known number fills the customer, a new one opens name + **More details**. **Walk-in** uses the store's default customer and placeholder number (77.0), never printed.
- **85.C4** High-value rules as country settings, checked on the server at checkout and asked on the till first. India: address at ₹50,000 or more to an unregistered buyer (CGST Rule 46(e)); PAN above ₹2,00,000 or Form 97 (IT Rules 2026 r.159); cash of ₹2,00,000 or more refused (s.269ST). UK £250, Germany €250, UAE AED 10,000; other countries blank.
- **85.C5** One-line customer status; the items table gets the height (77.2).

### 85.D Bills

- **85.D1** Server-issued POS bill number from the store's POS series at finalise, at most 16 characters. Offline tills draw from a block reserved per terminal. Receipts and reprints show it (76.10).
- **85.D2** GST B2B tax invoice when the customer has a GSTIN: buyer name, address, GSTIN, place of supply (state + code), HSN-wise tax summary, CGST/SGST or IGST, in its own print format. B2C keeps the receipt. Both print the customer name and a real number, never the code or the placeholder (77.15, 77.3).
- **85.D3** Customer Advance (74.2.1): the Advance tab, receipt voucher, the Customer Advance liability account, use on a later bill as a tender, refund to the original tender with a reason, and an Advance Register.

### 85.E The rest of Stage 77

| ID | Item | Note |
|---|---|---|
| 85.E1 | Scan without Enter, everywhere (77.1) | One delegated detector in `app.js` |
| 85.E2 | Tender master per store (77.5) | Turns 73.1.6's fixed modes into a master; tender-wise report and close |
| 85.E3 | Salesman per store (77.6) | Default salesman, Active / Left, ask off / optional / mandatory per store |
| 85.E4 | Store returns (77.8) | Search this store's bills; centralised return = SR at the original store + TO + TI into Inward |
| 85.E5 | Per-tender session close (77.10) | 73.2.7 built the cash figure; this adds the per-tender count |
| 85.E6 | Top bar: total, CGST / SGST / IGST, discount, loyalty (77.12) | On Stage 68's frozen screens |
| 85.E7 | Manual discount, bill and line, % or amount (77.13) | Role-limited, reason above a limit |
| 85.E8 | Bulk line upload from a template (77.14) | `BulkImportCSV`'s line path |

77.7 (hold / resume) was built as 73.2.5 and is closed as a duplicate.

### 85.F Close

- **85.F1** USER_GUIDE §4 (POS) and ADMIN_GUIDE (store access, stock locations, tenders, salesmen, thresholds, advance), walked against the screens.
- **85.F2** Tests for every rule above, the full `go test -p 1`, fresh-database migrate, release budget, attack surface, brain.
- **85.F3** Live browser pass on a scratch server (POS sale, B2B bill, advance take / apply / refund, store switch, sellable flip).
- **85.F4** One release: commit, push and deploy to app and minn, with the user's explicit push.

## 4. Guardrails

- Every schema change is additive. No stock row moves, no customer is merged or changed automatically, and no existing user loses access: they inherit their current store.
- The tax and legal thresholds are settings with country defaults, not code. The CA confirms the advance GST setting before go-live.
- Another session is editing the release tooling (`deploy/`, `cmd/releasever`, `engines/release_versions.go`) and 84.7's `Stores` migration. This stage does not touch those files.
