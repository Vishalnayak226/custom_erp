---
doc_id: DOC-PROD-IMPROVEMENT-20261125
title: ERP improvement review and prioritized plan
type: reference
status: draft
owner: product-owner
approvers: [product-owner, engineering-owner, operations-owner]
audience: [product, engineering, operations, implementation, finance, qa]
applies_to: 2026-11-25 working tree; HEAD 7d59685, release 0.29.0
authority: proposed-policy
confidentiality: internal
last_verified: 2026-11-25
review_by: 2027-02-23
supersedes: none
superseded_by: none
verification_scope: source and tracker inspection plus artifact-budget measurement; no new browser, device, provider or production acceptance
---

# ERP improvement plan — 2026-11-25

## Recommendation

Yes, improvements remain. Start with the Returns work queue, then finish the existing
foreign-currency settlement decision and verify the organization's connected workflows.
The useful next step is operational depth and reliable evidence rather than another large
feature expansion. Much of the earlier October plan has already been built; do not restart it.

This review inspected current source in Returns, Finance and the shared document-list
handler; the module tracker, recent handover and release history; current screen-list
patterns; and the scope of the existing organization-cycle record. It measured current
artifact budgets. Findings below distinguish source-confirmed behavior, existing recorded
gaps and proposed verification. A static finding is not a claim that a new live UI run passed.
The shared tree contains other sessions' edits; this ask changes documentation only.

At review start, the checklist-map check reported **25 modules, 191 open checkboxes and
2,544 built checkboxes**. These include parents, sub-items, hardware/provider gates and
acceptance reviews: they are not 191 missing features. The checklist introduction still
says 24 modules; its permanent skeleton/map has 25. Reconcile that small wording discrepancy
in the next tracker-maintenance pass, without adding/removing any module.

## 1. Evidence and priorities

| Priority | Improvement | Evidence / confidence | Consequence |
|---|---|---|---|
| P1 | Make the Returns queue complete beyond its first 200 records | Source-confirmed: `loadReturnsView` fetches `ReturnRequest?limit=200`; status filtering runs on that array; no paging/cursor is consumed | An outstanding return outside the loaded batch is unreachable from this work queue |
| P1 | Make displayed and effective Returns filters agree after navigation | Source-confirmed: module-level `returnsViewFilter` survives navigation; new select markup defaults to Open; no assignment synchronizes it with the retained value | A user can see Open selected while the table uses the earlier filter; reproduce in-browser before fixing |
| P1 | Capture the date and rate of foreign-currency payments | Existing item **264.4** plus source confirmation: shared `askSettlementAmount` asks only amount/reference | An operator cannot enter the historical settlement facts through this dialog; existing owner decision remains open |
| P2 | Replace typed item codes in return QC with the existing item picker | Source-confirmed in `openReturnQCDialog`: exchange item is an optional text box asking for item code | An operator must know a technical identifier and can mistype it; role/stock/price checks still belong on the server |
| P1 | Check other bounded operational lists for hidden outstanding work | Candidate, not blanket defect: recent lists in Inventory, Job Work and Warehouse use fixed limits | A bounded list is healthy only if search, filters and paging still reach all permitted work |
| P1 | Finish role-operated, reconciled acceptance on the intended release | Existing 55.22 / 47.17 / 74.5.7 gates; the October organization-cycle record explicitly describes Super Admin API calls | A privileged API pass does not prove an ordinary user's actual screen journey or independent finance acceptance |
| P1 | Close physical-print and external-provider outcomes with real evidence | Existing printer/mobile/provider gates and PrintBridge hold; no new device/provider test in this review | A saved/spooled job or local fake is insufficient proof of a physical print, refund, delivery or statutory receipt |
| P1 | Align release, deployment, held work and acceptance records | Git shows 0.29.0 at HEAD while several current tracker/handover entries still describe uncommitted work or an in-progress release | Operators need one reliable statement of what is running and what remains held; deployment is not inferred from HEAD |
| P2 | Protect shell headroom and measure task latency as data grows | Measured now: cold core **182,660 / 184,320 bytes**, initial JS **113,817 / 122,880 bytes**; releasebudget exits 0 | Budgets currently pass, with 1,660 bytes cold-core headroom; prior overruns are historical, not today's result |

