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

Generated from `docs/project_ledger.md`'s own Stage sections - **273** entries as of this build. Each excerpt is that section's own opening paragraph, not a rewritten summary, so it reads like an engineering build log because that is what it is. For the full detail behind any entry, including what was verified and how, read the ledger itself.

Newest first. Earlier entries continue on:

- [Part 2](release-notes-2.md) - 2026-08-19 to 2026-10-18
- [Part 3](release-notes-3.md) - 2026-07-27 to 2026-08-19

## 2026-11-16

**Every transaction screen has its own lesson, and every module a picture** *(KB handbooks + KB build + guides + Guide suites, ask 217; NOT COMMITTED)*

**Best-practice POS: B2B bill, GST cash/PAN rules, three discounts, every standard offer, Sales History, online bills and returns at the till** *(code + migration + tests + guides + Guide suites, ask 215; NOT COMMITTED)*

**PrintBridge in its own public repository, the build signs it once a certificate exists, and Test alignment on paper** *(PrintBridge repo + build script + real print, ask 218; ERP docs NOT COMMITTED)*

**Data Healer rule in the constitution, and Guide's key set live** *(constitution + PR template + production config, ask 216; NOT COMMITTED)*

## 2026-11-15

**Screens hold still when you type or click - every screen walked, all three roles** *(front end + guide + Guide suite, ask 200; the later fixes NOT COMMITTED)*

**Switch paper, template and printer for one print - at Print, the till and Reprint; profiles by B2B / B2C and user** *(code + migration + tests + guides, ask 213; NOT COMMITTED)*

**Data Healer > Past names: change an item's name on documents already made, without SQL** *(code + tests + guides, ask 211 with 201.4; NOT COMMITTED)*

**PrintBridge: "did not print" in seconds, no wake-box, and a security review - agent 0.3.2** *(ERP + PrintBridge agent + tests + guides, asks 209 and 212; NOT COMMITTED)*

**Help opens the Knowledge Center at your screen's lesson: What, Why, Use case, How, with pictures** *(front end + KB + guides, ask 210; NOT COMMITTED)*

**Data Healer: one-time fixes old records catch up through, starting with Item aliases** *(code + migration + tests + guides, ask 207; NOT COMMITTED)*

**System Errors becomes a fix list: real faults only, one row per fault, Copy for fix and Resolve** *(code + migration + tests + guides, ask 206; NOT COMMITTED)*

**Each screen's short code beside its name in the sidebar menus** *(front end + guides, ask 208; NOT COMMITTED)*

**API access for every module, limited by module access - research and plan** *(plan only, ask 205; NOT COMMITTED)*

**"Push all and deploy all" - release 0.25.0 once every session was done, deployed to app and minn** *(release + deploy + pipeline fixes, ask 204)*

**"Build everything. All approved" - every buildable open item: SaaS packages and billing, passkeys, tax rules from official sources, Unicommerce, the role checks Guide found, and the owner checklist** *(code + tests + guides + docs, ask 193; shipped in 0.25.0, §379)*

**OMS picks and packs by typed SKU without a barcode; Guide runs PO → GRN → transfer → bill on app** *(code + tests + guide + Guide suites, asks 197 and 198; released in 0.25.0, 53af64d)*

**A short item alias on stickers, the item name from Category + Type, and combinations named by their item name** *(code + migration + tests + guides, asks 199 and 201; NOT COMMITTED)*

**A short code for every menu screen, and a switch to turn codes off for a client** *(code + tests + guides, ask 202; released in 0.25.0, 7c43c56)*

**"Build all recommended" - search on very large masters, the panic button off Activity Log, the cold-load gate re-run and three more connected-organisation journeys** *(code + tests + guides + docs, ask 192; NOT COMMITTED)*

**"start building all next 40 itms" + "all recomennded approved" - 37 buildable items: the Customer-create migration, CI that always tests, jobs capabilities, a strict style CSP, a second-replica job test, an API access page, and the doc and walk items** *(code + tests + guides + docs, ask 191; NOT COMMITTED)*

## 2026-11-14

**"Build above pending all approved" - what ask 178 left open: Forgot password from the sign-in screen, a second-admin document's own APPROV code, the ten doclint findings that stop release G7, 57.26's browser walk, and the approved dead-code list where its files were free** *(code + tests + guides + KB, session erp-0f, ask 186; NOT COMMITTED)*

