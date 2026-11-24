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
- **2026-11-25 ask 266 — current ERP improvement plan ready (266.1 Done, ledger §431); planning only, no commit/deploy.** New `docs/product/erp-improvement-plan-2026-11-25.md`: source-confirmed Returns first-200/filter-state gaps and typed exchange-code friction; existing 264.4 FX settlement decision; eight proposed packages and coverage of all 25 modules. Artifact budget check passes: cold core 182,660/184,320, initial JS 113,817/122,880 bytes. HEAD at review start `7d59685` (0.29.0); deployed versions were not checked. No app/database/server changes or new live acceptance; preserve the existing dirty files. Implementation awaits the user's next instruction; do not treat the proposals as built or duplicate existing acceptance owners.
- **2026-11-24 PrintBridge 0.3.3 refuses a print to a printer that is off or unplugged (ask 249, ledger §430): 249.1 Built, not verified. NOT COMMITTED, no release.**
  - **PrintBridge repo (not committed):** `agent/internal/print/ready_windows.go` + test (new), `printer.go` (`ReadyChecker`), `winspool_windows.go` (status consts at package level), `agent/internal/agent/handlers.go` (`BuildVersion` 0.3.3, check in `print`), `print_ready_test.go` (new), `docs/PROTOCOL.md`.
  - **ERP:** `public/downloads/printbridge-*` (0.3.3, signed), **listed in `release/hold.txt`** until a plugged-in USB printer prints (LD0801 round, 251.3); then remove those lines. Guides: silent-printing-setup, user guide §7B.1, admin guide items-printing.
  - Agent 0.3.3 is installed on this PC. The scratch server on :8190 is an older build (old `/api/v1/print/qz/*` routes); the check mapped the new route names to it.
