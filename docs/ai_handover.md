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
last_verified: 2026-10-04
review_by: 2026-10-18
supersedes: historical handover captured 2026-09-10
superseded_by: none
---

# Current developer handover

Start with this page, then use the [live checklist](micro_checklist.md) for open gates and the
[project ledger](project_ledger.md) for dated evidence. The full handover that preceded this
concise index is preserved as a [2026-10-03 capture](archive/handovers/ai-handover-2026-10-03.txt).
Its document ID is distinct from this live page; historical entries are context, not a substitute
for checking the current source and worktree.

## Current repository state

- Last observed HEAD: `afaf419`. The tree is intentionally dirty and contains parallel work;
  preserve unrecognized edits. No commit, push, deployment or external delivery was performed for
  the 2026-10-04 batch. One additive migration *was* applied, to a disposable local database only
  (`custom_erp_t2` on loopback 5490) — never to a tenant or production database.
- The Stage 50.14 / BLD-041 native ES-module split is recorded complete in ledger §181. Frontend
  screens load lazily after authorization; there is no bundler or npm build step.
- **2026-10-04 batch (ledger §190).** BLD-046 and BLD-048 **closed**; 47.8.2 **done**. BLD-052,
  BLD-055, 47.8.1 and 47.8.4 advanced only — their entries state what remains; do not read the
  progress notes as closure. 47.8.3/47.8.5 stay excluded under their own gates.
- **The browser runs under an enforced strict CSP** (`script-src 'self'`, no inline exception): all
  126 inline handler attributes are gone, replaced by the `data-act` dispatcher in `public/app.js`.
  **A new inline `onclick=` will silently not run** — use `actionAttrs()`, never relax the policy.
  A committed test fails if one returns to a shipped file.
- **Five failures in this tree are not from this batch**, were left alone, and were identical across
  three consecutive full runs. `TestKnownModulesMatchTheTenantSchema`,
  `TestAdministratorAndAuditorCoverEveryShippedDoctype` and `TestModuleManifestCatalog` trace to a
  concurrent session's uncommitted removal of `"Store"` from `knownModules`
  (`engines/role_templates.go`) against a fresh database where
  `migrations_stores_master_fields.sql` sorts *after* `migrations_stage30_5_5_retire_stores.sql`
  and re-seeds the doctype it retired — a real ordering interaction that session should know
  about. The other two sit in `engines/audit_evidence.go` and `internal/kb/drift.go`, also modified
  by that session.
- BLD-023 is still an explicit product/architecture decision blocking the SaaS-packaging wave.
  Do not infer or make that decision while building unrelated locally actionable work.
- Stage 52 (sticker/label printing) is closed except 52.7, which is a deliberate "if/when asked"
  extension point, not a defect. 52.8 fixed per-lot print line selection; the print endpoints now
  also accept a per-line `lines` array and the older `skus`/`copies_override` shape still works.
  Detail in the checklist and ledger.
- Stage 51 is closed except **51.1a's backfill decision (still the user's)** and a small follow-up
  51.10 — see ledger §184. Two facts worth carrying: the "Master id = code" rule now lives in one
  place, `engines.ApplyMasterIDCodeInvariant`/`IsMasterDoctype` (`engines/doctype.go`), used by both
  `handleGenericDoc` and `importBatch`; and 51.1a has a read-only audit script,
  `scripts/audit_master_id_code_drift.sql`, kept in `scripts/` on purpose because
  `db/migrate.go:361` runs every `.sql` in `db/`. Nothing was run against production.
- Concurrent-session caution (2026-10-04): several sessions write to this tree at once, so re-read
  any file immediately before editing it and attribute suite failures before chasing them.
  `TestEveryAPIMiddlewareRouteIsClassified` was failing on an unclassified
  `POST /api/v1/security/csp-report` route — **fixed**, it is now `LevelPublic` in
  `route_capabilities.go` (a browser posts violation reports with no Authorization header).
  **The portable Postgres runs on 5490, not the 5435 the `db` package hardcodes**, so always pass
  `TEST_DATABASE_URL` and verify the live listener before trusting a suite result.

## Build and verification

- Build the server with `go build ./cmd/server` (or `go build ./...` for package coverage).
- The browser uses `public/index.html`, classic `public/app.js`, and native modules
  `public/view-*.js`. Run `node --check public/app.js`; check modules with
  `Get-Content -Raw public/view-X.js | node --input-type=module --check`.