**"Clean up" + "Build half and give me a prompt to build rest 50%(now)" - the first half of ask 187: the local dev data cleaned up, guard tests first, a faster test database on GitHub, a certificate check at deploy, a security-change acknowledgement at release, and every hook naming who it acts for** *(release scripts + CI + code + tests + SDK, session erp-e0, ask 187; NOT COMMITTED)*

**"Build half and give me a prompt to build rest 50%(now)" - the second half of ask 187: QA, UI, Docs and TEN items sorted into buildable, gated and decision; the re-code tool's guide paragraph, sections that keep their rows while they reload, a camelCase field-name fix, and the open decisions with a recommended answer** *(code + tests + guides, session erp-ba, ask 188; NOT COMMITTED)*

**"Build all untouched next 30 items" - PIM, PROC, INV, PRT, CHN, DATA and CFG: a media rule exception that a second person approves, master merges through approval, a registers step on the setup path, till number series for offline bills, one badge per setup-path step, and three decisions with a recommended answer** *(code + tests + migrations + guides, session erp-fa, ask 179; NOT COMMITTED)*

**"C is getting filled. See what we can clean up" - C: from 22.6 GB to 48.7 GB free** *(machine clean-up, no code, ask 181)*

**"I got the new printer, guide test in now" - the new label printer does not take TSPL** *(test only, no code, ask 182)*

**"all recommended" / "Build all don't ask/wait for approval" / "Complete al" - ask 175's decision batch built as recommended: the shipped bootstrap accounts closed, ten screen and save fixes, approval delegation and reminders for every document, the capability checks filed, and every stock ledger line keeping its UOM, cost, price and tax** *(code + tests + migrations + guides, session erp-70, ask 178; NOT COMMITTED)*

Detail is in the tracker under each item id and the ask 178 run header.

**"Recent not required in home. Home is for Dashboards Not shortcuts" - Home loses its Recent list and its quick-action tiles** *(code + guides, ask 180; NOT COMMITTED)*

Detail is in the tracker: the ask 180 run header in the Asks log and 180.1 in *RPT › Dashboards & home*.

**"Once all pushed and deployed start building all the open items" - FIN, TAX, HR, MFG, ASQ and RPT: the gaps left on ask 170, receivable and payable accounts by party group, a cash position, travel requests, corporate cards, field visits, help articles and instrument calibration, and every ask-170 item verified** *(code + tests + guides, session erp-57, ask 176; NOT COMMITTED)*

Detail is in the tracker under each item id (now in the archive) and the ask 176 run header.

**"Why it is taking so much time?" / "i need is 1 min all should be done" - the release suite moves to GitHub's machines and the click's own gates are cut** *(release scripts + CI + guides, session erp-5c, ask 177; NOT COMMITTED)*

Detail is in the tracker: 177.1-177.7 in *OPS › Release pipeline & gates* and the ask 177 run header.

**"Once al push and deployed start build" / "All open items" - TEN, SEC, PLAT, UI, OPS, QA and DOC: what 0.24.0 proves is closed, one decision batch with recommended answers, hidden fields kept off prints and filters, a consent-withdrawal test, and the budgets re-run on a quiet machine** *(code + tests + guides, session erp-70, ask 175; NOT COMMITTED)*

Detail is in the tracker under each item id and the ask 175 run header.

**"push & deploy all" - release 0.24.0 (bfd0ff0) and its deploy to app and minn** *(release + deploy, session erp-5c, ask 174)*

Detail is in the tracker: 174.1 in *OPS › Release pipeline & gates* and the ask 174 run header.

**"Start building next 20 item. Include from prev which was left in half way" - eight decisions built as recommended (a read-only mode, an encrypted evidence pack, buyer e-mails dropped after delivery, no takeover of an unrecorded command claim, the store cut on two old reports and the payroll export), the loyalty spend served by an index, PO and transfer-in line upload, security friction measured, a privacy-requests screen, and two more replayed commands in the E8 journey** *(code + tests + guides, session erp-2b, ask 173; NOT COMMITTED)*

Detail is in the tracker: the ask 173 run header in the Asks log, and each item named below in its sub-module.

## 2026-11-13

