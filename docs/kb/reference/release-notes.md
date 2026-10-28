---
title: Release notes
section: Reference
order: 25
summary: What shipped and when, generated from the build ledger so this page can't say something the ledger doesn't.
audience: everyone
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Release notes
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Release notes

Generated from `docs/project_ledger.md`'s own Stage sections - **198** entries as of this build. Each excerpt is that section's own opening paragraph, not a rewritten summary, so it reads like an engineering build log because that is what it is. For the full detail behind any entry, including what was verified and how, read the ledger itself.

Newest first. Earlier entries continue on:

- [Part 2](release-notes-2.md) - 2026-07-27 to 2026-09-17

## 2026-10-28

**Stage 135.21-135.26 — every posting under its document's lock; provisioning seeds stock locations; PIM publishes to Shopify linked** *(code + tests + docs, NOT COMMITTED - held in release/hold.txt)*

The user: *"Yes, Acknowledged all as recommened"* - build 135.21 and 135.23, route Shopify channels in PIM Publish through the Shopify page (135.22), and the 135.10 / 135.18 answers confirmed. Item detail: micro_checklist 135.21-135.26.

**Stage 140 — Guide runs the whole organisation cycle on app as user, BA, audit, accounts and customer** *(live run + docs; no ERP code changed)*

The user asked Guide to go through every module as each kind of user, on app.wholeops.in, and to end with two documents: the process, and the enhancements filed in the tracker module by module. Guide got a new suite, `orgcycle`, for the cycles its existing suites did not cover: a customer's sales, advances, credit invoices, receipts, credit note and return; a website order through Accept, rack pick, label, handover and an OMS return; Shopify, Amazon and Myntra templates; CRM segment and campaign; and a before-and-after reconciliation of every report that ties to the GL. Each cycle was rehearsed on a local copy of 0.18.0 until only real findings were left. The user then approved the listed writes, and Guide ran `orgcycle`, HR and the procurement / quality / service flows on the test store, and finally all 421 reports.

## 2026-10-27

**The approved follow-ups: one statement per request, a lighter font, eight list columns; 0.18.0 released and deployed** *(code + tests + docs, NOT COMMITTED)*

The user approved the §296 batch and its five open decisions. The batch shipped in 0.18.0 (`9a008cf`), with the parallel sessions' finished work, and was deployed to app and minn after a verified backup. Before the deploy, the data-change scan learned a check it had missed: a background sweep that starts deleting a table it never touched (the retention sweep had gained three) now counts as a data change. Release G9 and Deploy D1 both list it, and the deploy waits for YES. The user answered YES.

**"Build next 20 items": fourteen Stage 47 / 49 slices** *(code + tests + docs, NOT COMMITTED - held in release/hold.txt)*

The user: *"Build next 20 items"*, then *"go ahead"*. Stages 47 / 49 / 50 had no unblocked item left in tracker order, so the run took the six not-started Stage 135 findings and the fourteen Stage 47 / 49 items earlier runs had passed over. Item detail: micro_checklist header of 2026-10-27 and each item's entry.

**Stage 135.10-135.22 — decisions, stock integrity and locking** *(code + tests + docs, NOT COMMITTED - held in release/hold.txt)*

The user: *"Build all. All recommended one is approved"* - the open Stage 135 items, with the recommended answers to 135.10 and 135.18. Item detail: micro_checklist 135.10-135.22.

**Stage 128.5.1 — minn and app configured from their Setup Workbooks** *(live data through Guide + one engine fix, fix released and deployed in 0.18.0 `9a008cf`)*

The user asked for minn's complete setup, then app's, with a record of what was done in an .md file (`Downloads\MansoorBusiness\client-setup-minn-and-app.md`). Item detail: micro_checklist 128.5.1.

## 2026-10-26

**Stage 139 — ERP shortcuts: one Ctrl+K box, short codes, report numbers, standard keys** *(code + migration + docs, NOT COMMITTED - held in release/hold.txt)*

The user asked for ERP shortcuts done the easy, best-practice way, a number for every report, and what else would make transactions easier. The plan (`docs/product/shortcuts-plan-2026-10-26.md`) was approved with "Build all of the above", with the shortcuts written up properly in the user help. Item detail: micro_checklist Stage 139.

**"Build next 20 open items": Stage 49 finished, Stage 50's buildable packages** *(code + tests + docs, released in 0.18.0 `9a008cf` and deployed 2026-10-27)*

The user asked for the next 20 open items. They were taken in tracker order after 49.17.4: the eleven left in Stage 49, then Stage 50's work packages that need no decision, host or real users (BLD-002, 028, 029, 032, 041, 044, 045, 053, 055). Four are fully done (49.18.3-49.18.6); the rest are built, with what is left written on each. Two packages (BLD-041 print pass, BLD-045 load time) ran as background agents. Release 0.17.0 (session erp-18) shipped while this was in progress, with this batch held.

**"Build all" run, Stage 49: the next 20 items** *(code + tests + runbooks, released in 0.15.0 `21e7c99`)*

The user asked for the next 20 open items, to start once release 0.14.0 (`0bcac93`) was pushed. They were taken in tracker order from 49.11.2, leaving out the items that need legal advice, live drills, a decision or real users. Five are fully done (49.11.3, 49.11.8, 49.12.2, 49.12.6, 49.12.7); fifteen are built in part, with what is left written on each entry.

**Stage 138 — No "Switch product" box for a client** *(frontend + `/me/modules`, released in 0.14.0 `0bcac93`)*

The user asked why minn's sidebar showed "Switch product..." and wanted a client to see app's clean layout with only the modules it lacks hidden. Stage 27's switcher appeared for a client with 2+ products but not every full-suite module, and picking one only changed the address bar. Removed the switcher, the server's `owned_packages` and its two helpers; the sidebar's existing module hiding is unchanged. Detail in `micro_checklist.md` 138.1; minn check after the deploy is open.

## 2026-10-25

**"Build all" run, Stages 47 / 49: the next 20 items** *(code + tests, NOT COMMITTED)*

The user asked for the next 20 items. They were taken in tracker order from Stages 47 and 49, each built as a self-contained slice with its own test. The twelve items Stage 134 had already closed were ticked first. Five items are fully done. Fifteen are built in part, with what is left written on each entry.

**Stage 137 — Printer presets and one PrintBridge for every computer** *(code + PrintBridge 0.2.0, NOT COMMITTED)*

The user asked whether printing was ready: stickers came out misaligned, they wanted a preset set once in config that stops the asking, and PrintBridge had to work on any computer, wake when the ERP is used, and come back after a Quit. Decisions: stickers; the printer's roll wins over item template rules; one installer from the ERP; Windows. Built: a Printer preset (roll, shift, darkness, speed) applied on the server (`applyPrinterRoll`) and in the sticker engine, with Test alignment and Calibrate roll; PrintBridge 0.2.0 trusts every `*.wholeops.in` address, asks once about any other, registers a `printbridge:` link that the ERP opens to wake it; the installer is served from `public/downloads` and a release gate keeps its version current. Verified on a fresh scratch DB, the agent's tests and Chromium with a recording stub; the installer and the paper alignment wait on the user's PC. Detail: micro_checklist Stage 137.

## 2026-10-24

**Stage 90.6 — Releases in a few words** *(migration + deploy script, NOT COMMITTED)*

The user found 0.13.0's automatic note too long and the older rows unchanged. The automatic note is now one line (each title's headline, first three, "+N more"); `db/migrations_stage90_6_release_notes.sql` rewrites 0.1.0-0.13.0 to a few words each at the next deploy, so no one runs anything by hand (Claude is blocked from production writes). Notes cell smaller and muted. Detail: micro_checklist 90.6.