- `TEST_DATABASE_URL` overrides the test database connection. Use an isolated disposable
  PostgreSQL database for database-backed suites; do not point tests at a shared tenant or assume
  a service/port is live. Check listeners and database identity before starting or mutating one.
- A prior test caused unrecoverable deletion of development audit history; the production database
  was not affected. Read the current archive/test notes in the ledger before running audit archive
  tests or broad `engines` suites. Never run those suites concurrently against a shared database.
- For release budgets, `go run ./cmd/releasebudget -root . -binary <stripped-release-binary>`
  emits JSON with byte units; pass a read-only database URL for storage observations. The same
  thresholds now run as Go tests (`go test ./cmd/releasebudget/`), so a regression breaks the suite,
  not only CI. The two JS metrics are **different sets**: `initial_js_gzip_bytes` is the gated
  shell-only figure; `initial_js_plus_first_screen_gzip_bytes` adds the first lazy screen and is an
  unbudgeted observation. Do not merge them to make a number look better.
- Browser XSS checks: `node docs/qa/browser-xss-boundary.cjs <abs-evidence-dir>` and
  `node docs/qa/browser-xss-action-boundary.cjs <abs-evidence-dir>`.
- `GET /api/v1/ready` is readiness, distinct from `/api/v1/health` (liveness): 503 while migrations
  are pending or the process is draining, naming the pending migrations. Both public.
- Regenerate brain pages only through `pwsh docs/brain/update-brain.ps1` (or the docs wrapper).
  Review staged output first; never hand-edit generated pages or change access controls to force a
  write. The updater's temp-then-copy path exists for Windows-protected folders.

## Safety and release boundaries

- Keep tenant identity and schema resolution server-side. Do not add shared services, Redis or a
  broker without measured need and an approved architecture change.
- CSP: `script-src`'s inline exception **is retired** (2026-10-04) and enforced, after both phases
  measured zero violations across all 29 views. The report-only header now carries the next
  candidate, strict `style-src` (629 inline-style violations reported, not enforced) — retire it
  the same way: migrate, confirm zero report-only violations, then flip. HTML-sink classification
  (47.8.1) stays open; ~300 candidates are listed in
  `security/browser-boundary-sink-classification.json`. See the
  [browser inventory](security/browser-boundary-inventory.md).
- `OPS_ALERT_WEBHOOK_URL` and `OPS_ALERT_RESPONDER` are both unset. Local drills point the webhook
  at a loopback collector and are safe; do not send real external alerts or claim responder delivery
  until an authorized endpoint and named owner are configured. Setting those two variables is the
  whole remaining change — delivery, redaction, bounding, cooldown and responder naming are built.
- Vendor penetration testing, physical device acceptance, production changes and commercial/legal
  decisions remain separate gates. No production deploy or external message is implied by local
  code/tests.
- Preserve the existing no-commit instruction unless the user explicitly asks for a commit in the
  active task. Review `git status` and stage only individually reviewed files.

## Durable references

- [Build checklist](product/erp-build-checklist.md) — item scope, dependencies and Done bars.
- [Micro-checklist](micro_checklist.md) — child-level statuses and operational TODOs.
- [Project ledger](project_ledger.md) — chronology, prior test hazards, and evidence.
- [Developer setup](engineering/developer-setup.md) and [service operations](operations/service-operations.md).
- [2026-10-03 prior handover snapshot](archive/handovers/ai-handover-2026-10-03.txt) — preserved copy of
  the previous long-form handoff; follow the live sources above where it is stale.