**"Start buildng all this. Carfully build I am building parallely other things" - every open FIN, TAX, HR, MFG, ASQ, RPT and PRT item: line-level ledger rows, bank part-matching and files, leases, CWIP, impairment and revaluation, maintenance cost and meters, production confirmations, scrap and WIP costing, lot genealogy, AQL sampling and supplier SCARs, positions, recruitment, exits, disciplinary cases, certificates, calibration, slab TDS / LWF / ECR / 24Q, benefits, salary revisions, a manager's team view, people analytics, holiday calendars, expense limits, staff advances, timesheets, milestones, project revenue, service SLAs, GSTR-1 / 2B files, export under LUT, retention policies, staff and vendor erasure, a masked sandbox copy, a data-quality queue and import load order** *(code + tests + guides, session erp-e9 / erp-d7, ask 170; NOT COMMITTED)*

Detail is in the tracker: the ask 170 run header in the Asks log and 170.1-170.43 in their sub-modules (FIN, TAX, HR, MFG, Assets, Quality, Data).

**"Use in optimal way where laptop is not getted heated up" / "this first priciple for building in the project" - building without overloading the machine becomes the constitution's first principle** *(docs + local settings only, session erp-71; NOT COMMITTED)*

Detail is in the tracker: the ask 172 run header in the Asks log and 172.1 in *OPS › Repository & developer tooling*.

**"Once all built mark done and keep in archive list" / "Clean up all temp file for the project" - the first archive pass, a disk clean-up, and both as rules after every task** *(docs + local machine only, session erp-0b; NOT COMMITTED)*

Detail is in the tracker: the ask 171 run header in the Asks log, 171.1-171.2 in *DOC › Engineering docs & trackers*, 171.3-171.5 in *OPS › Repository & developer tooling*.

**"Complete all this 3" - every open PIM, PROC and INV item: contracts and service POs, supplier onboarding and a supplier portal, product lifecycle, range, markdowns, merge, dated changes, listing drift, category attribute rules, the four procurement decisions, a Myntra price harvester, a photo copy into the image drive, and nine walk-throughs** *(code + guides, session erp-0b; NOT COMMITTED)*

Detail is in the tracker: the ask 168 run header in the Asks log, and each item in *PIM*, *PROC* and *INV*.

**"build all print modules open items" - bin labels through sticker templates, Sales Order as a sticker source, GRN quantity headings that no longer break, and the print items that only needed a live check walked on a scratch server** *(code + tests + guides, ask 167; NOT COMMITTED)*

What changes for existing clients: the Bin list and the Bin Generator have **Print labels**, which opens a dialog (template, copies, Print to the sticker printer, **A4 sheet** for the old print window) with a built-in *Bin label 50 × 25*; **Sales Order** is in the sticker screen's Module list; the built-in GRN print's quantity headings are 10 wide (the table total is still 114). No migration, no new route.

## 2026-11-12

**"Once deploymeent is done, Start next 20 items to build" - a stock ledger key index, cached setup checks, the synthetic client's PO step in batches and a 100k end-to-end run, a smaller `app.js`, reason-code tabs, the last three strict record types, journal voucher lines checked on every save, market-specific product values, a store move from the Users screen, favourites for every role, notification clock badges, erasure that reaches files and saved views, go-live evidence that goes stale, a replay journey, both guides split into parts** *(code + tests + guides, session erp-10, ask 166; NOT COMMITTED)*

What changes for existing clients: the Users screen says when a home-store move went for approval; the Setup reason-code list has category tabs; Security Events shows Due / Overdue on each notification decision and links the incident runbook; a role made in Setup can keep favourites; SetOffVoucher, StewardshipChange and ImportBatch refuse a status move with no rule; a JournalVoucher saved or imported through the generic route is refused when its lines do not balance (FIN-0844) and its total is set from its lines; an erasure also deletes the person's attachment files and saved views; go-live integration evidence goes stale when a destination is added or switched off. The admin Home card's Finance checks can take up to a minute to show a posting change. Both guides are now an index plus part files.

**"all recommended" - four decisions built: strict transitions for the last eight approval / WMS / service record types, a High-severity reason that holds a purchase return or a transfer loss for approval, signed public image addresses for marketplace feeds, a Privacy approver role** *(code + tests + guides, session erp-4c, ask 165; committed in 1c026d1, release 0.23.0)*