**"Build all" run, first slice** *(session erp-f9; paused for a release)*

The user asked to build everything open that needs no decision. The run was recorded at the top of the checklist and stopped after its first four items when the user asked to release what was done. **132.10** fixed the eight bugs Guide found. Two error codes reached users as "Unexpected error occurred"; a new test now fails on any engine or handler code without a catalog row. Soft-deleted masters still counted as Active, which blocked new setup and let deleted credentials, offers and campaigns keep working; 82 queries now exclude them, and restoring a record re-runs its rules. Other fixes: a wrong "barcode" headline on every other duplicate (new MASTER-1321), a crash on an empty integration log, operator-only screens in a client's menu, and New offered on record types the generic save refuses. **135.2-135.4** brought thirteen KB handbooks in line with the screens that exist now (one subagent swept nine of them), put a dated-baseline note on the 2026-10-04 plan, and gave the capability register a label with no version number. **135.3** corrected the publish worker's "stub" comment. The sweep found a real bug (Book Pickup always fails) and two doc gaps, recorded as 135.11-135.13. Details: checklist 132.10, 135.2-135.4.

## 2026-10-23

**Stage 90.5 — The Control Panel's Releases table says what changed** *(deploy script + Control Panel, NOT COMMITTED)*

From the user's Releases screenshot: the note said only "Release.cmd 1356183 from 0.11.0 (d5d7668)". `deploy/deploy.ps1` now builds the note from the commits since the live build (`Get-ReleaseSummary`: Release.cmd's "- " title lines, hand feature/fix subjects, stage numbers dropped, one line each) and sends it base64-encoded, because the old sanitiser stripped every comma, colon and dash; `remote_deploy.sh` decodes it; `Invoke-Deploy.ps1` no longer passes its own note. The column is **What changed**, one line per change. Verified against all ten past release ranges; first live row at the next deploy. Detail: micro_checklist 90.5.

**Stage 134 — The five open decisions taken and built: security groups, sticker bugs, client branding, Railway staging** *(code + docs, NOT COMMITTED)*

**Stage 136 — Transaction tables audit** *(audit + one-line fix)*

The user asked whether every transaction's table is built and working, whether the pending checklist missed a table, and which upcoming items need one. Every migration in the working tree was applied to an empty scratch database, and the result was compared with every table and record type the Go code and screens use. Nothing is missing. The local client schemas match `tenant_default`, and both halves of the open checklist were swept by two subagents. The full suite on copies of that database turned up one real miss: Stage 134.3's `inbound_webhook_receipts` was never classified for the Stage 91 data reset (fixed, 136.2). The storage that the 73.4.x and other open items will need is listed in 136.1. A read-only production check was handed to the user, because the classifier keeps production reads with the user. Detail: **micro_checklist.md Stage 136**.

## 2026-10-22

**Stage 130 — Build all that needs no approval, plus the 2026-10-22 decisions** *(code + docs, NOT COMMITTED)*

**Stage 133 — Sticker printing remembers its setup, reprints from the history, and every print can be previewed** *(code + docs, not released)*

**Stage 101 — CRM made best-in-class: one customer page, consent ledger, segments, a send gate, templates and senders, campaigns, journeys, feedback, tickets, WhatsApp inbox, loyalty depth, clienteling, a business-buyer pipeline** *(code + docs, NOT COMMITTED)*

**Stage 102 — Store app, the rest: push alerts, shared-phone PIN sign-in, Today, receive / count / labels by scanning, basket to the till, order from another store** *(code + docs, NOT COMMITTED)*

**Stage 129 — Bulk Purchase Upload: one Excel file from the client becomes POs and the goods receipts of its bills** *(code + docs, NOT COMMITTED)*

## 2026-10-21

**Stage 128 — Setup Workbook: a whole organisation's setup from one Excel file** *(code + docs, NOT COMMITTED)*

**94.15 money rounding; Stage 127 — every transaction prints, silent print with no browser dialog, print profiles, Reprint by module, number fields, picker search scope** *(code + docs, tab B, NOT COMMITTED)*

**Stage 88 (88.2-88.6) — OMS one page per step, the Pending gate and the two bills, one Templates page, Inventory documents** *(code + docs, NOT COMMITTED)*

**Stage 126 — New purchase order: Enter moves through the entry, a missing state asked and saved, Edit on an added design; Prefix Configurations bug sweep** *(code, NOT COMMITTED)*

**Stage 125.8 — remember this device for the authenticator** *(code, NOT COMMITTED)*

**Stage 125 — one login for every client; Super Admin is the platform operator's alone** *(code, NOT COMMITTED)*

## 2026-10-19

**Stage 124 — a step-by-step guide to set an organisation up for purchase orders** *(docs, NOT COMMITTED)*

**Stage 122 — Configuration → Item fields keeps its column header in view** *(code, NOT COMMITTED)*

**Stage 120 — the Wholeops logo in the app, to the user's logo placement spec** *(code)*

From the user's two logo images (blue vs slate-grey wordmark) and then `ERP_Logo_Placement_Developer_Specification.md`. Full detail in checklist Stage 120.

**Stage 119 — sticker print direction and count, a clean print, and a template kept per product type from the Print screen** *(code)*

From the user's prod Print-dialog screenshot and a photo of the label it printed (the 1-up Seznik tag landed small and turned on the printer's 100 × 150 shipping label, barcode smeared), then *"Make diff template ... horizontal and vertical. 1 count, 3 count"*. Full detail in checklist Stage 119.

**Stages 114 and 115 — a leaner PO entry with prices that flow and are required, PO MRP to the GRN, MRP rules per product type; number series year at the end and a deleted store no longer blocks a save** *(code)*

From the user's New purchase order, sample PO, and Number Series screenshots; decisions asked and answered the same day. Full detail in checklist Stages 114 and 115.

**Stage 117 — Combination IDs numbered in sequence; number series with plain names, grouped** *(code)*

From the user's Prefix Configurations screenshot. Full detail in checklist Stage 117.

**Stage 118 (+114.8) — vendor fixed once picked; names, not codes, in pickers and PO / GRN lines** *(code)*

From the user's Purchase Orders and New purchase order screenshots. Full detail in checklist Stage 118 and 114.8.

**Stages 113 and 116 — sticker template per Product type, every design on the drop-downs, the Seznik tag; frozen headers stop leaking rows** *(code)*

From the user's Template rules, Print and Prefix Configurations screenshots. Full detail in checklist Stages 113 and 116; Stages 114 (PO entry) and 115 (number series) were opened the same day and are not built yet.

**Stages 110-112 — goods receipt follows the PO's GST; plain-words Verify; sticker load, print on scan, Square/Ring cards** *(code)*

From the user's GRN/PO, Verify and Sticker Printing screenshots. Full detail in checklist Stages 110-112.

**Stage 109 — one click to release, one click to deploy, the server cleans itself; SSH bots blocked** *(tooling + server)*

User: *"I want auto mechanism to do this ... a script or bot ... It should learn parallely with you doing in future ... Three action three click. 3rd one if can be done without me also fine."* Checklist Stage 109 (D1-D6).

**Release 0.4.0 to app and minn** *(release)*

**Stage 108 — The New Location form fills itself in; POS adds only an exact barcode** *(code + docs, NOT COMMITTED)*