- **2026-11-24 Fix from photo reads a Line up, the alignment test or the label's own print (ask 257, ledger §429): 257.1 Done. NOT COMMITTED, no release.**
  - **Files (new, untracked):** `public/print-align.js` (`readTags`: tags from die-cut edges, template match per tag, one print per photo; scoring by running sums, about 2x faster), `public/print-check.js`, `public/print-align.test.mjs` (14/14, `node --test`).
  - **Files (changed):** `public/sticker-engine.js` (stage by hunk; other sessions' hunks sit at about lines 1384-1776), `user-guide-labels-and-printing.md`, `admin-guide-items-printing-and-stock.md`.
  - Verified live on a scratch server (:8190) with the owner's six photos: shifts 1.87-2.39 mm, a desk photo reads *Print not known*. The scratch server is stopped when the session ends.
- **2026-11-24 KB review and handover trim (ask 264, ledger §428): 264.1-264.2 Done, 264.3 runs at the next release, 264.4 waits on the owner. NOT COMMITTED, no release.**
  - **Files:** 12 pages under `docs/kb/` (getting-started, reference, role-journeys, troubleshooting) plus `internal/kb/content` regenerated; `docs/ai_handover.md` (473 -> 139 lines; released §6 moved to `docs/archive/handovers/ai-handover-2026-10-26.md`); `docs/engineering/developer-setup.md`; `cmd/doclint/main.go` (message only); `release/scripts/Invoke-Release.ps1` and `release/README.md` (G7).
  - **G7 now fails on any doclint warning** except the register's own two kinds. Keep this page under 150 lines: move released §6 entries to the dated archive file.
  - **doclint today:** only the two register findings from ask 263's rename; G7 rewrites the register.
- **2026-11-24 No "qz" names left in the code (ask 263, ledger §427): 263.1-263.5 Done, verified live on a scratch server (Guide stickers, print143, giftslip; both route sets probed). NOT COMMITTED, no release.**
  - **Renamed (new files untracked, old ones deleted):** `engines/qz_print.go`, `qz_payload.go`, `qz_print_test.go`, `qz_po_print_test.go`, `qz_receipt_invoice_test.go` -> `engines/print_agent*.go`; `internal/server/handlers_qz_print.go` -> `handlers_print_agent.go`; `public/qz-print.js`, `qz-print-flow.js` -> `print-agent.js`, `print-agent-flow.js`; `cmd/qzcert` -> `cmd/printcert`; `docs/guides/qz-printing-setup.md` -> `silent-printing-setup.md`. Ask 260's remarks hunks in the payload and receipt test moved with them.
  - **New:** `db/migrations/migrations_stage263_4_os_printer_name.sql` (applied to local `custom_erp` by hand, not recorded; the runner records it at release), `engines/data_healer_printer_name.go` + test (heal `printer-name`).
  - **Changed, stage by hunk (other asks share most of these):** routes.go, route_capabilities.go, middleware.go, middleware_compress.go, printer_choice*.go, print_history.go, print_issue.go, print_profiles_rest.go, stickers.go, code128.go, data_healer.go, a few engine tests and comments; public/app.js, index.html, print-routing.js, sticker-engine.js, trade-docs.js, view-printing.js, view-oms.js, view-pos-extras.js, view-reports.js; deploy/erp.service, erp.env.example; release/README.md and Invoke-Release.ps1 (G4 now checks `print-agent.js?v=` only when index.html names it); cmd/releasebudget; guides, docs/README, archive readme, brain map.
  - **Old names that still work:** `/api/v1/print/qz/*` route aliases (with capability entries), the `QZ_PRIVATE_KEY_PATH` / `QZ_CERTIFICATE_PATH` settings, a `qz_private_key.pem` made before 263, channel `qz`, `qz_printer_name` in requests and in printer JSON, and the old `print_job_log.qz_printer_name` column. QZ protocol strings, shipped migrations and release notes keep "qz".
  - **Each client runs Settings > Data Healer > Printer names once** after the release. Until then printing reads either key and the Printer form waits.
  - **Rollback:** an old binary reads only `qz_printer_name`, so after a client has run the heal, roll forward, or copy the key back with one UPDATE on `documents` (doctype Printer). New print-log rows write only `os_printer_name`.
  - **Left for the release:** G6 regenerates `docs/security/attack_surface.json` for the `/print/agent/*` routes. Seven other sessions' local migrations are still pending (205.4, 205.15, 213.2, 213.5, 215.7, 248.2, 251.2); `-migrate` was not run here.
  - **Tracker:** another session rewrote `micro_checklist.md` at 08:53 on 2026-11-24 from an older copy, which removed the ask 263 items and the 258.4 decision. They were recorded again. Check that your own items are still there.
- **2026-11-24 Payment Mode box offers Split payment... and Add tender...; remarks on a bill (ask 260, ledger §426): 260.1-260.3 Done. NOT COMMITTED, no release.**
  - **Files (new):** `internal/server/pos_bill_remarks_test.go`.
  - **Files (changed, stage by hunk; asks 262 and 263 edit the same files):**
    - Server: `internal/server/handlers_pim_pos_finance.go` (`Remarks` on the checkout request, `cleanBillRemarks`).
    - Engines: `engines/qz_payload.go` (remarks + `escposWrap`), `engines/print_documents.go`, `engines/qz_receipt_invoice_test.go`.
    - Browser: `public/view-pos.js` (Remarks box, payload, finished panel, clear), `public/view-pos-extras.js` (`wirePOSPayModeActions`, `addPOSTenderFromTill`, receipt remarks), `public/trade-docs.js` (bill row).
    - Guides: `user-guide-selling-and-stock.md`, `admin-guide-settings-numbering-and-batches.md`.
  - **Ask 263 (session erp-6b) will rename the qz_* files** to print_agent_* and `window.QZPrint` to `window.PrintAgent`. It agreed to move them with these hunks intact.
  - **Release risk:** the cold-core budget (180 KiB gzip) is already over in the working tree because of other sessions' `app.js`/`view-pos.js` edits. Ask 260 adds about 108 gzip bytes. The release gate will fail until someone trims or moves code out of the cold core.
  - Verified: go build/vet on engines and internal/server, 3 targeted tests, and a live Playwright check on a scratch server (custom_erp_inspect, :8261, now stopped). The 4 test tenders it made are set Inactive.
- **2026-11-24 till sells only what is in stock here; City/PIN lists; shorter till customer form (asks 261 and 262, ledger §424-§425): 261.1-261.4 and 262.1-262.2 Done. NOT COMMITTED, no release.**
  - **Files (new):** `public/pin-directory.json` (generated, India Post directory), `scripts/gen_pin_directory.py`, `engines/inventory_ats_batch_test.go`.
  - **Files (changed):** `engines/inventory.go` (`AvailableToSellMany`, `MaxATSBatch`), `public/view-pickers.js` (`attachCityPinLists`, the till plan without Sizes/More details), `public/view-pos-customer.js` (ask 259), the user and admin guides (`user-guide-selling-and-stock.md`, `admin-guide-foundation-users-and-settings.md`).
  - **Shared with ask 260 (stage by hunk):** `internal/server/handlers_pim_pos_finance.go` (`handleGetAvailability`: `skus=` batch + `tracked`), `public/view-pos.js` (search list + refusal), `public/view-pos-extras.js` (`uploadPOSLines`).
  - **Decisions auto-picked by the 5-minute ask timeout** (no human answered): the City/PIN source, services hidden but still scannable, only More details hidden. Tell the user; each is easy to reverse.
  - Verified: Go test + build/vet, Node wiring test, live Playwright on a scratch server (also closes 259.1's click-through). Server stopped, scratch deleted.
- **2026-11-23 till New customer takes the typed mobile (ask 259, ledger §423): 259.1 Done (live check 2026-11-24). NOT COMMITTED.**
  - **Files:** `public/view-pos-customer.js` (`typedInBoxes`, used in `openPOSCustomerForm`), `docs/guides/user-guide-selling-and-stock.md` §4 step 5 (that file also carries ask 258's edits, so stage by hunk), the tracker, the ledger, this note.
- **2026-11-23 QZ Tray taken out of what users read (ask 258, ledger §422): 258.1-258.3 Done and archived, 31.1.8 closed as void; 258.4 (rename the code's "qz" names?) Needs decision, recommended keep; 258.5 (PrintBridge + QZ Tray on one marketplace PC) Not started. NOT COMMITTED, no release.**
  - **Files:** `public/qz-print.js`, `public/qz-print-flow.js`, `engines/capability_maturity.go`, `docs/guides/qz-printing-setup.md`, `admin-guide-foundation-users-and-settings.md`, `admin-guide-items-printing-and-stock.md`, `admin-guide-settings-numbering-and-batches.md`, `uat-checklist.md`, `user-guide.md`, `user-guide-labels-and-printing.md`, `user-guide-selling-and-stock.md`, and in `docs/kb/module-handbooks/` the courier, POS, printing and procurement handbooks; plus the four tracker files. `qz-print.js`, `qz-printing-setup.md` and the trackers also carry other sessions' edits, so stage by hunk.
  - `docs/product/capability-maturity.md` (generated) still shows the old Limits text until `pwsh docs/update-docs.ps1` runs; the guide title change also reaches the generated manifests then.
- **2026-11-23 tracker archive pass (ask 256, ledger §421): every Done item moved out of `docs/micro_checklist.md` (61 items plus 256.1). The live file is now 579 KB, down from 626 KB. NOT COMMITTED, trackers only.**
  - **Files:** `docs/micro_checklist.md`, `docs/archive/micro_checklist_closed_stages.md`, `docs/project_ledger.md`, this file. All four also carry other sessions' uncommitted edits (asks 242-257), so stage by hunk.
  - 75.2.8 and 56.11 were closed Done. 73.B.7 was un-ticked (browser check pending). 118 ticked sub-items stay under 23 open parents that are blocked on the owner checklist.
- **2026-11-23 lag and code-quality fixes, and tables written together (asks 253 and 252, ledger §420; the review was asks 248 and 250, §419): 248.1-248.4, 250.1-250.3, 252.1 and 252.2 built and tested, 248.5 measured and not needed; all archived Done. One new migration. NOT COMMITTED, no release.**
  - **Files (stage by hunk where a file also carries another session's work):**
    - DB: `db/db.go` (schema cache, pool 50/25), new `db/tenant_schema_cache_test.go`, new `db/migrations/migrations_stage248_2_lookup_indexes.sql` (applied on local `custom_erp`).
    - Engines: `tenant_lifecycle.go`, `sandbox.go`, `stock_book_ledger.go`, `wms_bin_allocation.go`, `transaction_controls.go`, `wms_gaps.go`, `wms_inbound_work.go`, `wms_loading.go`, `pos_payment.go`, `short_pick_hold.go`, `vendor_po_link.go`, `store_app_floor.go`, `returns_centralised.go`, `wms_vas.go`, `loyalty.go`, `loyalty_online.go`, `wms_wave.go`, `wms_productivity.go`, `transfer_orders.go`, `wms.go`, `warehouse_task.go`; tests `reservation_release_test.go`, `wms_p2_test.go`, new `transaction_controls_test.go`, `wms_gaps_test.go`, `wms_vas_test.go`.
    - Browser: `public/view-warehouse.js` (pick confirm waits for the server).
    - Guides: `user-guide-receiving-and-transfers.md`, `admin-guide-items-printing-and-stock.md`, `admin-guide-developer-and-glossary.md`, `admin-guide-security-and-operations.md`, `admin-guide-foundation-users-and-settings.md`, `admin-guide-finance-integrations-and-go-live.md`.
  - **Verified:** vet on engines, db and internal/server; 24 engines tests (4 new) on `custom_erp`, not `custom_erp_test`, which is unmigrated. Full suite on a fresh `custom_erp_s253` (2026-11-24): engines all pass after one fix (`logTaskCompletionTx` now takes `tenantID` and uses a savepoint, so an ID with no user row no longer fails the cycle count post). Failing and not this build's: `internal/server` DAST print-route 500s and `/ready` 503 (scratch DB one migration behind the tree), `TestOperatorWidthLayoutInvariants` (uncommitted `styles.css`), `TestStage4947IdempotencyAuditIsCurrent` and `securityscan` attack-surface drift (both regenerated at release; 248.2 and 251.2 add two migrations).
  - **Scratch DB to drop:** `custom_erp_s253` on :5490 (script in the session scratchpad, for the user to run).
  - **Ask number clash:** the print-detection ask and this build were both logged as 251; this build is now 253, and 251.N stays with print detection.
  - **Code map and generated pages:** the print-detection session's three new files (`public/print-align.js`, `print-align.test.mjs`, `print-check.js`) are now filed in the *printing* region of `docs/brain/brain.map.json` (user: "finish the left one"); `update-brain.ps1` redrew at 100% and its `-Check` passes; `update-docs.ps1` published 14 pages.
- **2026-11-23 "Build next 20 items" (ask 246, ledger §418): a second Guide bug hunt on the working tree, 4 defects fixed; ask 247 found nothing buildable. NOT COMMITTED.**
  - **Files changed (this ask's hunks only; several files also carry ask 245's uncommitted work, so stage by hunk):**
    - Server: `engines/expense.go`, `engines/report_catalog_expense_hr.go`, `engines/usability_test.go` (246.3); `internal/server/handlers_core_doc_engine.go` (one more `madeOnOwnScreen` entry), `finance_journal_voucher_role_test.go` (one more loop entry) (246.4).
    - Browser: `public/app.js` `MODULE_MENU_GROUP`, `listMenuEntryId`, `restoreActiveMenuState` (246.1); `public/view-printing.js` `stickerSourceOpening` (246.2); `public/view-oms-steps.js` `bookingID` (246.4).
    - Docs: user guides (getting started, finance/CRM/approvals, reports and orders), `user-sop.md` §13.2, the tracker (246.1-246.4 archived), the ledger, this note, and the generated pages from `update-docs`.
  - **Guide tool changes are outside the repo** (`~/dev-tools/guide`: `Guide.ps1` sets `MEDIA_STORE_DIR` under `%TEMP%`; `suites/connected.js`, `connected_more.js`, `print143.js`, `stickers.js`, `printpreview.js`, `orgcycle.js`, `sidebar.js`, `ui14012.js`).
  - **No server or browser left running.**
- **2026-11-22 "Build next 20 items" (ask 245, ledger §417): a Guide bug hunt on the working tree, 14 defects fixed. NOT COMMITTED.**
  - **Files changed (this ask's hunks only; most are shared with other asks' uncommitted work, so stage by hunk):**
    - Server: `engines/report_registry.go`, `engines/pim_listing_drift.go` (245.2); `engines/control_totals.go` (245.3); `engines/service_sla.go`, `engines/expense_project_service_test.go`, `internal/server/handlers_settings.go`, new `internal/server/settings_update_validation_test.go` (245.11); `internal/server/handlers_core_doc_engine.go`, `handlers_profile.go`, `finance_journal_voucher_role_test.go` (245.4, 245.5); `engines/wms_outbound_work.go` and its test (245.14).
    - Browser: `public/view-crm-messaging.js` (245.1), `public/app.js` `sweepLinkNames` (245.6), `public/view-inline.css` (245.7, 245.9, 245.10, 245.12), `public/view-hr-pay-extras.js` (245.8), `public/view-documents.js` (245.13).
    - Docs: admin guide (finance, integrations and go-live: INVENT-0499 note), the tracker (245.1-245.14 archived), the ledger, this note, and the generated pages from `update-docs`.
  - **Guide tool changes are outside the repo** (`~/dev-tools/guide`: `guide.js` keeps a store-switch token, `suites/flows.js`, `flows3.js`, `config.js`, `kbhelp.js`).
  - **Left in the scratch DB `custom_erp_inspect`:** Guide's own rows (an open till at GUIDE-TS, 11 INSPECT roll templates). It is recloned on Guide's next run.
  - **No server or browser left running.**
- **2026-11-22 "Build next 20 items approved all and recommened all" (ask 244, ledger §416). NOT COMMITTED.**
  - **Files changed:**
    - `public/app.js`: only the `renderHomeView` approvals hunk and `homePendingLast` above it (244.1).
    - `docs/qa/perf-budgets.json`: three ratchet ceilings and the `ratchetNote` tail.
    - `docs/product/erp-build-checklist.md`: BLD-045 and closeout row 1 ticked, one BLD-045 note.
    - The tracker (244.1, 50.14, ask 244's header; 162.5 with its sub-items moved to the archive), the ledger and this note. These are shared files, so stage only this ask's hunks.
  - **Done:** 244.1, 162.5, 234.5 (archived). **Blocked:** 50.14 (BLD-042/044/047 need a Linux or production host).
  - **Gate:** whole gate met but phone LCP 2196 on this laptop; the 0.28.0 GitHub evidence job met every budget.
  - **No server or browser left running.** Scratch DB `erp_perf_244` is left for the user to drop (script in the session scratchpad).
- **2026-11-22 "build next 20 open items", then "approved all" (ask 243, ledger §415). NOT COMMITTED.**
  - **Files changed:**
    - `public/qz-print.js`: only the `connect()` wait after a failed round (woken now or a moment ago: until 10 s after that wake).
    - `docs/guides/qz-printing-setup.md` and `docs/guides/user-guide-labels-and-printing.md`: the Edge *printbridge-agent* prompt (tick Always allow, click Open).
    - The tracker, archive, ledger and this note. These are shared files, so stage only this ask's hunks.
    - Guide outside the repo: `~/dev-tools/guide/suites/pbdown.js` (PrintBridge-down timing, `noPrint`, `qzFile`) and `edge-prompt.ps1` (answers Edge's prompt).
  - **Done and archived:** 132.9, 132.12.
  - **212.1:** "did not print" 14.6 s on app 0.28.0, 9.5 s with the fix. It ships at the next release.
  - **194.1:** a real Line up strip printed twice on the LD0801. The letter tap is left to the user.
  - **No server or browser left running.** PrintBridge is running again.
- **2026-11-20 "Start building next 20 items. all approved and all recommened" (ask 242, ledger §414). NOT COMMITTED.**
  - **Files changed:**
    - `public/app.js`: only the `bootstrap` hunk (qz-print requested, `init` on the next frame) and `init`'s `loadViewModule('/qz-print.js')` line.
    - `public/index.html`: the qz-print script tag replaced by a one-line note.
    - `public/styles.css`: the phone-boot logo rule after the `min-width: 821px` boot block.
    - `docs/guides/po-setup-guide.md`: two label fixes.
    - The tracker, archive, ledger and this note. These are shared files, so stage only this ask's hunks.
    - Guide suites outside the repo: `~/dev-tools/guide/suites/modules.js` (PIM own masters + clean-up) and `guidecheck.js` (reads po-setup-guide).
  - **Done and archived:** 230.5, 131.2, 124.1.
  - **234.5 built.** Phone LCP p75 1492 on a `--no-server` gate, so the release gate is the evidence. Cold core is 183,315 / 184,320.
  - **Waiting on the user's own yes:**
    - 132.12: PIM on app. It also needs an admin sign-in, because manager1 and cashier1 may not set up PIM.
    - 132.9: `flows -AllowWrites` on app.
    - 214.1: the watched run.
  - **Dev state:** no server or browser left running (scratch 8746 stopped; Guide's 8190 stopped by Guide). `custom_erp_inspect` has three approved ProductContent rows from the first rehearsals (scratch DB only).
- **2026-11-19 ask 241 after 0.28.0 (ledger §412). NOT COMMITTED: `cmd/journeylab/journeys.go`, which goes in the next release.** 0.28.0's evidence job passed browser budgets, so 47.9.4 is Done. It failed J5, because an unready period close needs a reason since 162.12 (FIN-0507). J5 now gives that reason for J2's one expected mismatch hold. Verified with vet, `go test -p 1 ./cmd/journeylab/` and a J2+J5 PASS on a scratch server, which is stopped. 47.17.2 closes when the next release's GitHub run passes journey. The scratch DB `journey_241_scratch` (local :5490) is dropped by `drop_journey_241_scratch.ps1` in this session's scratchpad, which the user runs.
- Older §6 entries (up to the 0.28.0 release on 2026-11-19) are kept verbatim, newest first, in [archive/handovers/ai-handover-2026-10-26.md](archive/handovers/ai-handover-2026-10-26.md) (moved 2026-11-12, Stage 134.9, 2026-11-14, item 186.1, and 2026-11-24, items 264.2 and 265.3, so this page fits its 150-line budget).
