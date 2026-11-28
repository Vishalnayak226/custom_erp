---
doc_id: DOC-HANDOVER
title: Current developer handover
type: reference
status: active
owner: engineering-owner
approvers: [engineering-owner]
audience: [developers, maintainers]
applies_to: shared development tree; verify state before editing
authority: canonical
confidentiality: internal
last_verified: 2026-11-12
review_by: 2026-12-12
supersedes: historical handover captured 2026-09-10
superseded_by: none
---

# Current developer handover

Start with this page, then use the [live checklist](micro_checklist.md) for open gates and the
[project ledger](project_ledger.md) for dated evidence. Historical entries are context, not a
substitute for checking the current source and worktree.

## Current repository state

- The tree is shared and usually dirty: §6 below names each session's uncommitted work. Keep
  edits you do not recognise.
- The portable Postgres runs on 5490, not the 5435 the `db` package defaults to: always pass
  `TEST_DATABASE_URL` and check the listener before trusting a suite result.
- Build, verification and safety notes for this tree (test database, browser checks, release
  budgets, CSP, alerts, the no-commit rule) are in
  [developer setup](engineering/developer-setup.md#notes-for-this-tree).
- The 2026-10-04 notes that stood here are kept in the
  [archived entries](archive/handovers/ai-handover-2026-10-26.md#repository-state-notes-2026-10-04).

## Durable references

- [Build checklist](product/erp-build-checklist.md) — item scope, dependencies and Done bars.
- [Developer setup](engineering/developer-setup.md) and [service operations](operations/service-operations.md).
- [2026-10-03 prior handover snapshot](archive/handovers/ai-handover-2026-10-03.txt) — the previous
  long-form handoff; follow the live sources above where it is stale.

## 6. Version Control
- **2026-11-28 ask 319 ("build next 20 untouched items to build") - Guide bug hunt on a local copy of HEAD: 13 defects, 319.1-319.13; 11 Done, 319.3 and 319.8 Need decision** (ledger §484). Not committed.
  - **Mine, whole files:** `engines/wms_bin_allocation.go` (`binDocJoin`), `wms_bin_allocation_test.go`, `traceability.go`, `wms_owner_isolation.go`, `wms_owner_stock.go`, `wms_physical_inventory.go`, `wms_putaway_ext.go`, `wms_slotting.go`, `master_data_validation.go` (`validateBinCodeAtStore`), `pim_related.go`, `pim_related_test.go`, `pim_publish.go` (`channelByRefSQL`), `connector.go`, `pim_family_readiness_test.go`, `public/view-pim.js`, `public/shell-nav.js`.
  - **Mine, by hunk (shared files):** `public/app.js` (`currentScreenLink()`, the hashchange "No access" notice, the `\d` in `formatDateTime`; other sessions' hunks are there too); `public/view-admin.js` (`auditDetailText` and the audit row only; LOG_PAGES and the security import are ask 311's); `public/view-reports.js` (the `tasks` links in `stockOnHandHTML` only; the `reportCatalog` hunks are 297.8's); `docs/guides/user-guide-getting-started.md` (the pasted-link sentence; ask 326 has one too); `docs/guides/admin-guide-finance-integrations-and-go-live.md` line 380 (Bin code per store; line 305 is another session's).
  - **Verified:** the three new tests fail on HEAD and pass with their related tests on custom_erp at 5490; `go vet ./engines/`; `node --check`; Guide reruns on the working tree. Nothing left running. Nothing needs `release/hold.txt`.
  - **Next:** the owner answers 319.3 (store check on OMS/WMS action routes) and 319.8 (bin codes per store); recommended answers are in the items.
- **2026-11-28 ask 318 ("All recommended approved, build 297.5 and 297.8") - the report catalog answers 304; `journeylab load` sizes the server for 1 lakh users. 297.8 Done; 297.5 built and run here at 200 users (PASS); its full run is Blocked on the owner** (ledger §482). Not committed. Session erp-59.
  - **Mine, new:** `internal/server/response_etag.go` (+test), `cmd/journeylab/load.go` (+test), `docs/qa/journeylab/load-20261128-042044.json`/`.md`.
  - **Mine, by hunk:** `handlers_report_engine.go` (`writeJSONRevalidated`); `public/shortcuts.js` (`loadReportCatalog`), `view-dashboards.js`, `view-reports.js`; `cmd/journeylab/main.go` (`case "load"`; the `Tenant` line and `client.go` are 297.4's), `journeys.go` (`receiveStockAt`; `openTill` is ask 317's); `docs/operations/journeylab.md` (intro bullet, front matter, § Load; the soak paragraph is ask 317's); admin guide C.2 (297.8 bullet, 297.5 sizing); user guide §9.1; `brain.map.json`; tracker 297.5/297.8/ask 318; ledger §482.
  - **Verified:** 297.8 by its tests, vet and a live scratch server (200, then 304 with 0 bytes; one catalog fetch per tab). 297.5 by 8 tests, vet and 200 users for 10 min: 0 of 6,758 pages failed, reads p95 118 ms, writes p95 684 ms, books tie, 12.15 server CPU ms and 5.08 DB ms a request. Scratch server, cluster and folders removed.
  - **Next:** the owner approves the many-tenant check (not built) and rents two machines for the 10,000-user run (recommendations in 297.5). 297.8 ships with the next release.
- **2026-11-28 ask 324 ("Need a UI where store wh can request in in ui & accept order & proces & dispatch it. or auto trigger" / "...submenu of request reason or can be text like advance order...") - stock requests between locations and a sourcing engine; 324.1-324.4 Done** (ledger §481). Not committed.
  - **Mine, new:** `engines/inventory_stock_requests.go`, `engines/inventory_request_sourcing.go`, `engines/inventory_stock_requests_test.go`, `internal/server/handlers_stock_requests.go`, `db/migrations/migrations_stage324_1_stock_requests.sql`, `public/view-stock-requests.js`.
  - **Mine, by hunk:** `internal/server/routes.go` (7 `/api/v1/stock-requests` routes, `StartStockRequestAutoWorker`), `route_capabilities.go`, `error_catalog_extensions.go` (STOCKT-0494), `public/view-transfers.js` (`transferHostView`), `public/index.html` (menus `menu-stock-requests`, `menu-stock-requests-incoming`), `public/app.js` (`LAZY_VIEW_MODULES`, `MENU_MODULE_MAP` only), `public/shortcut-codes.json` (SRQ, SRF), `public/view-stores.js` (Transfers section), `public/txn-screen.css` (line 125), User Guide §7.8, Admin Guide B.3.9 block, `user-guide-getting-started.md`, `keyboard-shortcuts.md`; 3 files and one role sentence in `docs/brain/brain.map.json` (the region that has transfers); tracker 324.1-324.4 and the ask 324 header; ledger §481.
  - **Verified:** `TestStockRequestSourcingAndFlow` (6 subtests) and the server menu, route and short-code tests on custom_erp at 5490; live in headless Edge, the full ask → accept less → pack → dispatch → reject → receive flow and 390 px; brain map 0 unclaimed. The local DB moved 2 units WH01 → HO. Nothing left running.
  - **Next:** release (G6 regenerates `attack_surface.json` for the new routes). Ask 323's phase 1 (another session) builds on this; 323's brain redraw is no longer blocked.
- **2026-11-28 ask 323 ("Now we have plan throughly the inv planning...") - the inventory planning plan, from the owner's blueprint, Vandeput and FPP; 323.1 Done, 323.2-323.30 open** (ledger §480). Docs only, not committed.
  - **Mine:** `docs/product/inventory-planning-plan-2026-11-28.md` (new); in `docs/micro_checklist.md` the ask 323 header and items 323.1-323.30 (INV › Replenishment & planning, Stock levels, Transfers, Batches/serials, Inventory dashboard; PROC › Vendors, Purchase orders; FIN › Currencies & multi-company; POS › Billing & till); ledger §480.
  - **Not done:** the brain redraw. It stopped on ask 324's unclaimed `engines/inventory_stock_requests.go` and `internal/server/handlers_stock_requests.go`, which belong to another session that is still building. Once that session claims them in `docs/brain/brain.map.json` (beside `engines/supply_proposals.go`), one `pwsh docs/brain/update-brain.ps1` also links the plan doc, which clears its doclint orphan warning.
  - **Next:** the owner answers 323.2 (D1-D13, recommended answers in the item). Phase 0 (323.3-323.9) can start then. Phase 1 waits for ask 324 (another session) to ship, because both change the shared reorder calculation in `engines/planning.go`.
- **2026-11-28 ask 326 ("How to avoid this kind of things? clash" / "Fix it") - a sidebar menu never opens beside a page's drop-down list; 326.1 Done** (ledger §479). Not committed.
  - **Mine:** three hunks in `public/app.js` (`pagePickerOpen()` after `cancelFlyoutHide`, its guard at the top of `showOnHover`, a capture `pointerdown` close beside the document `click` close in `setupModuleFlyouts`); one sentence at the end of the sidebar paragraph in `docs/guides/user-guide-getting-started.md`; tracker 326.1 and the ask 326 header; ledger §479. The other `app.js` hunks (`MENU_PERMISSION_MAP`, `fetchDocTable*`) are another session's.
  - **Verified:** `node --check`; headless Edge 158 against a local server, HEAD vs HEAD plus these lines (menu opened beside an open list before, stays shut after, opens normally once the list closes). 326.1 is still in the live tracker: another session was writing it, so the archive pass waits.
- **2026-11-28 ask 322 ("see if anything helpful to build" / "Build it") - a guard on SQL built from text; 322.1 Done** (ledger §478). Not committed.
  - **Mine:** `internal/securityscan/sql_splice.go` and `sql_splice_test.go` (both new); `docs/security/verification-standards-matrix.md` (front matter dates, rows CWE-89, CWE-862/863, CWE-639), one row in `docs/security/README.md`, one sentence in `docs/guides/admin-guide-developer-and-glossary.md` (line 74); tracker 322.1 and the ask 322 header; ledger §478.
  - **Verified:** gofmt, `go vet ./internal/securityscan/`, the 3 `SQLSplice` tests. `TestEveryEgressPointIsReviewed` and `TestAttackSurfaceManifestIsCurrent` in the same package fail on other sessions' untracked `cmd/journeylab/load.go` and 5 new migrations; the release's G6 regen settles the second.
  - **For the next session:** a new query that writes a value into quoted SQL fails `TestNoUnreviewedSQLSplice`; use `$n`. Only a reviewed constant or closed-list name goes in `reviewedSQLSplices`, with its reason.
- **2026-11-28 ask 320 ("1 click/ meessage it will do all steps" / "buld that") - the bulk PO upload's Send to and Print stickers?; 320.1 and 320.2 Built, not verified** (ledger §477). Not committed.
  - **Mine:** `internal/server/bulk_purchase_send.go` and `bulk_purchase_send_test.go` (both new); hunks in `internal/server/bulk_purchase.go` (bpBillOut TO fields and `posted_now`, totals `sent`, `send_to`, the `sendOn` hook in `doBills`, `bpSendTo` in `bpPrepare`), `engines/transfer_lifecycle.go` (`TransferOpenLines`), `public/view-bulk-purchase.js` (Send to select, `bpOfferStickers`), `public/view-printing.js` (sticker queue); user guide §6.12 and the admin guide's bulk-upload paragraph. Also one line in `docs/brain/brain.map.json`: another session's new `engines/import_lookup.go` (313.2) claimed under Bulk Data I/O, which the redraw needed.
  - **Verified:** vet; `TestBulkPurchase*` incl. `TestBulkPurchaseSendsToStore` against custom_erp on 5490; `node --check`; code graph and brain map redrawn (0 unclaimed). Not seen live. Nothing left running.
  - **Next:** ships with ask 316's release; then minn's Nov 3 file again with Send to BLR HSR (owner's OK first).
- **2026-11-28 ask 321 ("pim has 5 open itm cabe built") - PIM's 5 open items are all built and wait on the owner or ask 316; the one code gap is closed: 321.1 Done, Shopify drops a recommendation list the product no longer has** (ledger §476). Not committed.
  - **Mine, by hunk:** `engines/connector_shopify_sdk.go` (`DeleteProductMetafields`), `engines/shopify_relations.go` (returns an error; `shopifyEmptyRelationLists`), `engines/shopify_sync.go` (`clearRelations`), `engines/connector_shopify_sdk_test.go` (the fake's `metafieldsDelete` case), `engines/shopify_relations_test.go`; one sentence each in `admin-guide-finance-integrations-and-go-live.md` (line 305) and `user-guide-master-data.md` (line 204); in `docs/micro_checklist.md` the ask 321 header, 321.1 and one sentence on 146.5.12; ledger §476.
  - **Verified:** the 11 Shopify tests in engines pass against custom_erp on 5490; vet clean. Generated manuals left to the release's update-docs. Nothing left running.
- **2026-11-28 asks 315 and 316 (minn's Combination ID gap; "Bulk upload is not working in Min properly ... It should accept and auto fix in erp") - the bulk purchase upload now reads bills as a person would; 316.1 and 316.2 are Built, not verified** (ledger §475). Status:
  - **Mine, uncommitted:**
    - `internal/server/bulk_purchase_bills.go` and `bulk_purchase_bills_test.go` (both new);
    - hunks in `bulk_purchase.go` (`doBills` pairing and total, the `note` field, `bpJoin`, the old-run patch in `handleBulkPurchaseRun`) and `bulk_purchase_template.go` (`bpRunFix` in Rows to fix);
    - `public/view-bulk-purchase.js` (a made bill's blank row cell);
    - user guide §6.12 and the bulk-upload paragraph of `admin-guide-items-printing-and-stock.md`;
    - tracker items 315.1 and 316.1-316.4 with the ask 316 header, and ledger §475.
  - **Verified:** the new and existing `TestBulkPurchase*` tests pass against custom_erp on 5490; vet clean.
  - **Done live on minn (2026-11-28):**
    - GRNWH00011/2627 is Posted (Rs 38,580; 52 pcs).
    - TO/WH/2627/00002 moved all 399 pcs (304 lines) from BLR Warehouse to BLR HSR and was received in full. The warehouse is now empty.
    - Guide params: `act-minn-grn11-post.json` and `act-minn-to-hsr.json` in this session's scratchpad.
  - **Next:** release and deploy on the owner's "push and deploy". Then, on minn through app's Control Panel, using the owner's OK given in ask 316:
    - 316.3: re-apply `Downloads/MansoorBusiness/po/MINN_Bulk_Purchase_rest_for_minn.xlsx` (preview first). This receives POWH00008-00018 and fills the 315.1 gap.
    - 316.4: a second TO for that stock to BLR HSR (S00001). Submit and approve it, since direct approval is off (STOCKT-0481).
    - Then close 315.1, 316.3 and 316.4.
- **2026-11-28 asks 313 and 317 (the scaling and recovery plan; "start building all of above") - 313.2, 313.3 and 313.3.1 Done; 297.5 and 297.8 went to erp-59 (ask 318); 313.4 Blocked on its triggers** (ledger §474, §483). Not committed.
  - **Mine:** `docs/product/scaling-and-recovery-plan-2026-11-28.md`, `engines/import_lookup.go`, `import_lookup_test.go`, `import_crash_test.go`, `cmd/journeylab/soak_import_kill.go`, `docs/qa/journeylab/soak-20261128-043756.{json,md}` (all new); hunks in `engines/import.go`, `import_staging.go`, `import_staging_bulk.go` (lookups, the `importCrashAt` seam and its calls, beside 297.2.5's uncommitted hunks), `cmd/journeylab/soak.go` (disturbance 6, `MEDIA_STORE_DIR`), `journeys.go` (`openTill` only; the receiveStock hunks are erp-59's), `journeylab_test.go` (one test); a sentence each in the user guide (getting started), the admin guide (developer) and `docs/operations/journeylab.md`; the tracker items and ledger §474/§483.
  - **Verified:** the new engines tests against custom_erp on 5490; journeylab build, vet and tests; a 6-minute soak, PASS on all six disturbances. Nothing left running; the scratch is removed. erp-59 runs update-brain and update-docs for both sessions.
  - **Next:** 313.2 ships with 297.2.5's `import_staging_bulk.go` change; 313.4 waits for 297.5's rented run; 75.1.1 and 290.2 need the owner.
- **2026-11-27 ask 311 ("approved", the 288.3(c) grouping) - Security Events split into five pages under Settings › Monitoring; 311.1/311.2 Done and archived, 288.3 closed** (ledger §472). Not committed.
  - **Mine whole:** `public/view-security.js` (rewritten: `renderSecurityPane(target, page)`, one draw function per page).
  - **Mine, by hunk:** `public/view-security-notify.js` (exports `renderExport`/`renderNotify`; `renderSecurityExtras` gone), `public/view-admin.js` (`LOG_PAGES` + the `startsWith('security')` dispatch), `public/index.html` (4 `menu-log-security-*` entries), `public/app.js` (4 adminOnly gate lines, `311.1`), `public/shortcut-codes.json` (ACCR, TRCE, BRCH, EMRG); docs: `admin-guide-security-and-operations.md` B.4a, `admin-guide-foundation-users-and-settings.md` (lines 94, 115), `user-guide-getting-started.md` and `kb/getting-started/keyboard-shortcuts.md` (4 shortcut rows; Settings summary), `kb/getting-started/finding-your-way-around.md`, `kb/admin-operations/incident-response.md`, `operations/incident-runbook.md`, `security/cert-in-log-inventory.md`, `security/operating-cadence.md`.
  - **Open:** `pwsh docs/update-docs.ps1` fails at brainmap on another session's unclaimed `engines/list_search_scale.go` (297.4); once that is claimed, rerun it so the generated manuals pick up the guide changes. Scratch server and `%TEMP%\erp311` removed.
- **2026-11-27 ask 312 ("fix it  what is open from above", after ask 305) - a list the user leaves is no longer a Critical 500 (312.1 Done and archived); 290.2 point-in-time recovery still Blocked: the host is ready, but this session was refused access to it, so the owner runs `sudo bash /opt/erp/deploy/install_pitr.sh` in a quiet hour** (ledger §471). The failing parser test from ask 305's report had already been fixed by ask 289 in 0.30.0. NOT COMMITTED.
  - **Mine, by hunk:** `internal/server/apierror.go` (`writeLeftOrTimedOut`, used by `writeEngineError` and by `writeAPIErrorGeneric` for any 5xx), `internal/server/apierror_test.go` (`TestListReadTheUserLeftIsNotAServerFault`, two imports), one `312.1` sentence in `docs/guides/admin-guide-developer-and-glossary.md` (283.3 bullet), the ask 312 header and 290.2 status in `docs/micro_checklist.md`, 312.1 in the archive, ledger §471. `internal/server/handlers_core_doc_engine.go`'s diff is ask 297.4's, not mine. No server or browser left running. Guides and KB regenerated (`update-docs -Group Content`, exit 0). The full `update-docs` and the code-map redraw stop on `engines/list_search_scale.go` (ask 297.4's new, uncommitted file, which no region claims yet); the 297.4 session adds its claim in `docs/brain/brain.map.json`.
- **2026-11-27 ask 310 (minn's stickers fail: "Cannot find printer LD0801 Label Printer") - minn's two Printer records now name the store PC's "DP27 Label Printer (Copy 1)" (live, no release); 310.2 built, uncommitted, ships with the next release** (ledger §470). 310.1 and 310.2 are Built, not verified: the store PC's next sticker print confirms 310.1.
  - **Mine, uncommitted:** `public/print-routing.js` (`installedHere`, `chosenPrinter`), `public/print-agent-flow.js` (one `310.2` line in `agentTryPrint`), `public/print-routing.test.mjs` (new), `docs/brain/brain.map.json` (one claim line; the redraw itself waits on another session's unclaimed `engines/list_search_scale.go`, then `pwsh docs/brain/update-brain.ps1`), `docs/guides/user-guide-labels-and-printing.md` §7B.3, one sentence in `docs/guides/admin-guide-foundation-users-and-settings.md` (another session also has edits in that file).
- **2026-11-27 ask 297 (1 crore rows, slow lines, no hard refresh; "All recommended approved, build all") - 297.1/297.3/297.6/297.7 shipped in 0.30.0, 297.2 in 0.31.0 (8b900f5, live on app and minn). 297.4 (every transaction timed on a 1-crore scratch client, slow reads fixed) and 297.2.5 (1 crore rows imported through two server kills, every total tied; a stale-statistics slowdown in the commit fixed) are Done, uncommitted and not held: they ship with the next release. 297.5 and 297.8 Need decision** (ledger §467).
  - **297.4 / 297.2.5, mine whole (new, untracked):** `engines/list_search_scale.go` and the four `db/migrations/migrations_stage297_4_*.sql` (list order indexes, item identity search, bill search indexes, till session indexes).
  - **297.4 / 297.2.5, mine by hunk (look for `297.4` / `297.2.5` comments; some files hold other sessions' hunks):** `engines/pos_sales_history.go` (+test), `pos_customers.go`, `oms_channel_return_test.go`, `import_staging_bulk.go` (`importAnalyzeMark`, `refreshDocStats`, +test `TestImportCommitRefreshesTableFigures`); `internal/server/handlers_core_doc_engine.go` (its 290.3 hunks are ask 305's), `handlers_core_list_search.go` (+test), `handlers_pos_customers.go`, `error_catalog_extensions.go` (POSOFF-0884; PURCHA-4965 is ask 309's); `public/view-documents.js`, `view-sales-history.js`; `cmd/journeylab/client.go`, `main.go` (X-Tenant-ID). Docs: `admin-guide-security-and-operations.md` (297.4 index bullets), `admin-guide-developer-and-glossary.md` (Large files: the commit's ANALYZE, 1-crore figures), `user-guide-getting-started.md`, `user-guide-selling-and-stock.md`; `brain.map.json` claims `list_search_scale.go` in doc-kernel (brain redrawn, 0 unclaimed; this unblocks the update-docs rerun asks 310/311 wait on).
  - **297.4 / 297.2.5 verified:** vet on engines, server and journeylab, `node --check` on both scripts, the 297.4 and import tests pass (REPORT-0161 already failed on the shared test DB). The 13 GB scratch DB `custom_erp_perf297` (and `custom_erp_s283`) on :5490 are dropped (2026-11-28).
  - **297.2 (released in 0.31.0 by erp-35, hold lines cleared):** its files carry `297.2` comments; the dev DB custom_erp holds Expired `live297` batches and one Receiving upload row the sweep clears after 24 h. 297.5 and 297.8 were built in ask 318 (top of §6).
- Older §6 entries (up to the 0.31.0 release on 2026-11-27) are kept verbatim, newest first, in [archive/handovers/ai-handover-2026-10-26.md](archive/handovers/ai-handover-2026-10-26.md) (moved 2026-11-12, Stage 134.9, 2026-11-14, item 186.1, 2026-11-24, items 264.2 and 265.3, and 2026-11-25, ask 265, and 2026-11-27, ask 289 (twice), and 2026-11-28, ask 327, so this page fits its 150-line budget).