From the user's New Location and POS / Billing screenshots. New Location: Short Code suggested by one standard rule (JP Nagar → JPN), Facility Code the next number, Level and Sellable follow Type, Parent = the HO, Legal Entity and salesman Mandatory preselected; the server fills a blank Short / Facility Code on create; Level gains HO and Store (migration, shipped list only). POS: only an exact barcode / GTIN rings up directly, anything else lists the containing products to pick, on Enter or a pause in typing. Unit tests + Chromium checks; detail in micro_checklist 108.1-108.2. Open gap: receipt-date barcodes are not found by the POS search (pre-existing).

**Release 0.3.0 to app and minn** *(release)*

**Stage 101 — the CRM blueprint planned onto the ERP** *(docs, plan only, NOT COMMITTED)*

User: *"Indepth CRM Module ... Understand it and plan it and put it in checklist and relevant places"*, with their 70-page *CRM Platform Business and Product Requirements* (38 modules, P0-P2, decision register D01-D12). It became the spec for Stage 101, opened that morning. Each module was checked against what the ERP already has: customers and merge, RFM / lifetime value, the loyalty ledger, offers / vouchers, campaigns with per-channel consent, notification delivery states, service tickets, privacy requests. What was missing became phases 101.1-101.12: customer page, consent ledger, segments, one send gate for every message, templates / senders / campaigns up to a pilot gate, journeys, feedback and a WhatsApp inbox, loyalty depth, clienteling, the business pipeline, reports. Not adopted: its stack, a separate commerce layer, shopper-app SDKs, ads / RCS / AI, and shared-phone profiles (85 D1 stands). Plan: `docs/product/crm-platform-plan-2026-10-19.md`. Nothing built; 101.0.2 waits on the user's decisions.

**"Fix all": the report catalog built - 252 of the user's 290 reports (Stage 75.3.2, 75.3.5-75.3.7)** *(code + docs, NOT COMMITTED)*

User: *"Fix all"* on the build run's leftovers. 75.2.8a (credit-note QR), 76.6 (re-code every master), the BLD020 skip and the manifest shipped in 0.2.0 (§257). The report catalog was unblocked - both spreadsheets were in `Downloads\MyBusiness\IT Solution\ERP\Reports` - and built after the release: `docs/specs/reports/catalog_reports.tsv` + `catalog_columns.tsv` (the spec, no sample data), `cmd/gencatalog` (spec → `engines/report_catalog_spec_generated.go`; `-coverage` → `docs/product/report-catalog-coverage.md`), one runner (`engines/report_catalog.go`, `_common.go`) and eight wiring files. **252 built** under the catalog's own modules with the catalog's exact columns (2,574 of 2,637 filled; the rest are fields the ERP does not record, blank); **38 wait** for modules that do not exist (Stage 74 challan / job work / MI-MR / wastage / damage / receipt note, 88.5.4 stock adjustment, bulk stock upload, e-way bill, leads, opportunities, feedback, recruitment). Control totals on eight of them (sales, stock valuation, four GST, AP / AR ageing tied to 2100 / 1300). Fixed on the way: masked gift card numbers, the Status select silently filtering (also the 75.3.4 builder), code columns renamed by the screen-wide name swap, the Reports menu order. Tests: `TestCatalogReportsRun`, three hand-worked figure tests (Wave A / B / C on throwaway tenants), control totals, wiring and the column-fidelity test. Browser on scratch :8161. Detail: checklist 75.3.2, 75.3.5, 75.3.6, 75.3.7.

**Release 0.2.0 to app and minn** *(release)*

**"Build all", steps 1-4 and the start of 5: Stage 94 goods receipt in steps, small fixes, clean-slate reset, media store, Stage 74 Wave 1** *(code + docs, NOT COMMITTED)*

User: *"build all"* - everything open that is decided and not blocked on the user, leaving out work already in flight in other sessions; order recorded at the top of the checklist. **Stage 94** (D1-D10 approved 2026-10-18): a goods receipt is now saved in steps - Receive (vendor first, open-PO tick-list, received and invoice qty, invoice no / date / total required, duplicate invoice refused, scan-to-count) → Verify (GRN qty and price, rejected / damaged, Keep beyond the PO through a new `GRNExcessApproval` approval record or Return, invoice total must match its lines within ₹1) → Price check (previous purchase price → child GRNs /A New, /B Same, /C Changed) → MRP and post per child (stock at the GRN price, MRP on the batch, stickers per child) → Pay (part, mode, UTR, TDS) with Correct after posting (only the difference posts) and Post all when everything matches. The note is invoice total − what was received and verified: a debit note is applied to the auto-made vendor bill as a settlement line, a credit note posts its GST leg, so paying the bill clears 2100 / 1500 exactly (tested to the paisa). The till sells at the batch MRP on the shelf and asks when there are two (94.6, first-in-first-out from the store's receipts). PO design reuse is limited to the PO's vendor (94.13). New `grn_line` / `grn_line_change` tables and eight reports. Browser-walked on :8194 (price changed + debit note + part payment; correction; Keep held for approval; scan-to-count). **Step 2:** schedule workers claim a due run atomically (73.D.18); production's database time zone checked - UTC, so 73.D.19 needs no code; the 73.D.21 tests all pass, the schedule test no longer touches other rows, CI / promote get a test timeout; Inventory rows link to their stock ledger (73.B.7). **Step 3:** `tenantctl reset-data` (91.1-91.2) - a clean slate in one transaction: suspended client and a 24-hour backup required, every table classified (a test fails on an unclassified one), logins and evidence kept, numbering from 1; not run on app / minn (91.4 is the user's). **Step 4:** `MediaStore` (75.4.1) - product media, thumbnails, transform cache and attachments through one interface, local disk as before. Full `go test -p 1` of `./engines` (497 s) and `./internal/server` green. **Step 5 begun (Stage 74 Wave 1):** the number-series first-allocation race is one atomic upsert in `GenerateSequence` and the POS bill number (74.1.2); attachments turned on for GRN and Purchase Return with a shared panel (74.1.6); reason codes governed - nine new categories with dates, evidence and loss class, 30 starter codes, one check for every caller, a code beside every typed reason (GRN lines, purchase return, price override, reprint), a client setting to make codes mandatory, pickers and the R-TX-12 report; the master moved to Master Data (74.1.1). Paused there at the user's request, about 9% of the run's scope done; the next thread starts with 74.1.3. Details: checklist Stage 94, 73.B/73.D, 91, 75.4, 74.1.

**Stage 104 — The Control Panel's "36 errors": read, explained, fixed** *(code + docs, NOT COMMITTED)*