What changes for existing clients: PasswordResetRequest, SupplierSubmission, Appraisal, Grievance, LPN, SortSlot, ServiceTicket and Appointment refuse a status move with no rule (GLOBAL-0019), and rejecting one of the four approval records needs a reason; a purchase return or a transfer loss with a High-severity reason waits in Pending Approval; a new Privacy Approver role can be named on a notification decision. Public image addresses stay off until the owner sets `PUBLIC_MEDIA_SIGNING_KEY` and turns the setting on. Two migrations change metadata only (rules, a hold record type, one favourites grant); no client document is touched.

**"Start Building next 20 items" - Cashier no longer sees PIM Reports, labelled fields and darker muted text, one number and date format everywhere, 2,764 inline styles moved to classes, faster screen switches and inbox, a batch-loading synthetic client, a release-candidate check at Deploy, threshold-splitting guard, joiner / mover / leaver approval, sign-in addresses, security-log history backfill, setup checks from the Finance pack** *(code + tests + guides, session erp-0c; released in 0.23.0 `1c026d1`, not yet deployed)*

What changes for existing clients: a Cashier no longer sees PIM Reports; numbers and dates read the same on every screen (day-first, `03 Feb 2026`); saving an Employee no longer re-activates a disabled user and a leaving Employee disables their user; Users shows each user's last sign-in address; the Security tab gains Access review and, on the Event export box, History from / Send history; Deploy stops before the backup when the commit has no passing release-candidate record. Two new settings, both **Off**: `approvals.threshold_split_guard` and `security.identity_change_approval`.

**"Once both done, Push and release ... DO all 4 of above" - the four steps left by "Implement all": the disk clean-up run, a duplicate price-override function removed, recommendations for the rest of the unused code, the old remote branches, then the release** *(session erp-30, was erp-0f)*

Detail is in the tracker under *OPS › Repository & developer tooling* (103.1, 103.4.1, 103.5).

## 2026-11-11

**"Start next 20 open items building" - the ledger in the caller's transaction, 18 more keyed routes, locked-record answers, stated import totals, the reason-code catalogue, faster screen switches, long lists that filter every row, full-disk safety, Go 1.27.4 with govulncheck blocking, the upgrade channel, history / binary / backup secret scans, a bigger synthetic client** *(code + tests + guides, session erp-8c; released in 0.23.0 `1c026d1`, not yet deployed)*

What changes for existing clients: reversing a journal voucher is one step and a second reversal is refused; editing a dispatched challan or a posted purchase return says what to do instead of a bare refusal; an opening-balance import can state its totals and approval refuses a file that does not tie; reason codes gain **Also used in**, **Severity** and **Needs a note**, plus 23 starter codes, and with codes required a transfer shortfall or a sales return needs a code; a second sign-off on a cutover pack is refused; switching back to an open screen no longer redraws its star and views twice; a filter or sort on a long list includes every row; a full disk leaves no partial audit archive or logo; new clients get every starter reason code; backups are written owner-only with the key kept off the command line (the restore drill too); Deploy honours the host's `UPGRADE_CHANNEL`.

**"All approved" - the twelve ask-162 decisions taken as recommended; seven built: store-only material issue, a reason to close a period that is not ready, a role-aware sidebar, server-side sign-out and idle limit, rehearsal on for new clients, a release-evidence schedule, a footprint entry per release** *(code + tests + guides, session erp-e7; shipped in release 0.23.0, `1c026d1`)*

What changes for existing clients: a Cashier no longer sees **Issue** / **Return unused** on Material Issue (Store Manager and HR/Admin do); closing a period with a failing check asks for a reason; the sidebar hides Trial balance, Chart of accounts, WMS labour and four PIM pages from roles that cannot read them; **Sign Out** now ends the session on the server. Unchanged unless a client chooses: the server idle limit is 0 (off); `platform.require_rehearsal` stays Off for existing clients (new clients start On).

**"Implement all" - repository restructure, phase two: migrations in `db/migrations/`, about 480 files renamed, nine unused functions removed, a disk clean-up script for the owner** *(code layout + docs, session erp-0f, run header erp-28; NOT COMMITTED)*

The owner's answer to the Stage 103 decisions and the renames §338 left until the other sessions were idle. Detail is in the tracker under *OPS › Repository & developer tooling*. The owner's explanation is section 6 of `docs/engineering/repository-restructure-2026-11-10.md`, and every old and new path is in `docs/archive/renamed-files-2026-11-11.md`.