## 6. Version Control
- **2026-10-28 Stage 140 (Guide runs the whole organisation cycle on app as user, BA, audit, accounts, customer; session erp-50) - DONE; docs only, NOT COMMITTED (ledger §302).** No ERP code changed. Files (mine): new `docs/qa/guide-org-cycle-2026-10-27.md` (process record), the Stage 140 section of `docs/micro_checklist.md` (140.1-140.11 Done, 140.12 = 27 enhancements; the user took every recommended answer 2026-10-28, only live WhatsApp/SMS waits on their provider accounts), ledger §302, this entry. Guide (outside the repo): new `suites/orgcycle.js` (`-Only sales,oms,wave,pim,crm,crmsend,acct`; wave and crmsend local only), `suites/flows.js` compares ATS not available (held units). **Live data left on app** (test store GUIDE-TS, user-approved): customer C00005, bills BGTS2627000002-04, advance CAGTS262700001, SI/GTS/2627/00001-00002, CN/HO/2627/0001, order SO-1793138172765898509-7tam34xu + SINV + return, zone GUIDE-ZA, bin GUIDE-R1, courier area GUIDE, attributes GUIDE-FABRIC / -SLEEVE, channels GUIDE-SHOPIFY / -AMAZON / -MYNTRA (off), 3 PIM export templates, segment + 2 templates + campaign draft CMP-1793138354296334258-7tam34xu (never submit), INSPECT employee + 2 payslips + loan, purchase return PRT/HO/26-27/00003, VI-1793138700289195880-7tam34xu, NCR, service ticket. Local scratch: `custom_erp_inspect` gained more Guide rows (throwaway); one admin draft SI/GTS/2627/00007 there was posted by cashier1 on purpose (140.12.9).
- **2026-10-27 "Build next 20 items" (session erp-d3: 14 Stage 47 / 49 slices - 47.11.8, 47.13.3, 47.14.2, 47.18.4, 49.2.1, 49.2.6, 49.3.2, 49.3.4, 49.3.5, 49.5.4, 49.5.6, 49.8.7, 49.10.5, 49.10.8) - BUILT, NOT COMMITTED; HELD in `release/hold.txt` (section `# erp-d3 "next 20" batch`) until the user approves (ledger §300).** Its six Stage 135 items duplicated erp-f9's work below; erp-f9's version stays and none of this run's 135 code was ported. One migration: `db/migrations_stage49_2_1_identity_lifecycle.sql` (users.last_sign_in_at / reactivated_at, additive). New settings `security.dormant_account_days`, `security.api_credential_unused_days` (both Off). New background job `StartIdentityLifecycleSweep` (hourly). No new route. Full suite on fresh `custom_erp_n20post` (5490, throwaway) green except `TestAttackSurfaceManifestIsCurrent` (that job + migration; G6 regenerates).
  - **Behaviour changes to know:** CSV import and PIM import preview now need create / update on the record type (403 GLOBAL-0011); PIM bulk edit, generic save and import can't write Approved / Rejected on approval-gated types; under self-approval admins / nobody the submitter / last editor of a round can't decide it; a job runs only for the client of the schema it was queued in; `ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=1` now also stops connector / courier writes; Date fields are validated and stored ISO (day-first typed dates converted, impossible ones refused); courier outages answer INTEGR-0167; server ReadHeaderTimeout 10 s / MaxHeaderBytes 64 KiB.
  - **New files (30):** engines `identity_lifecycle.go`, `import_controls.go`, `locale_dates.go`, `security_detection_abuse.go` + tests; internal/server `server_limits.go` + tests; `internal/supplychain/gate.go` (+ test); `internal/securityscan/egress_test.go`; `cmd/gendocs/finding_register_test.go`; the migration. Shared-file hunks are listed in the hold section (whole files held - several also carry erp-f9's / others' hunks, so releasing either batch needs the other's OK or a hunk split).
  - **Left behind:** scratch DBs `custom_erp_n20{m,s,p,i,a,b,f,e,d,full,post}` on 5490 (throwaway). Brain redrawn (100%), graphify updated.
- **2026-10-27 Stage 135.10-135.20 (decisions; stock writers, sub-location check, till vs reservations, production / GL / return-QC locking; Price List Version approval; Shopify article; KB list converter) - DONE, NOT COMMITTED; HELD in `release/hold.txt` (blocks "# Stage 135.12-135.19" and "# Stage 135.12-135.20, more paths"). 135.21 (15 unlocked posting paths) and 135.23 (new clients get no stock locations) Not started; 135.22 needs a decision (ledger §299).** No migration, no new route, no new setting.
  - **New file:** `engines/stock_integrity_stage135_test.go`.
  - **My files (whole-file mine, unmodified before):** engines `inventory.go` (`respect_reserved` floor, `InsufficientStockError.Reserved`), `pos_checkout.go`, `fulfillment.go` (dispatch via choke point), `wms.go` (stale count), `delivery_challan.go`, `transfer_orders.go`, `manufacturing.go` / `manufacturing_mrp.go` (order lock), `finance.go` (advisory lock), `returns.go` (lock + one ledger line per SKU), `wms_receiving.go` (QC splits merged per SKU), `stock_book_ledger.go`, `shipping_package_test.go` (fixture stocks what it ships); internal/server `handlers_core_doc_engine.go` + `action_only_status_stage132_test.go` (PriceListVersion Superseded); `internal/kb/markdown.go` + `markdown_test.go`; KB sources `channel-connectors.md`, `pim-pxm.md`, `faq.md`, `hr-payroll.md`.
  - **My hunks in shared files:** `public/view-documents.js` (one `// 135.12` line + `PriceListVersion` in the submit-document condition, ~l.386; the file's other hunks are Stage 139's); `docs/guides/USER_GUIDE.md` (§8.3 Price lists, §8a competitor note, till reservations paragraph, cycle-count stale sentence, drift-finder own-locations sentence, OMS handover shortage sentence); ADMIN_GUIDE B.3.0.8 price-list bullet; plan §8 / scorecard, BLD-023 note, service-operations Restore row, data-and-runtime-views gaps. Regenerated: KB articles / search.json (`update-docs.ps1 -Group KB`), brain map.
  - **Overlap with erp-d3's held "next 20" batch (2026-10-27):** `engines/inventory.go`, `internal/server/handlers_core_doc_engine.go`, `public/view-documents.js`, the two guides and others now carry hunks of both batches (erp-d3's list is under `# erp-d3 "next 20" batch` in hold.txt). Whichever batch ships first must lift the other's lines or split hunks.
  - **Behaviour to know:** a till sale can no longer take units reserved for orders at its store; a dispatch / handover of more than the location holds is refused ("mark the rest short first"); a cycle count whose variance would take on_hand below zero is refused; Stock Book vs Ledger has two new columns.
  - **Scratch:** DBs `custom_erp_s135b` (helpers' live check, P00001-P00003) and `custom_erp_s135full` (full suite) on 5490, throwaway (the build worktree is already removed).
- **2026-10-27 Stage 128.5.1 (minn and app configured from their Setup Workbooks) - DONE, live on both sites (ledger §298).** Guide applied the corrected copies `Downloads\MansoorBusiness\{minn-setup-workbook-v2,app-setup-workbook-v2,-v3}.xlsx`. Re-previews read 58/58 and 61/61 unchanged. The record for the user is `Downloads\MansoorBusiness\client-setup-minn-and-app.md`. The engine fix (a partial HSN update keeps the stored number: `engines/hsn_catalog.go` + `hsn_catalog_stage128_test.go`) was RELEASED in 0.18.0 `9a008cf` and DEPLOYED to app and minn by erp-aa. The three tracker edits are not committed. **Needs the user:** categories for minn's 5 held item names; minn users; minn admin's home store "HO" (no such location); more minn vendors; app's Wholeops GSTIN and the HO legal entity; keychains' HSN.
- **2026-10-26 Stage 139 (ERP shortcuts: Ctrl+K box, 52 short codes, report numbers R1001..., one key set, extras a-h) - all 7 items DONE (verified locally), NOT COMMITTED; HELD in `release/hold.txt` (block "# Stage 139"). Ledger §297, plan `docs/product/shortcuts-plan-2026-10-26.md`.**
  - **New files:** `public/shortcuts.js` (lazy, owns the top-bar search now), `public/shortcuts.css`, `public/shortcut-codes.json`, `docs/specs/reports/report_numbers.tsv`, `db/migrations_stage139_user_shortcuts.sql` (UserShortcut record type, additive), `internal/server/handlers_doc_find_stage139.go` (`GET /api/v1/doc-find`) + `doc_find_stage139_test.go` + `shortcuts_stage139_test.go`, `engines/report_numbers_stage139_test.go`, `docs/kb/getting-started/keyboard-shortcuts.md`.
  - **My hunks in shared files (stage only these):** `public/app.js` (search block removed -> `loadViewModule('./shortcuts.js')`; one `erp:write` dispatch in `apiFetchOnce`; erp-4b's hunks sit beside them), `public/view-documents.js` (ticks for every list, `#doc-bulk-bar`, `runDocBulk`, Copy row action; beside erp-4b's), `public/view-procurement.js` (Approvals ticks + `decideApprovalsTogether`, `copyPurchaseOrder`, PO save `erp:txn-done`), `public/view-reports.js` (number in menu/picker/run line/export name), `public/txn-screen.js` (`txnDone` event), `public/styles.css` (.global-search-* moved out), `engines/report_registry.go` (`Number`, `ReportNumber`), `engines/scope_policy.go` (UserShortcut personal view), `engines/pim_dam_test.go` (TestMain snapshot), `cmd/gencatalog/main.go`, `cmd/gendocs/kbdocs.go`, routes + route_capabilities, `docs/guides/USER_GUIDE.md` §3A, `docs/brain/brain.map.json`; regenerated: attack_surface.json, KB content, report catalog KB, page inventory, dictionary.
  - **Release needs:** migration `139_user_shortcuts` (record type only, no client data); one new session route (attack surface regenerated). Cold core 180,920 / 184,320 (freed ~2 KB). The local dev DB `custom_erp` had only that migration applied by psql (not recorded; idempotent). Scratch DB `custom_erp_s139` left in place.
  - **Not clicked live:** the after-save next-step button and an Approve/Reject ticked run (no pending data). Seen, not changed: the PO screen shows + New to a role without create permission.
- **2026-10-26/27 "build next 20 open items" (second run: Stage 49.17.5-49.18.7 + Stage 50 BLD-002/028/029/032/041/044/045/053/055) - all 20 BUILT, user-approved, RELEASED in 0.18.0 `9a008cf` and DEPLOYED to app and minn 2026-10-27 (ledger §296, §301).** Follow-ups 49.18.8 (lookup caching) and 50.20 (font, list columns) done after the release, NOT COMMITTED; 50.20's hunks and the BLD-045 hunks in app.js / view-documents.js sit in files the Stage 139 hold block holds whole, so they ship with Stage 139. One migration: `db/migrations_stage50_15_bld053_audit_sig_key.sql` (additive `sig_key` on audit_logs / audit_checkpoints; applied to scratch DBs only). New setting `platform.error_log_retention_days`. Full suite green on fresh scratch DB `custom_erp_n20_full` (5490, throwaway; also `custom_erp_n20`, `_b41`, `_b45`).
  - **Behaviour changes to know:** `listTenantSchemas` now serves only operational clients (14 maintenance loops use `listRegisteredTenantSchemas`, allowlisted by test); all retention sweeps go through `retentionDelete` (batched, no delete under legal hold); `apiMiddleware` takes the per-tenant slot right after the token and refusals log one summary row per client per minute (`refuseBusy`); audit verification reports rows signed by a missing key as unverifiable; Deploy.cmd reads `release/security-floor.txt`.
- **2026-10-26 Stage 138 (no "Switch product" box for a client) - BUILT, RELEASED in 0.14.0 `0bcac93` by session erp-55 (ledger §294); not yet checked on minn after the deploy.** Removed `renderProductSwitcher` (app.js), `owned_packages` (handlers_profile.go), `ResolveOwnedPackages` / `IsFullSuite` (modules.go). `docs/qa/module-manifest-ui.cjs` already fails at HEAD (`quickCreateReturn is not defined`), a harness gap.
- **2026-10-25 "Continue building. Next 20 items" (Stages 47 / 49) - all 20 BUILT, NOT COMMITTED (ledger §293). RELEASE SOON: two security fixes are live bugs on app / minn - the MFA challenge / enroll token passed as a full session (`purposeTokenRoutes` in middleware.go) and a purged client's extension token resolved to tenant_default (`TenantIsOperational` gate for extension tokens). Also: one reset link used twice at once set two passwords (`CompletePasswordReset`'s UPDATE now claims the token).** One migration: `db/migrations_stage47_11_7_hook_api_version.sql` (additive column; applied by hand to scratch `custom_erp_f9` / `custom_erp_s1358`). New settings: `platform.integration_log_retention_days`. New codes: EXT-0292, GLOBAL-0309. Files (mine): engines `extensions.go`, `integration_log_retention.go` (new), `settings_definitions.go` (one block), `doctype.go` (unsafeTextRune + check), `import.go` (safeCSVWriter, numeric pass-through), `report_export.go` / `pim_export_template.go` / `pim_product_groups.go` / `pim_reports.go` / `print_export.go` (writer swap), `jobrunner.go` (secret redaction, dead-letter audit), + 8 new `*_stage4*_test.go` / `*stage49*_test.go`, `password_reset_test.go` + `import_sanitize_test.go` (edited); internal/server `middleware.go` (RetryAfter, heavy-read cap, no-store, purposeTokenRoutes, extension tenant gate, tenant-routing audit - **its CSP `frame-src printbridge:` hunk is Stage 137.5, not mine**), `page_inventory.go` unchanged, `handlers_settings.go` (audit names keys), `handlers_integrations_admin.go` (api_version), `handlers_core_doc_engine.go` (out-of-scope read = 404 + audit), `handlers_auth.go` (ACCOUNT_LOCKED), `error_catalog_extensions.go` (2 codes), `routes.go` (one sweeper line), 9 new `stage4*_test.go`, `rate_limit_bounds_test.go` / `report_schedule_owner_stage73d9_test.go` / `stage73_attachments_test.go` (expectations); cmd/gendocs `pages.go` (help + gaps) + `pages_test.go`; public `app.js` (apiFetch coalescing + 429 retry + renderErrorPanel kinds + one text), `view-admin.js` (labels, escapes, one line removed), `view-finance.js` (labels); docs: checklist entries, ADMIN_GUIDE (integration log retention), `extension-hooks-checklist.md`, `security/audit-and-logging-policy.md` (taxonomy), plan doc owner row, `qa/generated/page-inventory.md` (regenerated). Cold core 182,218 / 184,320.
- **2026-10-25 Stage 137 (printer presets; one PrintBridge that wakes with the ERP) - 137.1, 137.2, 137.7 DONE; 137.3-137.6 BUILT, NOT VERIFIED on a real PC (the user runs the new installer and prints Test alignment); NOT COMMITTED (ledger §292).** Tested on scratch DB `custom_erp_s137` (5490, migrated from empty; throwaway) and a scratch server on 8237 (stopped).
  - **New files:** `db/migrations_stage137_1_printer_preset.sql` (Printer fields only), `engines/sticker_printer_preset.go` + `_stage137_test.go`, `public/downloads/printbridge-setup.exe` (2.8 MB, committed through a `.gitignore` exception) + `printbridge-version.txt` (0.2.0).
  - **My hunks in shared files (stage only these):** `engines/stickers.go` (printerCode into `resolveStickerLabel`, one `applyPrinterRoll` line), `engines/qz_print.go` (5 preset fields), `internal/server/middleware.go` (`frame-src 'self' printbridge:` in both CSP strings - another session has large edits in this file), `public/qz-print.js` (wake + `agentVersion`), `public/index.html` (`qz-print.js?v=2`), `public/sticker-engine.js`, `public/view-printing.js`, `public/sticker-studio.js` (`export` on `patchDoc`), `.gitignore`, `release/scripts/Invoke-Release.ps1` + `release/README.md` (G4 qz-print bump, new G4d), `docs/brain/brain.map.json` (2 match lines + role sentence; 3 other lines are another session's), USER_GUIDE §7A.1 / §7A.6, ADMIN_GUIDE sticker section (2 other lines there are another session's), QZ_PRINTING_SETUP.
  - **PrintBridge 0.2.0** lives in `../PrintBridge` (no git history there): wildcard origins, ask-once, `printbridge:` wake link, `-wake`, `printbridge.version`, `build.ps1 -Universal`, NSIS policies. Rebuild the ERP download whenever the agent changes (G4d stops a release otherwise).
  - **Release needs:** migration `137_1` (fields only, no client data), no new route or env. Cold core 182,848 / 184,320.