From the user's Control Panel screenshot (Default ERP Client, Errors (24h) = 36). The rows were read from production by the user (agent prod reads are blocked). None was a server fault: 24 were reports with no rows, the rest catalog messages users were shown. Fixed at the shared choke points: an empty report is no longer logged (104.1); the Control Panel's Errors (24h) and the error-rate alert count faults only, not codes the catalog answers with 2xx/4xx (104.2, re-reads old rows, so prod's 36 read 0 after the release); each logged message names its request (104.3). One real bug behind six rows, still live on 840909b: a PO edit from the PO screen is refused "PO Number (Internal) is required" - fixed by 73.D.8's merge in the committed, undeployed `57d3d04`, now with a PO regression test. Details: checklist Stage 104.

**Stage 105 — Spend fewer tokens per session** *(docs + settings, NOT COMMITTED)*

17 closed checklist sections archived, CLAUDE.md's file sizes corrected (the live tracker is ~220k tokens, not ~43k) with tighter tracker and subagent rules, the memory index halved and the global effort default set to high. Details: checklist Stage 105.

**Stage 98 — Print, PDF, send and export for every transaction; each computer's own printer** *(code + docs, NOT COMMITTED)*

From the user's Send to Vendor screenshot and two messages. Every print window (PO, GRN, transfer, returns, notes, and now the **Sales Invoice**) gained **CSV** and **Excel** line exports built from the print data itself (design, combination, barcode, every attribute under its renamable label, brand, quantities, MRP; cost only on PO / GRN, the selling price on a sales bill), a **PDF** made in the browser from the previewed pages with a hand-written PDF writer (no library), and **Send**: the PDF emailed through the server's SMTP account with the store's email as Reply-To, or downloaded to send yourself; Send to Vendor uses it and still records the PO as sent. With QZ Tray, **Print** goes straight to the computer's own printer for that kind of document, chosen once (by itself when clear) and kept on the server per computer (`PrinterAssignment`); `resolveQZPrinter` now prefers the job's own store, so one store never prints on another's printer. Also Setup ▸ Printing (88.1.14) and lists with their own menu entry left out of Setup (88.1.15). Found and fixed: a package bill's e-invoice read the whole order's lines. Detail, decisions D1-D7 and verification: `micro_checklist.md` Stage 98.

**Stage 97 — The docs folder made simple: one front door, one archive** *(docs + doc tooling)*

At the user's request, `docs/` was reorganised in one pass. 48 files moved: dated audits and evidence from `docs/`, `audits/`, `assurance/` and `qa/` went to `archive/audits/`; superseded specs, plans, the blueprint and the old BRD/PRD went to `archive/legacy/`; old handovers went to `archive/handovers/`; loose checklists, API docs, research notes and the contract template got proper homes. Three sets of small pages were merged (data handbook, *Running the service*, SSDF section). Four approved files were deleted: the duplicate OpenAPI copy, the never-run Stage 48 migration preview (2) and a stale local PDF. A script rewrote 262 links and 73 path mentions. `docs/README.md` is now a plain "I want to…" front door with a folder map, and `docs/archive/readme.md` maps every old path to its new one, replacing redirect stubs. `cmd/gendocs` no longer writes the OpenAPI copy, and its permission snapshots now land under `archive/audits/permissions/`. The top of `docs/` went from 14 files and 23 folders to 6 files and 20 folders. doclint: 0 broken links and 0 unregistered or missing files. Detail and verification: `micro_checklist.md` Stage 97.

## 2026-10-18

**Stage 96 — No Windows Firewall dialog for a dev erp-server** *(code + docs, NOT COMMITTED)*

On Windows, an unset `HOST` now binds `127.0.0.1` (`internal/server/routes.go`), so a new `erp-server.exe` build no longer triggers Windows Firewall's "allow public and private networks?" dialog. Linux (production, Docker) is unchanged, and `HOST=0.0.0.0` opts back in for LAN access. ADMIN_GUIDE §B.1. Detail and verification: `micro_checklist.md` Stage 96.

**Build run, steps 2 and 6-9: numbering, Inventory / WMS, small fixes, e-invoice, reports** *(code + docs, NOT COMMITTED)*

From the user: *"Build 2 to 9 all"* (the module order recorded at the top of `micro_checklist.md`), then *"All approved which are recommended for me"* - recorded on 94.0, 95.0, 88 D12-D14, 73.0.1 and 74.0.1 (BRD defaults; CA sign-off still owed on the tax ones), 34.5 (not built). Full detail per item in `micro_checklist.md`.

**Stage 95 — Images for each Design and Combination ID after the PO, stored on each client's own drive** *(code + docs, NOT COMMITTED)*

From the user: *"We have to introduce image upload for Design and Varinat/Combination ID. Once PO doen, I will upload image"*, then *"You have suggest where I shoud upload that. Once config need to build to connect for erp and the upload drive. Client to client the url will change."* Suggested in chat and decided (asked): each client's own S3-compatible bucket (one connector for R2, Spaces, S3, B2, Wasabi, MinIO; Google Drive advised against - per-client OAuth, quotas, unstable links), uploads from the PO with the same strip on the Design / Item forms, the client's admin sets the drive. Full detail in `micro_checklist.md` Stage 95.

**Stage 93 — A cost center for every location; legal entity and GSTIN required; every person in a department; postings carry store, company and team; old long codes cleaned once** *(code + docs, NOT COMMITTED)*

From the user's Cost Center, Department, Legal Entity and Location screenshots: *"Can it create auto cost center once I create a store ... Not this lengthy name"*, then *"It should ask Mandatory field and auto linked ... If I create diff entity ... How to assign a user to dept? ... If HR, IF someone don't take HR than? ... I want to mapped from day one automatically ... Clean them and ask to set them once."* The three masters and Location were explained in chat first, at the user's request. Decisions (asked): every location type; existing locations linked or given one on release; tagging built in the same pass; department on the User with the Employee leading under HR; postings take the raiser's department; GSTIN mandatory, standard format only; old codes cleaned in a guided step. Full detail in `micro_checklist.md` Stage 93.

**Stage 92 — The submitter may approve their own documents; Send to Vendor without a vendor email** *(code + docs, NOT COMMITTED)*

From the user's Approvals screenshot (an admin refused on their own PO, APPROV-0840) and Send to Vendor screenshot: *"As Admin it should allow to approve all"*, *"Client also create PO and approve. Any one approve. Its ok."* Full detail in `micro_checklist.md` Stage 92.

**Module-wise build, modules 3-5: Finance & Tax, Security & Access, Backups & Platform** *(code + docs, NOT COMMITTED)*

The run the user set ("4,8,1,2,3 ... stop after 3") ends here. Full detail in `micro_checklist.md` 73.D.6-73.D.20, 75.1.4-75.1.8; the clean-slate request is recorded as Stage 91 (parked).

**CI blockers 84.7-84.10 and Stage 90 — version numbers follow each release** *(code + docs, NOT DEPLOYED)*

From the user's "do what needs to be done to fix" (the 840909b release's open items) and their Control Panel screenshot showing `0.1.0` and an empty Releases table. Full detail in `micro_checklist.md` 84.7-84.10 and Stage 90.

**Stage 89 — The item master asked once: org defaults, kind of item, item name pick, where each field is asked, attributes per type, the admin's treatment correction** *(code + docs, NOT COMMITTED)*

From the user's Edit Item screenshots and five follow-up messages; decisions D1-D5 asked (`micro_checklist.md` Stage 89). Full detail there.

**Stage 85 — POS billing for a real store: store access, the working store on top, store stock locations, phone-first customers, GST bill, customer advance, and the rest of Stage 77** *(committed locally - not pushed, not deployed)*

Full detail in `micro_checklist.md` Stage 85; plan `docs/product/pos-store-billing-plan-2026-10-18.md`. Opened from the user's POS screenshot: *"I know i asked and it was built"* - it had been asked (Stage 77, 2026-10-17) but only its decisions were recorded.

**Module 2 (Numbering / Masters), first part — 76.7, 76.8, 76.9** *(code + docs, committed with §241, not pushed; 76.7's country cap waits on Stage 86's commit)*

Full detail in `micro_checklist.md` 76.7-76.9.

**Module-wise build order, module 1: Procurement / GRN / Print — 84.2, 84.3, 84.4, 73.C.7** *(code + docs, committed, not pushed)*

The user set a module-wise build order on 2026-10-18 (`micro_checklist.md`, top: "4,8,1,2,3,5,9,7,6", this run stops after Backups & Platform). This is the first module. Full detail under 84.2-84.5 and 73.C.7.