Evidence anchors: [Returns source](../../public/view-returns.js),
[Finance source](../../public/view-finance.js),
[shared list handler](../../internal/server/handlers_core_doc_engine.go),
[module tracker](../micro_checklist.md),
[organization-cycle record](../qa/guide-org-cycle-2026-10-27.md),
[performance budgets](../qa/perf-budgets.json).

## 2. Proposed implementation packages, in order

These packages are proposals, not approved implementation or newly completed features.
Existing owner/decision gates remain with their original items. Record a build item in the
appropriate permanent sub-module when implementation starts; do not create a parallel stage.

### A. Complete and trustworthy Returns work queue

**Owner:** POS › Returns & exchanges at the till, coordinated with OMS › Returns & cancellations.
**Story:** as the return-desk operator, I can find every outstanding return and understand the
filter and amount shown, regardless of how old it is or how many closed returns exist.

- Use server-side status/search filters before pagination and consume the existing bounded
  paging contract. If the Open group needs several statuses, reuse an existing safe list
  facility or add narrowly validated support; do not invent arbitrary SQL/operator input.
- Provide paging/search, stable order and a truthful loaded-page versus whole-result count.
  Do not fix this by downloading all records or raising 200 to a larger arbitrary number.
- Restore the selected filter consistently on navigation, or reset both state and select
  explicitly. Preserve search/page only where the screen makes that behavior clear.
- Distinguish no matching work from failed/incomplete loading. Test repeated Refresh and
  navigation during a slow response so stale responses cannot paint over newer choices.
- Keep the existing targeted refund lookup: it already filters by `return_request_id`.
  Do not undo item 215.14 or recreate the old global-refund scan.

**Done bar:** on an isolated database with at least 250 returns, including an open return
outside the first batch, the operator can find and action that record through the UI.
Status/search/paging remain correct after leaving and reopening the screen. A restricted
role cannot see another location's return, and summary scope is explicit. Refund/replay
and stock/tax behavior remain unchanged and covered by the relevant regressions.

### B. Make return inspection/exchange understandable

**Owner:** same Returns owner; reuse shared picker patterns.
**Story:** I select an exchange item by readable name/barcode rather than memorizing its code.

- Reuse the existing authorized Item picker and keep the returned line, inspected quantity,
  condition/disposition and original bill visible within the existing screen rules.
- Show the selected SKU and name, then explain any price difference or stock constraint using
  the existing exchange workflow. Do not reinterpret exchange/refund accounting policy.
- Preserve valid entries after an invalid selection or server error; retain keyboard and
  scan behavior. Selection never substitutes for server eligibility checks.

**Done bar:** a beginner completes a partial exchange without typing an item code, an invalid
or unavailable item is rejected clearly, cancellation preserves the return, and stock, tender,
tax and value-ledger results reconcile to the original sale.

### C. Finish foreign-currency settlement facts — existing 264.4

**Owner:** FIN › Currencies & multi-company. **Gate:** 264.4 explicitly waits for owner approval
of new fields on the money screen; this planning request does not close that decision.

- Proposed shared form: amount, actual settlement date and optional bank exchange rate for a
  foreign-currency invoice; preserve the simple domestic path and existing repeat identity.
- Display invoice currency, posting/base currency, outstanding amount, rate source and the
  resulting base amount before confirmation. Use current backend date/rate support after
  verifying its validation and period rules; do not duplicate FX calculations in the browser.
- Preserve partial settlement, full settlement, bank references, closed-period policy and
  realized-gain/loss treatment. Account Head reviews expected accounting; CA reviews applicable
  policy separately.

**Done bar:** AP and AR each pass partial and final foreign-currency settlements on different
dates/rates, a closed-period attempt, invalid-rate validation and a lost-response retry.
Invoice balance, payment history, FX result and GL agree. A domestic settlement stays simple.

### D. Audit bounded queues at shared points

**Owner:** UI › Lists, tables & search with each domain's list owner.
**Starting sample:** Inventory Adjustment, Material Issue, Delivery Challan, Subcontract Order,
WarehouseTask moves, PIM data and OMS release/hold queues.

- Classify each list as a preview/recent list, selectable reference list or complete work
  queue. A fixed limit is not inherently a defect; discoverability and labeling decide it.