**"Build all recomended" - the reel review's three decisions built: Deploy alerts warning, browser errors in the System Log, a browser smoke run as a release gate** *(code + scripts + guides, session erp-10; NOT COMMITTED)*

The user approved the recommended answers to §340's open decisions.

**"Checklist is still organisation required" / "Stage 35 onwards it is not organised" - the stage write-ups filed by module in the archive** *(docs only)*

After §337 the live tracker still ended with 67 `### Stage N` write-ups under Background notes, and the archive held 115 more in no order. A one-off script, not kept in the repo, moved them all into the archive's Background notes:

## 2026-11-10

**"WAtch all insta video and derview the insight and prepare which is required and which is not with reason" - six AI-coding reels judged against the ERP** *(review only, no code)*

Six Instagram reels (CI and tests; SQL injection; exposed keys, open database and an id changed in the URL; the ponytail plugin; one agent with a playbook per job; "vibe coder vs AI engineer") were downloaded, transcribed with a local Whisper model and read frame by frame in scratch, then each claim was checked against the tree. Most is already in place: CI and the full suite on a fresh DB, parameterized SQL (the document filter key passes `safeFilterKeyRe` and the doctype's declared fields), no secrets in `public/`, tenant and insider isolation tests, human-gated destructive steps, skills and scripts as playbooks. Not adopted: ponytail (repeats Article 1 and loads every session), docs via MCP, per-PR agent review and auto fix-PRs, "strip semicolons" sanitizing. Four gaps filed as decisions with a recommendation each: 158.2 production alert webhook never checked, 158.3 browser errors never reported, 158.4 no clicked smoke check in the release, 158.5 UI rules only in Claude's memory (since answered by Article 7, item 159.1). Not committed. Detail: items 158.1-158.5.

**"Arrange in proper std and structured way... one core rule like a constitution" - CONTRIBUTING.md, the layout guard, the root tidy** *(docs + one test + scripts, session erp-3f; NOT COMMITTED)*

Items 159.1-159.3, 103.2, the root part of 103.1, `scripts/archive/` from 103.4, and 158.5 (answered) in `micro_checklist.md` under *OPS › Repository & developer tooling*. The full explanation the user asked for, with every move, deletion, rule and what waits, is `docs/engineering/repository-restructure-2026-11-10.md`.

**"Checklist rebuild task" - the tracker rearranged by ERP module, skeleton permanent** *(docs + one script)*

The user found 120+ stage sections too many to read. `docs/micro_checklist.md` and its archive are now arranged the same way: 24 permanent modules (PIM, PROC, INV, WMS, OMS, POS, CRM, FIN, TAX, HR, MFG, ASQ, RPT, PRT, CHN, DATA, CFG, TEN, SEC, PLAT, UI, OPS, QA, DOC) and 130 sub-modules, with each module's open and built counts in a Module map at the top. All 1,898 top-level items (491 live, 1,407 archived) were sorted into sub-modules by five classifier passes against one taxonomy, then spot-checked. Ids, text and order within a sub-module are unchanged, so code comments and the release commit message still find them. Stage write-ups, minus their items, sit under Background notes in each file, so old stage anchors still resolve. New asks get a run header at the end of the Asks log and items in their sub-module; no new stage sections. `docs/update-checklist-map.ps1` refreshes the counts and *Nothing open.* markers, prints the next free ask number, and fails on a skeleton that differs between the two files, an open item in the archive, or an item outside a module. The release runs it in G7. Its commit-title fallback now reads run headers as well as `## Stage` lines. A session filed Stage 158 the old way mid-rebuild; its five items were moved into modules the same day. Verified: open and built line counts, every item line and every prose line preserved; no broken links; doclint clean; brain claims the new script. CLAUDE.md's documentation conventions rewritten. Not committed. Detail: the "Checklist rebuild task" run header in the tracker's Asks log.

**"WHich are built from checklist archive it" - built work moved out of micro_checklist** *(docs only)*

Same split as §174, by a checkbox-and-status script: 35 closed stages moved whole, and from stages still open 10 closed sub-groups and 475 `[x]` items, into `docs/archive/micro_checklist_closed_stages.md` (724 `[x]` lines). A block moved only with no `[ ]` and no unfinished status or open decision; 12 stages flagged only by stale notes were checked by hand and moved. Stages 49, 146, 148, 155 left alone (other sessions' uncommitted edits). Relative links rewritten for the archive folder, all resolve; open-line count unchanged. Tracker 1,571 KB → 920 KB. Not committed. Detail: the 2026-11-10 entry at the top of micro_checklist.

## 2026-11-05

**"Build next 20 items" - Stage 148 Quality and optional modules, seven open bugs, the rest of Stage 146 with no blocker** *(code + tests + guides, session erp-22; NOT COMMITTED)*

What changes for existing clients with no setting: an OMS bill now carries the order's shipping charge with its GST; a returned free gift credits the gift account the sale charged, not cost of goods sold; Ready to pay leaves out bills still waiting for their PO, which sit on a Waiting for PO tab; a PO routing rule must name roles that exist; download file names are cleaned; a product sheet saves whole or not at all. Everything else here stays off until a module or setting is turned on.

**"Start building next 20 items" - Stage 148 Finance, Selling, Memo stock and Vendor link, five half-built Stage 49 items** *(code + tests + guides, session erp-4e; NOT COMMITTED)*

The ask, the 20 items and the result are on the "Start building next 20 items" (erp-4e) header in `micro_checklist.md`; each item's line carries what was built, how it was verified and what is left. This section is the summary and what the run taught.

## 2026-11-04

**"I told to take next piece of point not already being build 20 point to build. Once this is fixed, Take next 20 to build" - Stage 140.14 screen fixes, what was left of 148.1 / .2 / .3 / .4 / .6, buying W2 and W3** *(code + tests + guides, session erp-60; NOT COMMITTED)*

The ask, the 20 items and the result are on the "Next 20" header in micro_checklist.md; each item's line carries what was built under **erp-60 (2026-11-04, second run)**.

**"start next 20 item ro build" - Stage 148 W1 and W2, the PIM phase of Stage 146, half-built 49 parts** *(code + tests + guides, session erp-60; NOT COMMITTED)*

The user's ask, the items and the result are on the erp-60 claim header in micro_checklist.md; each item's line carries what was built under **erp-60 (2026-11-04)**. Peers were asked first; erp-48 keeps Stage 143. The user then added "take next piece of point not already being build" and "Once this is fixed, Take next 20", so the next run's header sits below this one.

**"Once deployed, Start building next20 items" - the rest of Stage 146 plus half-built 47 / 49 / 157 parts** *(code + tests + guides, session erp-3f; in release 0.23.0 `cbf2799`)*

The user's ask, the 20 items and the result are on the erp-3f claim header in micro_checklist.md; each item's line carries what was built under **erp-3f (2026-11-04)**. erp-79, erp-d8, erp-34, erp-48 and erp-f8 were asked first and claimed nothing.

**"Push and deploy. All approved" - 0.22.0, every session's work, deployed to app and minn** *(release, session erp-d8)*

User, 2026-11-04: *"Push and deploy. All approved"*, then *"Deploy"*.

## 2026-11-03

**Stage 157 — The UI fits any window size or browser zoom** *(CSS + guide, session erp-79; NOT COMMITTED)*

The user's POS screenshot showed the top bar's second row (store, Refresh, Reset, ?) drawn over the page in a narrow window, and they asked for a UI that adapts at any zoom. Items 157.1-157.4 in micro_checklist.md carry the detail.

**Stage 146 - 20 of the vendor-recording gaps built** *(code + tests + guides, session erp-e9; NOT COMMITTED)*

The user's ask (the next 20 items, half-built first, skip items in flight) and the result are on the erp-e9 claim header in micro_checklist.md; each item's line carries what was built under **erp-e9 (2026-11-03)**.

**"Build next 20 items" - 20 half-built Stage 47 / 49 items taken further** *(code + tests + guides, session erp-e8; NOT COMMITTED)*

The user's ask, the rule (half-built items first, skip items in flight, 20 in all) and the result are on the erp-e8 claim header in micro_checklist.md; each item's line carries what was built under **erp-e8 (2026-11-03)**.

## 2026-11-01

**Stage 153 — Sticker Printing ▸ Print from Transaction: select all** *(code; NOT COMMITTED)*

The user's ask ("need a select all in print", on a Print from Transaction screenshot) is quoted on the Stage 153 header in micro_checklist.md.

## 2026-10-31

**Stage 149 — No explanatory blurbs anywhere; PIM Workbench loads on a full catalog** *(code + tests, session fb7b6adf; 149.2 and part of 149.1 shipped in 0.21.0, the rest NOT COMMITTED)*

The user's asks (the Bulk CSV Import screenshot, then "PIM workbench is not loading") are quoted on the Stage 149 header in micro_checklist.md.

**Stage 150 — Switching the store keeps every screen where it is, like an app** *(code + docs, session erp-f2; NOT COMMITTED)*

The user's ask (an RFQ ▸ New RFQ screenshot) is quoted on the Stage 150 header in micro_checklist.md.

**Stage 148 — What a 138-episode D365 F&O course shows the ERP should do, smarter** *(research + docs, session erp-f2; plan shipped in 0.21.0)*

The user's ask is quoted on the Stage 148 header in micro_checklist.md.

**Stage 146 — What 12 vendor meeting recordings show that the ERP lacks: blueprints, gaps, plan** *(research + docs, session erp-99; NOT COMMITTED)*

The user's ask is quoted on the Stage 146 header in micro_checklist.md.

**Stage 151 — HRM: each HR page its own sidebar entry, like PIM** *(code + docs, session erp-hr1; shipped in 0.21.0)*

The user's ask (an HRM ▸ HR ▸ Appraisals screenshot with the tab strip) is quoted on the Stage 151 header in micro_checklist.md.

**"Release all changes until now from all work" - 0.21.0, every session's work, deployed to app and minn** *(release, session erp-b6)*

User, 2026-10-31: *"You can ask everyone to stop working unill you say. & Release all changes untill now from all work"* (after *"Relase now. That is high priority to test"* for the barcode print, 143.6).

**Stage 147 — Item boxes find by Design ID, Combination ID or barcode; the OMS order line shows stock and fills the price** *(code + tests + docs, session erp-oms; released in 0.21.0 `4b80806`, deployed to app and minn)*

The user's ask (an OMS ▸ New order screenshot, then two corrections: Design ID, Combination ID / SKU and barcode may all carry letters) is quoted on the Stage 147 header in micro_checklist.md.

**Stage 145 — Inventory: a one-page management dashboard, every inventory page on the top-bar store, Stock on Hand by design with columns each user picks** *(code + tests + docs, session erp-inv; shipped in 0.21.0)*

The user's ask is quoted in full on the Stage 145 header in micro_checklist.md; items 145.1-145.4 carry the detail.

## 2026-10-30

**"Push and deploy properly ... all approved" - 0.20.0, everything in the tree, deployed to app and minn** *(release, session erp-1f)*

micro_checklist run header "Push and deploy properly" (2026-10-30).

**"Build next 20 open items" (session erp-6b): twenty Stage 47 / 49 slices - a cross-store write hole, raw database text in errors, a screen burst refused, a posted GRN cancellable, strict posting transitions, revocable exports, job actors** *(code + tests + docs; NOT COMMITTED)*

The user's "build next 20 open items" (2026-10-29). erp-7c held the six approved decisions and 140.12 was erp-91's; the Stage 47 / 49 items no run had touched needed a reviewer, an environment, counsel or real users. So the run took the "Left:" slices earlier runs had named and two not-started follow-ups: 47.10.7, 47.11.2, 47.11.3, 47.11.8b, 47.13.3, 47.14.3, 47.17.5, 47.18.3, 47.18.4, 49.3.8, 49.4.6, 49.4.7, 49.5.1, 49.5.3, 49.5.4, 49.8.2, 49.8.5, 49.8.6, 49.10.4, 49.11.1. Item detail: each item's entry in micro_checklist and the run header after the erp-7c block. Seven are done and ticked; thirteen are built in part, with what is left on each.

## 2026-10-29

**The erp-d3 follow-ups: offline sales on the till's day, held offline cash in the drawer, SKU scans, no PO for a stateless store** *(code + tests + docs; released 0.19.0 932f3c8)*

The user's *"all recommended"* on the six follow-up decisions of §304 (micro_checklist 49.5.6d, 49.5.6e, 47.11.8b).

**Stage 141 — Wholeops beside a client's brand; refreshes that keep the brand and the menu; a rate limit sized for real use** *(code)*

From the user's minn screenshots (branding on: Wholeops only as grey foot text), their pick of four real-screenshot mockups (*"c1 b2"*), and a video of repeated refreshes on app.wholeops.in. Full detail in checklist Stage 141.

**"Build next 20 items" (session erp-5e): twenty Stage 47 / 49 slices - privilege-change evidence, two hostile clients, insider scenarios, GST / Companies Act controls, receivables vs ledger, synthetic tenants, slow-query shapes, CERT-In logs, a secret scan** *(code + tests + docs; released 0.19.0 `932f3c8`)*

The user's "Build next 20 items except 140's list" (2026-10-28). Everything open outside Stages 47 / 49 waited on the user, a device, a credential, a decision or another session, so the run took the twenty Stage 47 / 49 items no run had touched that have a self-contained slice: 47.9.1, 47.9.3, 47.12.4, 47.13.2, 47.13.5, 47.14.3, 47.14.4, 47.16.1, 47.16.3, 47.16.4, 47.18.2, 49.3.6, 49.3.7, 49.5.5, 49.5.7, 49.6.2, 49.6.6, 49.8.8, 49.10.3, 49.10.6. Item detail: each item's entry in micro_checklist, and the run header under erp-d3's decisions block. All twenty are built in part; what is left is on each.

## 2026-10-28

**The erp-d3 decisions: offline sales validated before they post, vendor state required, unknown client codes refused, nothing booked twice, per-client date order, outside calls switch** *(code + tests + docs; released in 0.19.0 `efc6403`)*

The user answered the eight decisions the "next 20" run (§300) left open; item detail: micro_checklist block "User, 2026-10-28: answers to the erp-d3 decisions" (49.5.6a-e, 49.3.2a, 49.2.1a, 47.11.8a-b, 47.14.2a, 49.8.7a, 49.10.8a, 49.3.4b).

**Stage 135.21-135.26 — every posting under its document's lock; provisioning seeds stock locations; PIM publishes to Shopify linked** *(code + tests + docs, released in 0.19.0 `efc6403`)*

The user: *"Yes, Acknowledged all as recommened"* - build 135.21 and 135.23, route Shopify channels in PIM Publish through the Shopify page (135.22), and the 135.10 / 135.18 answers confirmed. Item detail: micro_checklist 135.21-135.26.

**Stage 140 — Guide runs the whole organisation cycle on app as user, BA, audit, accounts and customer** *(live run + docs; no ERP code changed)*

The user asked Guide to go through every module as each kind of user, on app.wholeops.in, and to end with two documents: the process, and the enhancements filed in the tracker module by module. Guide got a new suite, `orgcycle`, for the cycles its existing suites did not cover: a customer's sales, advances, credit invoices, receipts, credit note and return; a website order through Accept, rack pick, label, handover and an OMS return; Shopify, Amazon and Myntra templates; CRM segment and campaign; and a before-and-after reconciliation of every report that ties to the GL. Each cycle was rehearsed on a local copy of 0.18.0 until only real findings were left. The user then approved the listed writes, and Guide ran `orgcycle`, HR and the procurement / quality / service flows on the test store, and finally all 421 reports.

## 2026-10-27

**The approved follow-ups: one statement per request, a lighter font, eight list columns; 0.18.0 released and deployed** *(code + tests + docs, NOT COMMITTED)*

The user approved the §296 batch and its five open decisions. The batch shipped in 0.18.0 (`9a008cf`), with the parallel sessions' finished work, and was deployed to app and minn after a verified backup. Before the deploy, the data-change scan learned a check it had missed: a background sweep that starts deleting a table it never touched (the retention sweep had gained three) now counts as a data change. Release G9 and Deploy D1 both list it, and the deploy waits for YES. The user answered YES.

**"Build next 20 items": fourteen Stage 47 / 49 slices** *(code + tests + docs, NOT COMMITTED - held in release/hold.txt)*

The user: *"Build next 20 items"*, then *"go ahead"*. Stages 47 / 49 / 50 had no unblocked item left in tracker order, so the run took the six not-started Stage 135 findings and the fourteen Stage 47 / 49 items earlier runs had passed over. Item detail: micro_checklist header of 2026-10-27 and each item's entry.

**Stage 135.10-135.22 — decisions, stock integrity and locking** *(code + tests + docs, released in 0.19.0 `efc6403`)*

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

