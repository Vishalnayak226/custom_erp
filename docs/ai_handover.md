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
[project ledger](project_ledger.md) for dated evidence. The full handover that preceded this
concise index is preserved as a [2026-10-03 capture](archive/handovers/ai-handover-2026-10-03.txt).
Its document ID is distinct from this live page; historical entries are context, not a substitute
for checking the current source and worktree.

## Current repository state

- The tree is shared and usually dirty: §6 below names each session's uncommitted work. Keep
  edits you do not recognise.
- The portable Postgres runs on 5490, not the 5435 the `db` package defaults to: always pass
  `TEST_DATABASE_URL` and check the listener before trusting a suite result.
- The 2026-10-04 notes that stood here are kept in the
  [archived entries](archive/handovers/ai-handover-2026-10-26.md#repository-state-notes-2026-10-04).

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
- **2026-11-15 Release 0.25.0 deployed (ask 204, ledger §379).** origin/main = 53af64d = live on app.wholeops.in and minn.wholeops.in (25 migrations; backup `/opt/erp/backups/custom_erp_20261115T132658Z.dump.enc`, rollback copy `/opt/erp/rollback_20261115_185702`). The release is 7c43c56 + 81b7a92 + 06fe9ae + 53af64d and carries asks 193-203 (the 193 entry below is now shipped).
  - **Pipeline:** `release.yml` promote now moves main through the GitHub API (`force=false`); a `git push` from its depth-1 checkout was refused. New gates: G0 GitHub runner load (no `TEMP` on Linux) and G0 GitHub promote. `candidate/0.25.0` is the only candidate branch; promote deletes it after a pass.
  - **Not done at this deploy:** `ERP_AUTOMATION_KEY_SHA256` (195.1) not set; `OPS_ALERT_WEBHOOK_URL` unset; no restore drill on the server yet. Ask 200 (erp-63) was not waited for and goes out with the next release. `release/security-ack.txt` (git-ignored) holds the two Razorpay webhook acks.
  - **Tree:** eight files show as modified with no content change (line endings or mode only); nothing is held in `release/hold.txt`.
- **2026-11-15 "Build everything. All approved" - every buildable open item (ask 193, ledger §378). shipped in 0.25.0, see the entry above.** What only the owner can do is in `docs/operations/owner-checklist.md` (§1-§11); every blocked item points at its line there. Nothing reaches users until the next release.
  - **New files (main):** `engines/saas_subscription.go`, `saas_package_transition.go`, `saas_metering.go`, `saas_invoice_gst.go`, `tenant_portable_export.go`, `webauthn.go`, `webauthn_store.go`, `transfer_store_scope.go`, `connector_unicommerce.go` and their tests, `tenant_lifecycle_walk_test.go` (BLD-028); `internal/server/handlers_saas_billing.go`, `handlers_passkeys.go`; `public/passkey-client.js`, `view-subscription.js`; migrations `stage49_2_3_passkeys`, `stage50_12_saas_subscriptions`, `stage50_12_saas_tax_invoice`, `stage74_0_1_job_worker_received_on`, `stage146_4_1_unicommerce_platform`, `stage146_4_1_marketplace_invoice_no` (all additive, no data change at release); `docs/operations/owner-checklist.md`, `docs/product/package-entitlements.md`, `docs/kb/admin-operations/subscription.md` (and its generated page).
  - **Shared files with others' hunks (stage by hunk):** `internal/server/middleware.go` (CSP `ws://127.0.0.1:*` + BLD-029 global guard), `routes.go`, `route_capabilities.go`, `handlers_core_doc_engine.go`, `public/app.js` (`refusedListRead`; label and number-format caches for 162.5), `public/shortcuts.js` (162.5 search-index cache; 202.2 and 200.2 hunks are other sessions'), `engines/public_api_credentials.go` and `oauth_client_credentials.go` (BLD-028: keys and tokens refused for a client not serving), `public/view-pim.js` (media thumbnails; another session's media-exceptions tab at l.8), `public/view-finance.js`, `docs/brain/brain.map.json` (claims the 11 files the 199/201 bullet named), the guide parts and `docs/security/browser-boundary-inventory.md`.
  - **Checks:** per item in `micro_checklist.md`; every test through the shared lock on scratch databases with `-p 1`; build and vet of `engines`, `internal/server`, `cmd/tenantctl`; Guide flows / flows2 / flows3 / learn / connected / connected_more green locally.
  - **Also:** the stage50_12 invoice migration is now `migrations_stage50_12_saas_tax_invoice.sql`, which sorts after the subscriptions file (fresh-DB migrate passed, 473 applied), so the hand-patched order on `custom_erp_inspect` noted below is no longer needed for a fresh DB. `update-docs` draws the code map before it publishes a new Knowledge Center page, so adding an article needs two runs before `-Check` is clean. The archive pass takes `--only=<ids>` and repoints relative links.
  - **Left:** the owner checklist; the release regenerates `attack_surface.json` and `page-inventory.md`. Scratch databases from this ask: the owner runs `~/erp-scratch-archive/2026-11-15/drop-scratch-dbs-ask193.ps1` (owner checklist §7).
- **2026-11-15 OMS picks and packs by typed SKU without a barcode; Guide's PO → GRN → transfer → bill chain on app (asks 197 and 198, ledger §377). NOT COMMITTED, no push, not deployed.** 197.1-197.3, 198.1 and 198.2 are Done.
  - **New files:** `engines/oms_pick_without_barcode_test.go` and `internal/server/oms_pick_queue_store_test.go` (both untracked).
  - **Changed:** `engines/fulfillment_pickpack.go`, `internal/server/handlers_wms.go`, `internal/server/handlers_order_mutations.go` (197.3: `handlePickQueue` follows the top-bar store), `public/oms.css`, `public/view-oms-steps.js`, `docs/guides/user-guide-reports-and-orders.md`, and one sentence each in `user-guide-getting-started.md` and `admin-guide-foundation-users-and-settings.md`. Other sessions have hunks in `view-oms-steps.js` and all three guides, so stage them by hunk.
  - **Checks:** `TestPickPackWithoutBarcodeByTypedSKUAndQty`, `TestPickQueueFollowsTopBarStore`, plus the local Guide suite `omsnobarcode` (20 checks, no finding). No migration, no new route and no data change. The flows suite's Stock on Hand and ATS findings on the scratch DB predate this work: they already show in today's 12:16 and 13:50 runs.
  - **Guide suites (outside the repo):** `grnbill`, `grnchain` and `omsnobarcode`, and `flows.js` gained a receive-only return. The 198.2 run on app wrote only to the Guide test stores GUIDE-TS and GUIDE-TS2.
  - **Scratch DB `custom_erp_inspect`:** carries ~477 stale Pending fulfilment tasks from earlier runs. Its migration order was patched by hand for another session's stage50_12 files, which sort gst before subscriptions. Tell the ask 193 owner.
  - **Dev-server state:** nothing left running; Guide stops its own server.
- **2026-11-15 Item alias on stickers, item name from Category + Type, combinations named by their item name (asks 199 and 201, ledger §376). NOT COMMITTED, no push, not deployed.** 199.1 and 201.1-201.3 are Done and archived; they reach users at the next release. 199.2 (rename minn's items through Guide) waits for the owner's OK to write; 201.4 needs a decision.
  - **New files:** `engines/item_alias.go`, `engines/item_alias_test.go`, `db/migrations/migrations_stage201_1_item_alias.sql` (additive: no data change at release, aliases are made on save or on the fly at print).
  - **Changed:** `engines/` `settings_definitions.go`, `item_setup.go`, `stickers.go`, `data_quality_queue.go`, `master_data_validation.go`, `master_duplicates.go` and their tests; `internal/server/` `handlers_item_setup.go`, `bulk_purchase.go` (+ test), `error_catalog_extensions.go` (PIM-9363) and `handlers_core_doc_engine.go` (others' hunks in it too, so stage by hunk); `public/` `view-item-form.js`, `view-documents.js`, `view-procurement.js`, `sticker-engine.js`, `sticker-studio.js`; four guide parts and the regenerated `error-codes.md`.
  - **Checks:** the tests named in §376, build and vet of `engines` and `internal/server`, gofmt; the Item Name and Item forms in Edge on a scratch server (stopped; its four test rows deleted). `graphify update .` done.
  - **Brain redrawn:** `brain.map.json` claims `engines/item_alias.go` and `internal/server/security_automation_key.go` (195.1); `update-brain.ps1` passed at 100% coverage.
- **2026-11-15 PrintBridge installs once and then updates itself (ask 203, ledger §375). NOT COMMITTED, no push, not deployed.** Agent and installer 0.3.0. 203.2, 203.4 and 203.7 are Done. 203.1, 203.3 and 203.5 wait for a check on app after the release. 203.6 (code-signing certificate) and 203.8 (own repository) need the owner.
  - **Areas changed (ERP):**
    - `public/print-routing.js` (`offerInstall`). Another session's 184.1 `TSPL_RE` hunk is in the same file, so stage by hunk.
    - `public/qz-print.js` (ws on 127.0.0.1 first, `askUpdate`, `updateStatus`) and `public/index.html` (`qz-print.js?v=3`).
    - `public/view-printing.js` (`qzRefreshStatus`).
    - `public/downloads/`: `printbridge-setup.exe` 0.3.0, `printbridge-version.txt`, and the new untracked `printbridge-update.json`. All three ship together.
    - Release gate G4d in `release/scripts/Invoke-Release.ps1` and `release/README.md`. Other sessions have uncommitted work in `release/`, so stage by hunk.
    - The user guide (labels and printing), the admin guide (items, printing and stock) and `qz-printing-setup.md`.
  - **Areas changed (`../PrintBridge`, not in git):**
    - The new `agent/internal/update/` with its test.
    - `agent/internal/agent/handlers.go` (`BuildVersion` 0.3.0, `networking.hostname`, `printbridge.update`).
    - `agent/cmd/pbsign`.
    - `installer/build.ps1` (reads `BuildVersion` and signs) and `installer/printbridge.nsi` (silent, no root certificate, uninstall clears `update\`).
    - `client/printbridge.js`, `README.md`, `INSTALL.md` and `docs/PROTOCOL.md`.
  - **Release key:** `%USERPROFILE%\.printbridge\release-key.pem` signs every update. It is never put in a repo. If it is lost, installed agents accept no further update until each one is installed again by hand.
  - **Checks:**
    - `gofmt`, plus `go vet` and `go test -p 1` of `internal/update` and `internal/agent`.
    - `node --check` passes.
    - The real `print-routing.js` offer passed in Edge.
    - The shipped installer, run by hand over 0.2.0, closed itself in 5 s and kept every setting.
    - Live self-update from 0.3.0 to a 0.3.1 test build took 11 s. The laptop is back on 0.3.0 and the test build is deleted.
  - **At release:** G4d checks that the served installer, version file and update file agree. PCs on agents before 0.3.0 need one manual install.
  - **Dev-server state:** nothing left running. A Node test server on :8291 was stopped. Its `127.0.0.1:8291` line was removed from this laptop's `erp-url.txt`, and the agent was restarted.
- **2026-11-15 A short code for every menu screen, and a switch to turn them off (ask 202, ledger §374). NOT COMMITTED, no push, not deployed.** 202.1-202.3 Done: 167 codes (116 new, the old 52 unchanged); Configuration ▸ Navigation ▸ Short codes (`navigation.short_codes`, on by default) turns them off and back on for a client.
  - **Areas changed:** `public/shortcut-codes.json`, `public/shortcuts.js` (`codesOn()` and its four uses), `engines/role_landing.go` (setting, `NavInfo.NoCodes`) and `engines/role_landing_test.go` (appended test), `internal/server/shortcuts_test.go`, the user guide §3A.3 (`user-guide-getting-started.md`), `docs/kb/getting-started/keyboard-shortcuts.md` and the KB HTML regenerated from it, `admin-guide-foundation-users-and-settings.md` (one bullet after the 47.14.5 one), the trackers, the ledger and this entry.
  - **Checks:** `TestStage139ShortCodes`, `TestNavForRole*`, `TestSettingsRegistry` (dev DB :5490), build and vet of `engines` and `internal/server`, `node --check`. Live on a scratch server (stopped): nine codes opened their screens; off and on again through the real settings API. The full suite was not run. The dev DB's `default` tenant now holds a `navigation.short_codes = true` row (same as the default).
  - **Rule for later:** a shipped code is never renamed or moved to another screen. A new screen gets a new code, or the test fails.
  - **Concurrent-session risks (stage by hunk):** `public/shortcuts.js` also holds ask 192's 162.5 hunks (`fillSetupMenuRows`, the `findButton` reorder); the user guide part and the admin guide part hold other sessions' hunks.
- **2026-11-15 Sticker printer on app, Line up / Reprint, Guide's hidden key (asks 183, 184, 189, 194, 195; ledger §373). NOT COMMITTED, no push, not deployed.** 183.1 and 189.1 Done on app (LD0801, PDF, Shift right −0.5, gap 3.2, rotate off; the 10:38 photo is even within 0.3 mm). 184.1, 194.1-194.3 and 195.1-195.2 Built, not verified (each needs a release for its live check). 194.1 / 194.2 already clicked through on app's data with Guide's new `lineup` suite (tree files served to its browser only, nothing printed); that run found and fixed Line up drawing the built-in 50 x 25 page when Roll loaded is None. 183.2 / 154.4 Done: minn set like app through app's Control Panel Open link on the owner's "write to minn" (read back); Guide opens minn only `-Via app` (196.1).
  - **Areas changed:** new `internal/server/security_automation_key.go` and `_test.go`; hunks in `internal/server/middleware.go` (`takeAutomationKey` after `WithRequestSource`, the limit lift after the signed-in default, `X-ERP-Automated` in the CORS allow-headers), `internal/server/handlers_auth.go` (the refusal in `handleLogin`), `public/app.js` (login fetch header, around line 3549), `public/store/store.js` (`api()`), `public/print-routing.js` (184.1), `public/sticker-engine.js` (`printLineUp`), `public/view-printing.js` (Line up, Reprint); `deploy/erp.env.example`; `docs/guides/admin-guide-security-and-operations.md` (C.4a), `admin-guide-items-printing-and-stock.md`, `user-guide-labels-and-printing.md`, `qz-printing-setup.md`. Outside the repo: `../PrintBridge` agent (189.1, rebuilt on this PC) and `~/dev-tools/guide` (195.2).
  - **At deploy:** nothing changes until `ERP_AUTOMATION_KEY_SHA256` is set. The owner runs `pwsh ~/dev-tools/guide/Guide.ps1 -NewKey -Site app` (and `minn`) and the printed line goes into that host's env file, with the owner's OK. Without the key line, Guide paces as before.
  - **Checks:** `TestStage195*` (3) and the existing rate-limit / login / middleware tests pass on the dev DB (:5490); `go build` / `go vet ./internal/server/` clean. The full suite was not run.
  - **Concurrent-session risks (stage by hunk):** `middleware.go`, `handlers_auth.go`, `app.js` and the guides hold other sessions' hunks.
  - **Left:** 183.2 (minn: the same records; Guide `act` params in this session's scratchpad, preview first); a new PrintBridge installer for other PCs.
- **2026-11-15 "Build all recommended" (ask 192, ledger §372), on top of ask 191 below. NOT COMMITTED, no push, not deployed.** Built the approved answers: 47.10 search on very large masters (exact id / code / barcode first, a `pg_trgm` index, identity-field search over 100k rows, estimated totals over 10k), 191.1 (the "Test Panic Recovery" button is off Activity Log; `/api/v1/debug/panic` stays for dev and test), the 162.5 cold-load fixes (hidden module flyouts built on first open, menu restore after first paint, deep-link title) and three more connected-organisation journeys for 135.8 (Recover, Fulfil, Close).
  - **Areas changed:** `db/migrations/migrations_stage47_10_master_search_trgm.sql` (new); `internal/server/handlers_core_list_search.go` and its test (new); `internal/server/handlers_core_doc_engine.go` (the 47.10 hunk in the list handler, around line 587); `internal/server/middleware.go` (`X-Total-Count-Estimated` exposed, around line 1110); `public/view-documents.js` (pager "about N"), `public/app.js` and `public/shortcuts.js` (162.5), `public/view-admin.js` (191.1); `user-guide-getting-started.md`, `admin-guide-security-and-operations.md`, `admin-sop.md`, `uat-checklist.md`; the trackers (47.10 and 191.1 archived), the ledger and this file. Outside the repo: `~/dev-tools/guide/suites/connected.js` and its README.
  - **Release must regenerate:** nothing new from this ask (no new route).
  - **At deploy:** the 47.10 migration needs `pg_trgm` (it skips with a notice if the host lacks it) and builds a GIN index on each tenant's `documents` without `CONCURRENTLY`, so writes to `documents` wait while it builds. Production data is small, so this is seconds. New tenants get the index through `LIKE tenant_default.documents INCLUDING ALL`.
  - **Checks:** `internal/server` tests for 47.10 pass on `custom_erp_t191`. 1m Items: search 23-36 s down to 225-509 ms p95 (over the 150 ms budget, recorded and closed as approved); a common word answers "about N" instead of counting. 162.5: the Edge check 12/12; gate re-run within noise of ask 191 (throttled and phone budgets still missed, every figure under its ceiling). 135.8: Recover, Fulfil and Close pass on `custom_erp_s1358d`; three suite fixes, no app bugs. The full suite was not run.
  - **Decisions:** all as the owner approved ("Build all recommended"). Observed, not filed: an approved journal voucher that cannot post (closed period) still answers 200 on approve and stays Approved.
  - **Dev-server state:** every scratch server stopped. Scratch databases for the owner's drop script: `custom_erp_scratch191`, `custom_erp_t191`, `custom_erp_s1358b`, `custom_erp_s1358c`, `custom_erp_s1358d`.
  - **Concurrent-session risks (stage by hunk):** `handlers_core_doc_engine.go` (49.3.4 / 57.26 hunks), `middleware.go` (Razorpay / CSP), `view-documents.js` (MediaRuleException / DeclarationPack / 57.26(g)), `app.js` and the guides hold other sessions' uncommitted hunks.
  - **Left:** 162.5 (paint the setup card from its last-known state; trace the long tasks in a view switch; re-run on a quiet machine) and 50.14 behind it; 135.8 Enrich, People / service, Restore, the interrupted variant and the Make / Move exception cases. Blocked unchanged: 47.9.3, 47.9.1, 55.11, 74.U.2.
- **2026-11-15 "start building all next 40 itms" + "all recomennded approved" (ask 191, ledger §371), on top of ask 185 below. NOT COMMITTED, no push, not deployed.** Built the 37 buildable items of ask 191 (list in the ask 191 run header and §371): the Cashier and Store Manager Customer migration (55.5a), CI Test step that always runs (50.11), `jobs.view` / `jobs.manage` (49.3.1) and the Monitoring menu per role, strict `style-src 'self'` CSP (49.4.3), Settings > API access (55.21), `engines/jobrunner_replica_claim_test.go` (47.11), the print harness paths (50.14), 48.7.6 guide-image move to the archive, the Help drawer fix (48.7.7), a GST odd-paisa fix, the offline till fix and three cold-load fixes in `public/app.js` (162.5). Found: 191.1, the "Test Panic Recovery" button on Activity Log (needs a decision). 47.10 needs a decision on search over very large masters.
  - **Areas changed:** `db/migrations/` (the 55.5a migration); `engines/` (job capabilities, GST rounding, jobrunner test); `internal/server/` (route capabilities, CSP middleware); `public/` (Settings > API access page, Monitoring menu, Help drawer, offline till; `app.js` cold-load fixes for 162.5); `docs/qa/bld041-print-paths.cjs`; `.github/workflows/ci.yml`; `docs/security/` (`browser-boundary-inventory.md`, `verification-standards-matrix.md`, `pentest-scope.md`); the admin and user guides; `docs/brain/brain.map.json` (claims `engines/query_explain.go`); `docs/governance/document-register.json` (only the four 48.7.6 entries, now at their archive paths); `docs/kb/reference/public-api-authentication.md` maps the `api-access` screen; the trackers (40 closed items archived), the ledger and this file. `docs/guides/img/approvals.png`, `record-list.png`, `trial-balance.png` and `screenshot-manifest.md` are deleted (moved to the archive, 48.7.6).
  - **Release must regenerate:** `attack_surface.json` (G6), `command-catalog.md` and `page-inventory.md`.
  - **At deploy:** the 55.5a migration conflicts with what the app and minn clients have for Cashier and Store Manager Customer Create/Update; look before the Deploy YES. 49.3.1 removes job access from custom roles unless they are granted `jobs.view` / `jobs.manage`.
  - **Checks:** Targeted Go tests on a fresh database (`custom_erp_t191`): `internal/server` pass, `engines` 53 pass / 3 skip (scheduler lock). 49.4.3: 397-screen report-only crawl with 0 `style-src` violations and 0 page errors; BLD-041 35/35 with the strict CSP enforced. 55.3 Done (connected suite set up a new organisation through the UI); 55.11 Blocked (real participants); 162.5 still Building (cold long tasks at 4x 448-826 to 380-453 ms; throttled / phone budgets still missed, ceilings held); 135.8 Setup, Buy, Move, Sell and Make walked, the rest not started; 47.10 Needs decision (1m Items: lists fine, search 14-36 s). Of the 37: 22 Done, 7 waiting on checks, 4 Blocked, 3 Building, 1 decision; 40 closed items archived, 242 open. `TestReportControlTotalsTieDetailSummaryAndLedger` fails in other sessions' files, not this ask's. The full suite was not run. `go test ./internal/kb/` passes; brain map 100% claimed; `update-docs` passes. `doclint -strict` has 9 findings, none from this ask: other sessions' unregistered files (`docs/architecture/pos-proposed-design.md`, the three new handbooks `privacy-approvals`, `purchase-contracts-and-service-entries`, `supplier-portal` and their HTML), the archive link above and the old handover-length warning - release G7 needs them registered (`go run ./cmd/doclint -write-register`) once those sessions finish.
  - **Decisions:** 39.18 and the 47.12.4 owner question were taken by the 5-minute ask timeout (recommended option), not by the owner.
  - **Dev-server state:** a scratch server on port 8731 against `custom_erp_scratch191`; stopped at the end of the task. Its 1,000,000 FX1M Items from the 47.10 run were deleted. It holds manager1 on store B41-ST, users `aud191` and `adm191`, jobs JOB-191-5521 and JOB-191-5521R and a BLD-041 seed (MWH3WYLJ). A drop script for the scratch databases is left for the owner to run.
  - **Concurrent-session risks (stage by hunk):** other sessions have uncommitted hunks in `internal/server/middleware.go` (Razorpay, around lines 499 and 666), `public/view-pos-customer.js` (185.1), `docs/guides/admin-guide-developer-and-glossary.md` (around line 150) and `docs/kb/reference/public-api-authentication.md` (a whole-file CRLF change). A broken link at `docs/archive/micro_checklist_closed_stages.md:3323` is another session's work.
  - **Left:** 47.9.3 (`pg_stat_statements` needs the production host) and 47.9.1 (needs a dedicated box) are Blocked; 76.10 and 149.2 need owner or production checks.
- **2026-11-14 "All done or anything pending. build pending ones" (session erp-8a, which was erp-fa before a restart; ask 185, ledger §370), on top of ask 179 below. NOT COMMITTED, not deployed.** Built 185.1 (till UPI QR), 185.2 (BharatPe / PhonePe / Paytm statements), 185.3 (Razorpay payment links), 47.12.5 and 47.13.6.
  - **New files (all `??`):** `db/migrations/` `migrations_stage185_1_upi_tender_qr.sql`, `migrations_stage185_2_upi_statement_rows.sql`, `migrations_stage47_13_6_declaration_packs.sql`; `engines/` `pos_upi_qr.go`, `acquirer_upi_statements.go`, `finance_payment_links.go`, `pim_declaration_packs.go` (+ `.json`), `approval_own_decisions.go` and each one's `_test.go`; `internal/server/handlers_finance_payment_links.go`.
  - **My hunks in shared files:** `engines/` `approval.go` (APPROV-0842, the erp-8a hunks the entry below names), `doctype.go`, `import.go`, `acquirer_settlement.go`, `pos_tender_master.go`, `pim_publish.go`, `settings_definitions.go`, `connector_disconnect.go`, `sandbox.go` and `_test.go`, `tenant_reset.go` (`pos_till_series`, 76.10); `internal/server/` `routes.go`, `route_capabilities.go`, `middleware.go`, `error_catalog_extensions.go` (and the two regenerated KB error-code pages); `public/` `app.js` (DeclarationPack meta), `view-pos.js`, `view-pos-customer.js`, `view-inline.css`, `view-finance.js`, `view-admin.js`, `view-documents.js`; the guides, the trackers, the ledger and this file. Stage shared files by hunk.
  - **Checks:** each item's engine test and the server route / capability / inline-style tests, vet; an Edge walk on the scratch server (:8177, `custom_erp_fa_ui`), no page errors. The full suite was not run. `attack_surface.json` is left to release G6 (four new routes: pay link, Razorpay keys GET/POST, webhook).
  - **Left:** 185.3's live run needs the owner's Razorpay test keys; the other open items are in the ask 185 run header. Scratch server stopped. `custom_erp_fa` and `custom_erp_fa_ui` are still in the owner's drop script below.
- **2026-11-14 "Build above pending all approved" (session erp-0f; ask 186, ledger §369), on top of asks 178 and 175 (ledger §363, §359). NOT COMMITTED, not deployed.** Built 92.4, 186.1, 186.2, 57.26's walk, part of 103.4.1 and 186.3; what stays blocked is in the ask 186 run header. The ask 178 and 175 file lists moved verbatim to the [archive](archive/handovers/ai-handover-2026-10-26.md).
  - **Ask 186, mine:** `engines/approval.go` (APPROV-0841 lines only; erp-8a owns the APPROV-0842 hunks), one row in `internal/server/error_catalog_extensions.go`, `engines/client_removal_test.go`, `item_master_test.go`, `security_baseline_test.go` and 103.4.1's deletions (listed in the item); `internal/server/handlers_auth.go` (`resetLinkBase`); `public/index.html` (forgot and reset forms), `public/app.js` (forgot/reset functions and the `renderView` focus restore; stage by hunk), `public/view-oms-steps.js` (Ship loads `status=Packed`); new KB articles `docs/kb/module-handbooks/privacy-approvals.md`, `supplier-portal.md`, `purchase-contracts-and-service-entries.md`; KB review dates, plan front matter, the guides, the trackers, the ledger and this file.
  - **Checks:** targeted tests on `custom_erp_70`; a Playwright walk on a scratch server (`custom_erp_inspect`) of forgot password end to end and 57.26 (e), (g), (h), no page errors. `doclint -strict` leaves only six `unregistered` findings for the new articles, which the release's register rewrite clears. The brain redraw stops on four other sessions' unclaimed `engines/` files; the release files them.
  - **Release:** migration `178_1` changes data (Deploy needs YES). Shared files are staged hunk by hunk. Scratch DB `custom_erp_70` to drop.
- **2026-11-14 "Clean up" + "Build half and give me a prompt to build rest 50%(now)", the first half (session erp-e0; ask 187, ledger §368). NOT COMMITTED, not deployed.**
  - **New files (`??`):** `release/guard-tests.txt`, `release/scripts/Invoke-GuardTests.ps1`.
  - **My hunks:**
    - Release scripts: `release/scripts/` `Invoke-Release.ps1` (G10's guards first, the G6b ack gate), `Invoke-Deploy.ps1` (D4b) and `ReleaseCommon.ps1` (`Read-GuardTests`, `Get-CertificateExpiryNote`), plus `release/README.md`.
    - `.gitignore` (`release/security-ack.txt`).
    - `.github/workflows/release.yml`: the `guards` job and the throwaway-database step.
    - `engines/` `extensions.go`, `pim_bulk.go` and `pim_market_harvest_test.go`, and the hook tests (`extensions_test.go`, `extensions_version_test.go`, `outside_calls_test.go`, `bld031_provider_extension_contract_test.go`).
    - `internal/server/handlers_core_doc_engine.go`: the two hook calls only.
    - `extension-sdk/` `README.md` and `hook-payload.schema.json`.
    - The tracker, the ledger and this file.
  - **Commit order:** `release.yml` and the other `??` files in `release/` belong to erp-5c's uncommitted ask 177 (§360), and my release work sits on top of it, so commit them together.
  - **Checks:** build and vet; 27 hook tests, and the guard runner (18/19), on a freshly migrated scratch database. The full suite was not run.
  - **Before the next release:** the guard `TestNoUnreviewedBypassPattern` failed on SB-021's uncommitted `migrations_stage178_1_bootstrap_accounts.sql` (erp-70, §363). Resolved: the owner approved the recommended reviewed bypass entry under ask 191, and it is now in `internal/securityscan/bypass_test.go`. 177.6 was closed under ask 191 as well.
  - **Local data:**
    - The `custom_erp` dev data is cleaned up (73.B.13, 84.5, 73.C.5).
    - `batchc55f` is deprovisioned. Purge request CDR-1794676340011783100-ftmg5tsj waits for a second admin to approve it; then the owner runs `tenantctl purge` and `verify`.
    - The ask179 script's `tenantctl` steps are superseded; its database drop still stands.
  - **Cleanup done:** scratch server, HEAD worktree, the `custom_erp_e0` and `custom_erp_e0g` databases and the temp files are removed.
- **2026-11-14 "Build half and give me a prompt to build rest 50%(now)", the second half (session erp-ba; ask 188, ledger §367). NOT COMMITTED, not deployed.**
  - **My hunks in shared files:** `public/app.js` (`holdSectionWhileLoading`; `fieldSemantic`, `detectFieldFormat`, `isPhoneFieldName` for 188.1); `public/styles.css` (the 50.13 `[aria-busy]` dim rule only); `public/view-warehouse.js` (Yard Board, Warehouse Cockpit loads); `public/view-oms.js` (OMS orders load); `engines/field_semantics.go`, `field_semantics_test.go`, `field_formats.go`; `docs/guides/admin-guide-settings-numbering-and-batches.md` (re-code steps), `user-guide-receiving-and-transfers.md` (one yard sentence); the tracker, the ledger and this file. Other sessions have uncommitted hunks in `app.js` and `styles.css`: stage mine by hunk.
  - **Checks:** `go vet` and the field-format tests in `./engines/` (with `-tags stage47redteam`); `node --check`; a Playwright walk on a scratch server against `custom_erp` without `-migrate`, all checks passing. The full suite was not run.
  - **Left:** the decisions and gates are in ledger §367; 47.15.4, 48.4.5 and 48.7.2/.6/.7 are buildable and not started. Scratch server stopped, scratch files removed.
- **2026-11-14 "Build all untouched next 30 items" in PIM, PROC, INV, PRT, CHN, DATA and CFG (session erp-fa; ask 179, ledger §366). NOT COMMITTED, not deployed.**
  - **New files (all `??`):** `db/migrations/` `migrations_stage55_11_media_rule_exception.sql`, `migrations_stage47_13_5b_master_merge_approval.sql`, `migrations_stage76_10_pos_till_series.sql`; `engines/pos_till_series.go` and `_test.go`; `engines/pim_media_exception_test.go`.
  - **My hunks in shared files:** `engines/` `pim_publish.go` (55.11), `master_merge.go` and `_test.go` (47.13.5), `setup_path.go` and `_test.go` (55.3); `internal/server/` `handlers_master_merge.go`, `handlers_pim_pos_finance.go`, `routes.go`, `route_capabilities.go`, `error_catalog_extensions.go` (POSOFF-1371); `public/` `app.js` (`defaultSelectValue` Draft default, till functions), `view-admin.js` (setup-path badges), `view-pim.js`, `view-documents.js`, `view-reports.js`, `view-pos.js`; the guides `user-guide-master-data.md`, `admin-guide-foundation-users-and-settings.md`, `admin-guide-settings-numbering-and-batches.md`, `admin-guide-developer-and-glossary.md`, `error-codes.md`; the trackers, the ledger and this file.
  - **Checks:** the new tests one at a time; `node --check`; Edge walks on a scratch server (:8177, `custom_erp_fa_ui`), zero console or API errors. The full suite was not run. `attack_surface.json` is left to release G6 (new till-series route).
  - **Left for the owner:** `~/erp-scratch-archive/2026-11-14/ask179-cleanup.ps1` drops `custom_erp_fa` and `custom_erp_fa_ui` and prints the `tenantctl` steps to remove `batchc55f` (73.C.5). Scratch server stopped, its files removed.
- **2026-11-14 "Recent not required in home. Home is for Dashboards Not shortcuts" (ask 180, ledger §362). NOT COMMITTED, not deployed.**
  - **My files:** `public/app.js` (`renderHomeView`, `HOME_QUICK_ACTIONS` comment, `navigateHomeRecent` removed), `public/styles.css` (home tile and Recent rules removed), `docs/guides/user-guide-getting-started.md` (Home section), `docs/guides/uat-checklist.md` (§2), the trackers, the ledger and this file.
  - **Checks:** `node --check`; live Home in headless Edge as admin, manager1 and cashier1 on `custom_erp_inspect` (no tiles, no Recent, zero console errors). No Go code changed. No scratch left.
- **2026-11-14 "Once all pushed and deployed start building all the open items" in FIN, TAX, HR, MFG, ASQ and RPT (session erp-57; ask 176, ledger §361). NOT COMMITTED, not deployed.**
  - **Closed and archived (51 items):** 148.4b, 170.1-170.42 (the ask-170 gaps built, every item verified), 176.1-176.6, 176.8 and the 73.4.8 bundle. 176.7 (finite capacity) waits on measured load. 73.4.5 is Blocked on it.
  - **New files (all `??`):**
    - `engines/` `finance_cash_position.go`, `expense_travel_request.go`, `expense_corporate_card.go`, `service_field_visit.go`, `service_knowledge_base.go` and `quality_calibration.go`, each with `_test.go`;
    - `engines/gstr1_amendments.go` and `gstr1_till_sales.go` (170.35);
    - `db/migrations/` `migrations_stage148_4b_party_control_accounts.sql`, `migrations_stage170_35_gstr1_filed_rows.sql` and `migrations_stage176_2`-`176_6`.
  - **My hunks in shared files:**
    - The posting and tie-out path: `posting_groups.go`, `posting_profiles.go`, `receivables_ledger_reconciliation.go`, `customer_receipt.go`, `control_totals.go`, `payables_subledger.go`, `period_close.go` and `po_terms.go`.
    - The 170 gaps: `gl_source_line.go`, `asset_lease.go`, `asset_cwip.go`, `asset_adjustment.go`, `asset_lifecycle.go`, `gstr1_json.go`, `gstr2b_reconcile.go`, `data_classification.go`, `hr_certificates.go` and `print_shapes.go`, with their tests.
    - The wiring: `transactional_validation.go`, `scope_policy.go`, `supply_state.go`, `document_numbering.go`, `import.go`, `report_registry.go` (a report's own GLOBAL-0002 is now 422, not REPORT-0162 503) and `report_catalog_spec_generated.go`.
    - `internal/server/error_catalog_extensions.go`.
    - The two finance guides; `docs/brain/brain.map.json` (8 files claimed); the trackers, the ledger and this file.
    - The archive entries name each item's files.
  - **Checks:**
    - 98 targeted tests on scratch DB `custom_erp_57_t` (:5490), run one at a time.
    - Guide forms and act runs on the working tree.
    - The full suite was not run.
  - **Release notes:**
    - Migrations 148_4b, 170_35 and 176_2-176_6 are additive.
    - New reports: R5173-R5175 and R7971.
    - Number series: TRQ, SV and CAL.
  - **Scratch DB to drop:** `custom_erp_57_t`. Use the script in the session's final message.
- Older §6 entries (up to the 0.24.0 release on 2026-11-14) are kept verbatim, newest first, in [archive/handovers/ai-handover-2026-10-26.md](archive/handovers/ai-handover-2026-10-26.md) (moved 2026-11-12, Stage 134.9, and 2026-11-14, item 186.1, so this page fits its 150-line budget).