**Stage 87 — Attributes asked in the PO's add row; a column per attribute on PO lines and the PO print** *(code + docs, NOT COMMITTED)*

From the user's *"It should ask attribute in PO itself. Where is it."* on the New purchase order screen. Full detail in `micro_checklist.md` Stage 87.

**Stage 86 — Refresh keeps your place; number series store code, year formats, GST length and characters, live preview; the ERP's own item attributes** *(code + docs, NOT COMMITTED)*

From the user's Number Series - ASN screenshots, their attribute question, and a refresh that landed on Help. Full detail in `micro_checklist.md` Stage 86.

**Release of §229-237 — Stage 73 batches A-D, Stages 76, 78-82, 71.8** *(one commit, pushed + deployed)*

At the user's *"if all task done, push all and deploy all to minn and app"*. This supersedes the "NOT COMMITTED" tags on §229-237. Full detail in `micro_checklist.md` 73.R and 84.7-84.9.

**Stage 82 — The industry is chosen in Configuration, by the org's admin only** *(code + docs, NOT COMMITTED)*

From the user's screenshot of the top bar's industry dropdown and its typed-code Override prompt. Full detail in `micro_checklist.md` Stage 82.

**Stage 81 — Product attributes per client from the industry attribute library** *(code + docs, NOT COMMITTED)*

From the user's message and draft workbook `ERP_Industry_Product_Attribute_Master.xlsx`. Full detail in `micro_checklist.md` Stage 81.

**Stage 80 — New Item form in sections with a variant-option picker; Help opens the right article on every list; one place for print templates; WMS start-here guide** *(code + docs, NOT COMMITTED)*

Four screenshots from the user on the deployed build. Full detail in `micro_checklist.md` Stage 80.

**Stage 79 — Number boxes take only numbers; PO record form pickers and status; Roles screen rebuilt** *(code + docs, NOT COMMITTED)*

Four messages from the user on the deployed build, all on screens reached from GRN → "create the PO" and Settings → Roles. Full detail in `micro_checklist.md` Stage 79.

## 2026-10-17

**71.8 multi-page prints, PO / GRN rules pinned, Stage 78 day limits** *(code + docs, NOT COMMITTED)*

Three of the user's messages in one session; full detail in `micro_checklist.md` 71.8, the 65.2 re-check note and Stage 78.

**Stage 73 batch C — OMS, platform, shared shell, masters: truthful notifications, exception queues and one order timeline, order changes after release, one task inbox, OAuth2 client credentials** *(code + docs, NOT COMMITTED)*

One of four parallel batches over Stage 73 / 55. Detail and verification notes are on each item in **micro_checklist.md**.

**Stage 73 batch D — Finance, HR, expenses, assets, reports: posting attribution, signed close with controlled reopen, bank workbench, payroll reconciliation, control totals** *(code + docs, NOT COMMITTED)*

One of four parallel batches over Stage 73 (decision D09: corrections by reversal only; reopen through maker-checker). Detail and verification notes are on each item in **micro_checklist.md**.

**Stage 72 — Item & Material Master: independent classification, packaging backflush at the POS, gifts with purchase, gift cards as stored value** *(code + docs; committed `0845470`, deployed 2026-10-17)*

From two PDFs of the user's: *Backflush vs Non-Sellable Items in Retail ERP/POS* (*"Build this also for pos"*) and the 41-page *Enterprise Item & Material Master Module - Build Blueprint* that grew out of it (*"Read and update checklist and build"*). The blueprint's rule - one item-type list cannot answer what an item is, whether stock is kept, whether it is valued, whether the till sells it, how it is consumed and where its cost goes - is now how the item master works: the Stage 60 **Item Type became the reusable profile** with eight independent dimensions (+ expense account) that any Item may override, resolved in one place (`engines/item_policy.go`), checked on every save against the blueprint's compatibility rules and frozen once an item has stock history. At the till (`ResolvePOSQuote`, the one pricing path): packaging and other non-sellable items are refused, promo-only gifts ring at zero only when a new **Gift With Purchase** offer earns them, and a stored-value item cannot be a bill line. **Backflush Rules** consume the store's usual box and butter paper with every sale, inside the sale's transaction, with a **Packaging** override for the cashier, a short-stock policy (default never blocks - the user's decision) and an **Item Exception** queue. Items that keep no stock (services, non-stock expense) and non-valuated stock now behave distinctly at GRN, at the till and on transfers; **virtual kits** issue their components at the POS. **Gift cards** are stored value: registered numbers, funded only when payment and liability posting commit, redeemed as a tender inside checkout, append-only ledger, masked everywhere, block / release / cancel. **Consumption Entry** posts periodic counts and manual issues. Nine section-20 reports. Found and fixed on the way: the POS screen had refused every zero-priced line since Stage 60.4, so no gift or free item could ever be billed. Full detail and verification: `micro_checklist.md` Stage 72; decisions still needed (item release maker-checker, effective-dated versions, posted-bill cancellation, per-store item policy, editor + reason in the audit trail, negative stock per profile, gift returns, gift-card refunds / transfer): 72.D.

**Stage 69 — Create and history on separate pages, everywhere** *(code + docs, NOT COMMITTED)*

From the user's HR Attendance screenshot: *"All txn where there is create and history both will be different page … Make it standard practice how it is in best erp being followed."* Every screen that records something now opens on its **list** (count beside the title) with **+ New …** top right; the form is a page of its own with a breadcrumb back, and saving returns to the list - the Odoo / ERPNext / Zoho shape. One shared mechanism in `public/txn-screen.js` (outside the cold core, `app.js` untouched): each page is a browser history entry on the same address, so Back / Forward move between list and form without a re-render and keep half-typed work, and `history.state` decides which page a re-render shows. Applied to the five Stage 65.8 / 68 screens that had New-first tabs (PO, GRN, Purchase Return, Stock Transfer, Journal Vouchers) and to sixteen more: HR (Attendance, Leave, Payroll, Loans/Advances, My Requests with three New pages), Expenses, Fixed Assets, Manufacturing orders + BOM, ASN, RFQ and its quotes, Order Management's manual order, Marketplace's three sections, Payment Proposals, Yard Board, Sticker Printing's Print tab, and Location Movement - which had no history of moves at all and now lists the completed Move warehouse tasks, each of which now records its batch and reason (`engines/wms_bin_move.go`). Guides, SOP and UAT checklist updated. Full detail and verification: checklist Stage 69.

**Stage 71 — Transaction prints follow the company's standard print-format pack** *(code + docs; committed `0845470`, deployed 2026-10-17)*

From the user's `ERP_Standard_Transaction_Print_Formats.docx`: the Standard layouts of every transaction print now follow the pack. Each has a common header with financial year and status, the pack's columns, reference lines that drop out when blank, totals, three signature lines and an audit line (created / approved / printed by and when). GRN and Transfer In print quantities only. Four documents that existed without a print gained one: the GRN's Vendor Copy (values stripped from the payload), Purchase Return, Credit Note to a customer and Sales Return (`migrations_stage71_1_print_formats.sql` adds them to the template transaction list). Found and fixed: Transfer In printed its received qty as "Sent". Fields the ERP does not hold were left out, and the eleven modules the pack implies but the ERP lacks (delivery challan, customer advance, material issue, inventory adjustment, job work, store credit, e-invoice / e-way bill, document print log, reason codes, PO / GRN fields, print profiles) are listed for the user's decision. Full detail: checklist Stage 71.