- Check filters/search are sent to the server before limits, permitted records beyond page
  one can be found, stable sort/paging is available, and totals are not derived from one page.
- Repair shared list/picker primitives where callers share the problem. Keep intentional
  previews bounded and provide their link to the complete list.

**Done bar:** every audited list has a documented scope and a beyond-first-page UI case;
only reproduced gaps become fixes. Tenant/location/field scope holds for counts and exports.

### E. Run one real organization cycle using actual roles and screens

**Owner:** QA › Audits, reviews & QA sweeps; existing **55.22, 47.17 and 74.5.7**.

Use a disposable organization and the existing UAT run sheet: set up → request/PO → partial
GRN → vendor bill/payment → hold/count/transfer → POS sale → OMS fulfillment → return/refund
→ bank reconciliation → period close. Include PIM product identity/approval and, where in
scope, material issue/production, payroll, expense and asset transactions.

Each business step is operated through the UI by its actual role, with distinct maker and
checker accounts. DB/API reads corroborate persistence and reconciliation; they do not replace
the clicks. Record release, config, scope, generated numbers, source links, before/after
balances, retries and errors. Follow the current user/admin guide literally. Maintain the
existing separate real-user, Account Head, CA and device/provider acceptance requirements.

**Done bar:** the chosen critical journeys pass normal, partial, denied, repeated and
interrupted cases with zero unexplained stock, liability or GL differences. Record exactly
which human/device/provider gates remain open; do not mark the whole platform accepted.

### F. Finish physical printing, mobile and provider acceptance

**Owners:** PRT / POS / CHN / TAX; existing **249.1, 251.3, 258.5, 102.5.2, 73.B.11** and
credentialed provider items. Keep PrintBridge release holds until their own gates pass.

- Run printer-off/unplugged → one attempted print → reconnect → intentional retry, proving
  no surprise queued duplicate. Test the actual LD0801 and label stock; do not infer success
  from a Windows spool acceptance or preview.
- Prove remembered printer/copies, label direction, preview, barcode readability and reprint
  reason against the issued document revision. Keep physical status truthful in history.
- Complete real phone install/scan/push/offline/reconnect and RF floor checks on target devices.
- For each advertised channel/payment/courier/message/tax adapter, complete its existing
  sandbox contract: one request, repeated callback, timeout after remote success, reconciliation
  and supported recovery. A passing local fake remains valuable but separately labeled.

**Done bar:** the named devices/providers have explicit acceptance records with build/config,
actor and outcomes. External credentials/hardware/participants remain prerequisites.

### G. Reconcile release and operational evidence

**Owners:** OPS / DOC / SEC; reuse ask 265 and existing rollout/restore/alert gates.

- Reconcile source HEAD, released artifact, actual deployed version on each host, held files,
  migration state and customer-run Heals. Inspect existing release results before rerunning
  a release or assuming anything has shipped. Update stale handover statements once proven.
- Establish the approved enforcement state for MFA and cross-origin checks from their own
  existing decisions and production report evidence. Do not switch production settings as
  part of this plan or infer authorization from elapsed time.
- Exercise authorized restore and alert-delivery drills with DB, media, secrets/config and
  release compatibility; record measured RPO/RTO and the responder. Reuse current runbooks.
- Close Built, not verified items only when the exact named gate ran and produced evidence;
  distinguish unbuilt code, unverified code, owner decisions, vendor access and real acceptance.

**Done bar:** the supported deployment has one traceable evidence pack, retained external
gates and tested recovery. A green source suite alone cannot establish this state.

### H. Preserve speed and lightweight operation

**Owners:** PLAT › Performance & scale; existing browser/server measurement items.

Artifact measurements today pass: cold core 182,660/184,320 bytes (1,660 bytes free), initial
JS 113,817/122,880 bytes. These are compressed artifact sizes, not response time, browser
interaction, server capacity or stripped-binary results.

Keep new screen code lazy, use server filtering before limits and eliminate avoidable lookup
fan-out through existing batching. Measure populated large queues and a long session on
the current reference profiles; inspect p75 interaction data, query/lock/pool waits and memory.
Use existing budgets/ratchets and report misses honestly; do not raise a ceiling to pass.
Keep one heavy job at a time on the shared laptop, and do not introduce a framework, broker,
cache service or data mart without measured need and the existing decision process.