- **2026-10-25 "build all" resumed (user: "Yes go ahead") - 135.11 DONE and RELEASED in 0.14.0 (b960805, by session erp-62); 135.7, 135.6, 135.5 DONE, NOT COMMITTED; 135.8 first slice (Setup → Buy → Move → Sell) passed with no bugs - Guide suite `~/dev-tools/guide/suites/connected.js` (outside the repo), scratch DB `custom_erp_s1358` on 5490 (throwaway; drop + re-migrate between runs); next Stage 55, Stages 47 / 49 / 50; recorded, not started: 135.15 (KB converter drops wrapped bullet lines), 135.16-135.19 (135.6 stock / GL gaps; 135.18 needs a decision).** 135.6 files (mine): `docs/architecture/data-and-runtime-views.md` (new section, dates), `engines/orders.go` (`CancelOrder` reservation release), `engines/pos_payment.go` (`releasePOSReservations`), new `engines/reservation_release_stage135_test.go`. 135.5 files (mine): new `internal/server/page_inventory.go` + `_test.go`, new `cmd/gendocs/pages.go` + `pages_test.go`, `cmd/gendocs/main.go` (one block) + `main_test.go` (fixture, count 7), new `docs/qa/generated/page-inventory.md` (generated; G7 regenerates), `docs/governance/register-policy.json` (one rule), `docs/qa/verification-strategy.md` (one link). 135.7 files (mine): `public/styles.css` (KB block and `.rf-*` rules removed, 2-line pointer), new `public/kb.css` + `public/rf.css`, `public/app.js` (lines 357 / 420: 4th registry entry), `public/view-help.js` (`openHelpDrawer` loads kb.css), `internal/server/frontend_accessibility_test.go` (RF test reads rf.css), `docs/brain/brain.map.json` (two lines) + brain redraw. Cold core 181,625 / 184,320 (2,695 free). 135.11 files (mine, whole hunks): `engines/returns.go` (`ScheduleReturnReversePickup`: blank courier = the booking's carrier picked by PIN code; a stopped booking is finished on retry), `engines/courier.go` (one line: `$1::text, $2::text` in `ScheduleCourierPickup` - it failed on every call), `internal/server/handlers_returns.go` (only `pickup_pincode` required), `public/view-returns.js` (`openReversePickupDialog`), new `engines/returns_reverse_pickup_test.go`; USER_GUIDE returns step; KB `courier-integrations.md`. No migration, no new route. Tested on `custom_erp_f9` (5490, throwaway) and Chromium on scratch :8211 (stopped; fixtures deleted).
- **2026-10-26 Stage 132 (Guide - the bug inspector) - 132.13, 132.15 (Setup Operator role), 132.17 DONE and live; 132.19 (business day: prod Postgres is UTC, days now counted in Asia/Kolkata via public.erp_day_start/erp_today/erp_local_date + engines.BusinessToday(), guard TestNoServerClockDays) and 132.20 (six decisions: short code reserved, leave remaining on approval, payroll loss-of-pay for Absent/Unpaid, returns ledger backfill, every stock-ledger write in its movement's transaction) SHIPPED in f78a96e (0.17.0, deployed 2026-10-26 21:20) - verified live 2026-10-27 (405 reports on app, minn on 0.18.0); 132.18 (Guide 100%) and 132.21 (`act` suite: any ERP task on request, preview first, writes only on the user's OK) DONE (Guide lives outside the repo). 132.22 (finished goods now carry their material cost - Guide found Inventory Valuation Rs 600 under GL 1200 on app) BUILT, NOT COMMITTED: engines/manufacturing.go + manufacturing_mrp.go (on top of 135.19's uncommitted hunks in the same files), engines/production_cost_stage132_test.go, USER_GUIDE §9F.2, tracker lines.** release/hold.txt holds erp-4b's unverified Stage 49/50 batch (and its docs/generated manifests + document register) and erp-d1's Stage 139 files - remove those lines only when their owners release. Engine tests must run on a fresh migrated DB, not `custom_erp_inspect` (Guide's courier service areas there switch on the pincode check and fail the ship tests). The tool lives outside the repo at `~/dev-tools/guide` (README; memory `reference-erp-bug-inspector`): `pwsh Guide.ps1 -Suite walk|stickers|forms|learn [-Roles] [-Only] [-WorkingTree]` locally, `-Site app` / `-Site minn -Via app` on the live sites in a visible Edge (the user signs in once in the GUIDE window; writes blocked unless the user OKs the run). Claude runs it on request. Left behind: worktree `%TEMP%\erp-inspect-wt` (detached HEAD, `git worktree list` shows it), binaries in `%TEMP%\erp-inspect-bin`, scratch DB `custom_erp_inspect` on 5490 (has INSPECT-* printers, a PrinterAssignment for INSPECTOR-PC, INSPECT template rows and test print-log rows - throwaway). Server stopped.
- Older §6 entries (2026-10-24 and before) are kept verbatim, in their original order, in [archive/handovers/ai-handover-2026-10-26.md](archive/handovers/ai-handover-2026-10-26.md) (Stage 134.9 / 48.8: the live file back under its 150-line budget).