**Stage 70 — Sticker templates: variant on the standard label, duplicate it, template rules, template chosen at print** *(code + docs; committed `0845470`, deployed 2026-10-17)*

The built-in standard label now prints the variant's values ("Silver / 7") under the name, and appears as the first card on the Sticker templates gallery with a **Duplicate** that copies it into an editable template. Which template a label uses is now a rule: Item → Item type → category → default → standard (`pickStickerTemplate`), with `Item.sticker_template` / `ItemType.sticker_template` added by `migrations_stage70_1_sticker_template_rules.sql` and a **Template rules** tab to set them. A GRN / Transfer Order print can choose a template per line or for the whole run, for that print only (`StickerLineSelection.TemplateID`). Found and fixed on the way: a soft-deleted template still printed, and two templates on one category or two defaults resolved in scan order. Full detail: checklist Stage 70.

**Stage 68 (with 65.7, 65.8) — Every transaction screen one frozen screen** *(code + docs, NOT COMMITTED)*

From the user's POS / PO screenshots and their Gofrugal RetailEasy reference: POS, Purchase Order, Goods Receipt, Purchase Return, Stock Transfer, Returns, Vendor / Sales Invoice, Debit / Credit Notes and Journal Vouchers now share one layout (`public/txn-screen.css` + `txn-screen.js`, loaded with those screens via a stylesheet entry in `LAZY_VIEW_MODULES`) - a bar with the totals and tax, a user · store chip and the main action; a compact header; the entry row; the items as the only scroller; New / History (Sale / Return) tabs that keep half-typed work. Also: PO **Billing / Shipping address** with the store's address and **Edit store** (fixes the store master and comes back), **Edit** = the full PO page with the same number, a Gmail-style menu toggle, readable **+ Create "<typed>"** in every picker, and the environment banner no longer cutting off the bottom of every screen. Cold core 184,127 / 184,320 for this change alone (net −119 vs HEAD). Full detail: `micro_checklist.md` 65.7, 65.8, Stage 68.

**Stage 67 — Stay signed in for a 12-hour shift; grouped Setup and Settings menus** *(code + docs, NOT COMMITTED)*

Users were being signed out after 30 minutes. That was the idle auto-logout every user was seeded with; the server token itself lasts 24 h. An ERP tab left idle in the background also signed out the tab being worked in, because each tab ran its own timer against one shared token. The timer now reads the last activity from any tab, and scrolling inside tables counts. There is a new 12-hour choice, and it is the default for new users and for a new client's first admin. A one-time migration moves everyone still on the old 30-minute seed to 12 hours; anyone who chose otherwise, and any client whose admin set their own default, is left alone. The Settings flyout is grouped under four headings. The Setup flyout's module headings, built since 30.5.4 but hidden by a Stage 57 selector that named a non-existent id, show again. Screen titles no longer have a descriptive line under them, on any screen; the help docs cover that. A CSS rule stops one coming back (67.4). Detail: `micro_checklist.md` Stage 67.

**Stage 66 — Smart HSN and GST rate suggestions; no number spinners** *(code + docs, NOT COMMITTED)*

The HSN box now suggests codes instead of waiting for a number. A shipped master list (`engines/hsn_master.json`, 290 codes over the ten industries, GST 2.0 rates) is read against the item's name: the material words decide, so silver goes to 7113 and zircon, AD or gold-plated go to 7117; it also tells cloth from fabric from garment. Codes the client's own similar items already carry rank first, and the client's industry breaks ties. Choosing an industry loads its codes into the HSN Code list. The GST rate follows the code: auto-filled for single-rate codes, offered as price-banded choices for garments and footwear, and the client's own list rate always wins. Quick-create now asks Tax Treatment when the rate is 0. Number boxes lost their spinners, and wheel and arrow-key stepping, everywhere. Picker logic is in the lazy `view-pickers.js`, so the cold core shrank by 225 bytes. Detail: `micro_checklist.md` Stage 66.

## 2026-10-16

**Stage 61.6-61.11 — GRN batch numbers with full flexibility** *(code + docs; committed `f017bd2` and deployed 2026-10-17)*

The user asked whether Stage 61 was "ready with expected flexibility"; the honest answer was no, six gaps were listed, and the user said build all of them, the expectation being drag-and-drop label design. Detail in micro_checklist.md Stage 61.

**Stage 64 — every client kept in step with tenant_default; background failures reach the client's error log** *(code + docs)*

Found while preparing the minn.wholeops.in handover: production's `tenant_minn` (provisioned 2026-09-22) was missing 16 tables, 11 columns (`audit_logs.signature`, so every audit write on that client failed), 53 record types with 446 fields, their permissions, 12 number series, all 230 status-change rules, the INR currency and the TDS sections; 10 of its tables drew ids from `tenant_default`'s sequences. Cause: most migrations change only `tenant_default`, the tenant-looping ones only reach clients that already exist, and provisioning clones a hand-kept table list. Fix at the two choke points: `db/tenant_schema_sync.go` (catalog-driven, insert-only) runs after every `-migrate` and inside `ProvisionTenantSchema`. Also: 11 per-client background-worker failure sites now go through `LogBackgroundJobError`, which records each distinct failure in that client's `system_error_logs` once an hour (the user: "all logs should be captured if a data issue is found; I should be able to browse the db and fix it in both instances"). Detail in micro_checklist.md Stage 64.

## 2026-10-15

**Stages 62 + 63 — designs inside the PO, multi-PO GRN at invoice price with automatic notes, designed transaction prints** *(code + docs, NOT COMMITTED)*

The user: create SKUs / design combinations in the PO from the org's attribute matrix (design number new or reused per config); one GRN for several POs of a vendor; PO price and invoice price on every line, the vendor's invoice amount at the top, the difference raised as debit / credit notes — "never on hold"; then, mid-stage, "print templates customisable by the user, drag and drop, mapped product-wise, PO / Vendor PO (images, Excel) / Transfer Out / In / GRN / notes, brand always there, which store I am generating from". Detail in micro_checklist.md Stages 62 and 63.

**Stage 61 — GRN batch numbers and scan-to-age lookup** *(code + docs, NOT COMMITTED)*

The user: a SKU keeps one barcode, so "in each GRN, each Combination ID/SKU will have a batch number" (every SKU), and "after scanning I should know the batch, so I will know ageing or which day it came in". Detail in micro_checklist.md Stage 61.

**Stage 60.8/60.9 — only the platform operator reaches other clients; one table per transaction** *(code + docs, NOT COMMITTED)*

The user: "Only Super admin I can, no one else. Client can't change other" and "I need individual table for all txn ... I should be fixing it". Detail in micro_checklist.md 60.8/60.9.

## 2026-10-14

**Stage 60 — number series per transaction and store, item types, names not codes, platform data console** *(code + docs, NOT COMMITTED)*

The user asked whether PO/GRN/barcode were ready and what was missing, then said "Build" with decisions: lengths 5 (purchase, HR) / 6 (stock, sales, production, assets); the store's Short Code inside numbers; Store Name / Store Code / Short Code / Facility Code with Facility Code for OMS; item types with barcode rules **and** behaviour; a raw-edit console over every table of any client with customer details hidden; no internal codes in the UI except Design, Combination and barcode. Full detail in micro_checklist.md Stage 60.

## 2026-10-03

**Stage 50 — BLD-041 completed: native ESM screen loading meets both cold budgets, preserves entitlement/error/help/print paths, no bundler or npm scripts** *(code + tests + docs)*