**Done bar:** targeted changes preserve current release gates and demonstrate the affected
task's behavior and latency on reproducible fixtures. Reuse recent comparable performance
evidence where unchanged rather than rerunning the entire suite for a documentation change.

## 3. Module coverage and where existing work remains

This table is a next-review map, not a finding that every area lacks functionality.

| Permanent module | Focus for the next improvement/verification pass |
|---|---|
| PIM | Supplier/role isolation, import result recovery, product-to-channel identity; retain 55.11/provider gates |
| PROC | Preserve the built purchase-to-pay path; partial/rejected receipts, bill matching and source links in the organization run |
| INV | Beyond-first-page adjustment/issue/challan discovery; movement drill-down and stock conservation |
| WMS | Durable pick/pack handoff and interruption; real RF/scanner acceptance |
| OMS | Source-linked order → allocation → shipment/invoice/return; repeat-callback/provider evidence |
| POS | Returns queue and readable exchange selection; preserve server stock/tender authority |
| CRM | Existing sender credentials, consent withdrawal and queued-send/reward reconciliation |
| FIN | Existing 264.4 FX decision; AP/AR, bank and Account Head close evidence |
| TAX | Existing IRP/e-waybill access and CA scope; external statutory acceptance remains separate |
| HR | Preserve completed functionality; role/privacy, local dates and payroll-to-payment reconciliation in UAT |
| MFG | Existing depth/measurement gates; partial output, material/WIP/cost and lot lineage |
| ASQ | Service/asset/quality source links, disposition, approvals and financial lineage |
| RPT | Whole-result versus page totals, exports, source cutoffs and scoped reconciliation |
| PRT | Named printer/device rounds, truthful outcomes and controlled reprints |
| CHN | Credentialed channel/payment/provider contracts and repeated/uncertain outcomes |
| DATA | Preview/reapply/correction evidence; production resets retain explicit authorization |
| HEAL | Customer-run old-record catch-up, preview and rerun safety; no deploy-time rewriting |
| CFG | Number uniqueness, fiscal/store scope and persisted effective configuration |
| TEN | Package/tenant lifecycle and cross-tenant evidence on the intended deployment |
| SEC | Existing enforcement rollout, external assessment and restore/isolation gates |
| PLAT | Bounded work, jobs/retries, current resource budgets and targeted concurrency evidence |
| UI | Shared picker/list/filter state, keyboard behavior, zoom/reflow and recoverable errors |
| OPS | Release/deploy/hold truth, monitoring delivery and recoverability |
| QA | Role-operated UI journeys and independent reconciliation instead of checkbox-only progress |
| DOC | Match current screen and release behavior; separate historical records from current support claims |

## 4. Execution order and approval boundary

1. **Build A after plan approval:** Returns completeness and filter-state correctness first.
   The reproduction fixture and role boundary are small, local and reviewable.
2. **Then B and the narrowly scoped audit D:** improve exchange selection; fix only list gaps
   demonstrated by the audit. Update their actual guide sections with verified steps.
3. **C follows the existing 264.4 decision:** implement once its owner has approved the proposed
   settlement fields/policy. Keep that decision attached to the existing item.
4. **Run E incrementally:** carry the same dataset through completed modules and record
   downstream conservation, authorization and recovery. Fix reproducible failures by source owner.
5. **Finish F/G with the required participants and infrastructure.** Prepare all local fixtures,
   run sheets and reconciliation first; obtain external acceptance rather than simulating it.
6. **Apply H throughout:** no performance-budget or laptop-load regression.

No feature estimate or world-class claim is justified by this source review alone. The next
build should close a specific measurable Done bar, then proceed to the next package. No code,
production settings, data, commit, push or deployment is authorized by this planning document.

## 5. Relation to the earlier plan

The [October whole-platform plan](wholeops-outsider-plan-2026-10-23.md) remains the broad
product map. This dated review narrows the next improvements using the current tree and
existing module owners; it does not reopen completed Stage 135/146/148 work or rewrite past
audit evidence. In particular, this review does not repeat old claims that journals,
returns/exchanges or CRM have no UI: those capabilities already exist.

**Ask 266.1 Done means this review and proposal are complete.** It does not mean packages
A–H were implemented or the listed acceptance/provider/device gates passed.