**This supersedes the intermediate classic-script implementation and open status recorded in §180.** Expanded the split to 18 native ES modules; `public/app.js` retains the app shell, shared services, navigation and the existing access checks. A route's module is fetched only after `/api/v1/me/modules` has returned and `isMenuModuleVisible` permits it. `loadViewModule()` caches the in-flight import/result to avoid duplicate requests and adds a cache-busting retry URL after a failed import; the existing retry panel handles failure. No bundler, build output, package install or npm script is required. POS, standalone Returns and RF traceability have distinct modules, so first paint on POS does not download the other screens. Help and cross-domain reports/PIM actions use explicit lazy bridges. Shared document-table, formatting, QZ and print-sheet helpers stay in the shell to avoid a dependency waterfall when entering those screens.

## 2026-10-02

**Stage 50 — BLD-041 opened (lazy screen code and initial payload budgets): baseline measured, a classic-script lazy-loading approach decided over an ES-module rewrite, and the mechanism proven end-to-end on 4 real view groups (Manufacturing, Expenses, Fixed Assets, Stock Transfer) — small but real, verified payload reduction; item stays open** *(code + docs)*

First session to touch BLD-041. Measured the real baseline first rather than assuming: three cold-cache HTTP runs against a disposable scratch server (`Accept-Encoding: gzip`) put cold-core (index.html + styles.css + db.js + components/erp-typeahead.js + app.js + qz-print.js) at 347,586 bytes (~339.4 KiB) against the 180 KiB budget, and initial JS alone at 307,602 bytes (~300.4 KiB) against the 120 KiB budget — `app.js` alone is ~96.7% of the JS total and the only lever that moves the needle.

**Stage 50 — BLD-036's gap (2) closed for real (all ~28 remaining "Failed to load" sites individually re-verified, one genuine dead end found and fixed); gap (3) deliberately accepted as residual rather than chased a fourth time** *(code + docs)*

Picked up where the 2026-10-01 thread left off: BLD-036 had three named gaps across three prior sessions, of which gap (1) (double-submit guard coverage) was already closed that same day. Two remained: (2) ~20 of the 54 "Failed to load" sites assessed as action-triggered, non-dead-end fetches but never individually re-verified one by one, and (3) several bespoke section-refresh functions (`loadYardBoard`, `loadWarehouseCockpit`, `loadOMSOrders`, and the same shape elsewhere in WMS/Finance) still blank-then-fill instead of the doctype-table's stale-while-revalidate pattern.

## 2026-10-01

**Stage 50 — BLD-036 continued again (recoverable empty/loading/error/offline states): double-submit guard swept across 18 more bespoke composers app-wide, and a real, unrelated third-instance timezone-skew bug found and honestly flagged (not fixed) while investigating a regression-run failure — item stays open** *(code + docs)*

Picked up the prior thread's handoff verbatim: BLD-036 was left open on three named, narrower gaps (~30 bespoke composers outside the five guarded paths still unguarded; ~20 of 54 dead-end sites unaudited; the bespoke section-refresh screens still blank-then-fill). Confirmed via `git status` that the tree's uncommitted state was unchanged since that handoff. Given (1) is the most mechanical and lowest-risk of the three — a proven extraction pattern applied for a third time, not new design — worked it in full rather than splitting effort across all three.

## 2026-09-30

**Stage 50 — BLD-036 continued (recoverable empty/loading/error/offline states): double-submit guard extended to Approve/Reject and two bespoke composers, 13 more dead-end "Failed to load" screens given a real retry, stable-loading-layout genuinely assessed, and two real bugs caught by live-verifying the fixes themselves — still real progress, item stays open** *(code + docs)*

Picked up the prior same-day thread's handoff verbatim: BLD-036 was left open on three explicitly named gaps (the double-submit guard covering only two of many save paths; only 1 of ~54 "Failed to load" dead-end messages swept; "stable loading layout" not assessed at all). Confirmed via `git status` that the tree's uncommitted state was unchanged since that handoff before starting, and worked all three gaps in the same pass rather than picking one.

**Stage 50 — BLD-036 (recoverable empty/loading/error/offline states): a global API timeout added, a real duplicate-document-creation gap closed on the two highest-traffic save paths, and the Approvals dead-end load failure given a real retry — real progress, item stays open** *(code + docs)*

Picked up as the next-ready item off the prior thread's handoff (BLD-036 — READY after BLD-035's shared-form fixes, which landed 2026-09-29). Confirmed via `git status` that the tree's uncommitted state (BLD-046/BLD-043/BLD-035/documentation-hygiene, `docs/ai_handover.md` §6) was unchanged since that handoff, then started fresh, touching `public/app.js` only.

## 2026-09-29

**Stage 50 — BLD-035 (consistent forms and truthful transaction outcomes): a hardcoded "saved" response replaced with the document's real status app-wide, five silent save/submit/decide call sites wired to show it, missing Date/Currency form fields fixed, and an unrelated Stage-51.1-era edit/delete routing bug found and fixed live** *(code + docs)*

Continuing from the prior thread's priority order (BLD-035 first — DEPENDS on BLD-010/011/012, all done, and had sat READY across several sessions' handoffs without being picked up). Confirmed via `git status` that HEAD was still `afaf419` with the prior session's BLD-046/documentation-hygiene work uncommitted, then started fresh. Checked MC-031/042/082/089 before touching code — they live in `docs/qa/erp-maturity-checklist.md`, not `micro_checklist.md` (confirmed via grep first, per this file's own MC-089 sourcing note) — and found MC-042 ("approval transitions and user messages match economic outcome") already scored FAIL and MC-089 OPEN, so this was a real, acknowledged gap rather than a verify-and-close item.

**Documentation hygiene — split `docs/micro_checklist.md` down to pending-only items, moving every closed item into the archive at item/sub-group granularity for the first time** *(docs only, no code change)*

User asked to clean the live checklist down to pending work. Checked every live stage (20, 26, 31, 34, 38, 39, 47-52) against the file's own rule (a stage header may only be marked closed if 100% of its items are `[x]`) — none qualified, since every stage retains at least one genuinely open item (several from the 2026-09-28 session). The existing archive convention only ever moves a *whole* closed Stage, so at that granularity there was nothing new to move; asked the user how to proceed, and they chose the larger, non-standard option: pull every individual closed `[x]` item out of the still-open stages too, not just whole stages.

## 2026-09-28

**Stage 50 — BLD-046 (bounded imports, reports, exports and jobs): a real cross-worker tenant-scoping bug found and fixed, four bespoke tickers bounded, CSV import capped, one export converted to real streaming — real progress, item stays open on the "interrupted" leg** *(code + tests + docs)*

Continuing the same thread that closed BLD-034 (§172) earlier the same day. Per the priority order the prior session handed off (BLD-046 first — DEPENDS on BLD-043, done; BLD-035 next). Confirmed genuinely unstarted via `git status`/the build checklist's own `[ ]` mark before touching anything.

**Stage 50 — BLD-034 (usable dense tables): server-backed pagination/search/counts replacing a 500-row client-side cap, plus a search field-permission leak and a pre-existing refresh-persistence bug found and closed along the way** *(code + docs)*

Continuing the same thread that closed BLD-043 (§171) earlier the same day. Per the priority order the prior session handed off (BLD-034 first — DEPENDS on BLD-003/043, both done; BLD-046/035 next). Confirmed genuinely unstarted via `git status`/the build checklist's own `[ ]` mark before touching anything.

**Stage 50 — BLD-043 (realistic datasets and query plans): a real unindexed `sort=recent` cost found and fixed via measurement at three scales** *(code + docs)*

Per the priority order handed off from the 2026-09-25/27 threads (BLD-038/037), both of which turned out to already be closed and committed (`8bed150`) by the time this thread started — confirmed via `git status`/the build checklist's own `[x]` marks before touching anything. BLD-043 was the next genuinely-unstarted READY item (no DEPENDS beyond BLD-001, done).

## 2026-09-27

**Stage 50 — BLD-037 (coherent visual hierarchy and support context): environment-banner truncation, a half-empty Home stat-card, a missing correlation reference, and raw doctype identifiers leaking app-wide** *(code + docs)*

Picked up the priority order the 2026-09-25 thread handed off (BLD-037 first — no DEPENDS, explicitly the lightest of the three ready items). Confirmed genuinely unstarted via `git status` before touching anything. Live-verified throughout against the existing disposable Postgres 16.3 on loopback `:5462` (reused, already fully migrated per the prior session's own note) and a fresh scratch server on `:8973`, using real Playwright/Chromium across light/dark/mobile contexts. Full per-item detail `docs/micro_checklist.md` 50.13 and the build checklist's BLD-037 entry; this is the index-style summary.

**Stage 52 — Category-based sticker/label printing module: StickerTemplate designer, GRN/Transfer-Order-driven bulk printing** *(code + tests + docs)*

New feature thread, opened directly from the user's own request for a real label-printing module for a jewellery retailer: different categories need visually different labels, printing should start from a GRN/Transfer Order rather than manual SKU entry (whole document or one line at a time), and the user wants full self-service control over the label layout via a drag-and-drop designer. See `docs/micro_checklist.md` Stage 52 for full per-item detail; this is the index-style summary. Entered Plan mode given the scope (new doctype, a generic cross-doctype transaction loader, a from-scratch drag/resize canvas UI, generalized ZPL rendering) — three parallel Explore passes grounded the design in actual file:line reads (doctype registration is entirely migration-driven; `Item.category` is free text with no master behind it; GRN prints against `accepted_qty`; `Printer.dpi` existed but was dead code) before the plan was written and approved.

## 2026-09-25

**Stage 50 — BLD-038 (accessibility and localization acceptance): closed dialog tab-order/AX-tree leak, missing error announcements, forced-colors focus gaps** *(code + docs)*

Continued the priority order the 2026-09-24 thread handed off (BLD-038 first, then BLD-037 if time allowed). Confirmed genuinely unstarted via `git status` before touching anything. Live-verified throughout against a disposable Postgres 16.3 on loopback `:5462` (already migrated, reused from the prior session per its own handover note) and a scratch server on `:8179`, using Playwright/Chromium plus raw CDP (`Accessibility.getFullAXTree`, `CSS.getMatchedStylesForNode`) for what a keyboard/AT user actually experiences, not just what the CSS/markup claims. Full per-item detail `docs/micro_checklist.md` 50.13; this is the index-style summary.

## 2026-09-24

**Stage 51.9 — Jewellery field-set correction against real client data, and the `minn` PO&Inventory.xlsx master-data migration** *(code + tests + docs)*

User supplied the client's actual business workbook and asked for the ERP's own bulk-import template (not molding the ERP to the client's format), with our own generated Design ID/SKU — see `docs/micro_checklist.md` 51.9 for full detail; this is the index-style summary.

**Stage 50 — BLD-033 (task-oriented role navigation and onboarding): a new Home landing screen, permission-derived not role-name-branched** *(code + tests + docs)*

Built on the priority order the 2026-09-23 thread handed off: BLD-033 first (READY, DEPENDS on BLD-009/021, both done), then BLD-038/BLD-037 next if time allowed. Confirmed genuinely unstarted first (`git status` showed no file evidence). Full per-item detail `docs/micro_checklist.md` 50.13, build checklist BLD-033 entry; this is the index-style summary.

## 2026-09-23

**Stage 50 — BLD-031 (providers, public API and extensions): two real bugs found and fixed via local failure campaigns** *(code + tests + docs)*

Continuation of the same Stage 50 build thread as §160-163. HEAD unchanged at `82f5517`; nothing committed. Per the next-thread prompt's priority order, verified BLD-031 genuinely showed no file evidence in this tree (no outbox/webhook/contract test files, no matching mtime cluster from the 2026-09-20 dispatch) before starting — confirmed not started, built it.

## 2026-09-22

**Stage 50 — BLD-020 (pairwise compatibility and boundary matrix) built and verified, closing Wave 2; a second real timezone-skew bug found and fixed** *(code + tests + docs)*

Same session as §160-162, immediately following BLD-019. BLD-020 formally `DEPENDS on BLD-002`, resolved earlier this session. HEAD unchanged at `82f5517`; nothing committed.

**Stage 50 — BLD-019 (targeted mutation proof) built and verified, three real test gaps found and closed** *(code + tests + docs)*

Same session as §161, immediately following. BLD-019 formally `DEPENDS on BLD-015/017`, both now done, so this was next-ready. HEAD unchanged at `82f5517`; nothing committed.

**Stage 50 — BLD-017 (economic property/metamorphic tests) built and verified, GST rounding bug found and fixed** *(code + tests + docs)*

Continuation of the same session as §160. Two other things closed first, both quick: re-asked and got the BLD-002 DECISION half from the product/QA owner (both reference candidates approved as drafted; Chromium/3-viewport/en-IN accepted as the release bar; a tenant-size dimension approved and defined as three tiers reusing the existing `tenant_limits`/`CheckTenantLimit` mechanism keyed on `max_users`; module/role subsets proposed per candidate with a stated gap — see `bld-002-test-configuration-inventory.md` §9 and the build checklist's BLD-002 entry; unblocks BLD-020). HEAD unchanged at `82f5517`; nothing committed.

## 2026-09-18

**Stage 50 — POSCart/override decision implemented, BLD-010/011 live-verified, two new focus/keyboard gaps found and fixed** *(code + tests)*

Continued §157's build thread: the two items it left open (§157's own "one new finding" and BLD-010/011's live verification). HEAD unchanged at `82f5517`; nothing committed. All verification against a fresh, disposable PostgreSQL 16.3 cluster on loopback port 5461 (deliberately new rather than reusing the 2026-09-17 session's still-running :5460 cluster, whose bootstrap role name could not be determined) plus a scratch `erp-server` on port 8462, never shared dev/production.

## 2026-09-17

**Stage 50 BLD-001–014 (Wave 0/1) built and verified, plus AUD-03/04 fixed and one new finding** *(code + tests)*

Executed the build checklist's next-thread prompt from §156: BLD-001's isolated environment, then the full BLD-003–014 release-blocker batch. HEAD unchanged at `82f5517`; nothing committed. All work verified against a disposable PostgreSQL 16.3 cluster on loopback port 5460 (four independently fresh-migrated databases across the session), never the shared dev DB or the frozen 2026-09-16 audit fixture on 5446/8178.

**Stage 50 build checklist and new-thread handoff** *(planning only)*

Prepared the detailed build and acceptance checklist at the user's request: 60 work packages in ten dependency-ordered waves, 23 business reference journeys and eight explicit candidate-domain decisions. Each work package identifies responsible roles, readiness/prerequisites, implementation scope and observable closure criteria. All 136 audit controls, nine defects, two verification findings, twelve package proposals, eight performance proposals and twelve Stage 48 parent gates are mapped. The live TODO now links the detailed queue through 50.11–50.19 and records only checklist preparation as complete (50.6b).

