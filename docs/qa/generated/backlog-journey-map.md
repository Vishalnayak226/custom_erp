# Backlog Journey Map

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: `docs/micro_checklist.md` open items, `docs/product/erp-build-checklist.md` JRN table
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from `docs/micro_checklist.md` open items, `docs/product/erp-build-checklist.md` JRN table, for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

BLD-002: the 194 open checklist items reconciled into the business journeys they serve. Acceptance environment for every journey: Chromium at three viewports, en-IN, tenant tiers Small / Mid / Large (BLD-002 §9). Reference configurations: REF-RETAIL-IN (PIM, OMS, CRM) and REF-WAREHOUSE-IN (WMS, procurement, OMS); a journey in neither is deferred until a customer needs it, which is its exclusion rationale. Item IDs are kept as they are in the checklist.

| Journey | Reference configuration | Owner | Open | Needs decision | Blocked |
|---|---|---|---:|---:|---:|
| JRN-01 POS/store | REF-RETAIL-IN | Store/finance; 47.2–47.4/47.6, Stage 31 | 7 | 1 | 6 |
| JRN-02 OMS/order-to-cash | REF-RETAIL-IN, REF-WAREHOUSE-IN | OMS/operations; Stage 35 | 2 | 0 | 2 |
| JRN-03 returns/refunds | REF-RETAIL-IN | Finance/store; 47.3/49.5 | 0 | 0 | 0 |
| JRN-04 inventory | REF-RETAIL-IN, REF-WAREHOUSE-IN | Inventory controller; Stage 36/47.5 | 11 | 0 | 9 |
| JRN-05 WMS | REF-WAREHOUSE-IN | Warehouse owner; Stage 36/47.6 | 0 | 0 | 0 |
| JRN-06 warehouse billing/3PL | REF-WAREHOUSE-IN | Warehouse/finance; 47.5; mixed owner excluded | 0 | 0 | 0 |
| JRN-07 source-to-pay | REF-WAREHOUSE-IN | Buyer/AP; Stage 37/procurement | 2 | 0 | 2 |
| JRN-08 PIM/catalog/DAM | REF-RETAIL-IN | Product data steward; 26.4/47.13 | 1 | 0 | 0 |
| JRN-09 record-to-report | REF-RETAIL-IN, REF-WAREHOUSE-IN | Qualified accountant; 26.6/Stage 37 | 0 | 0 | 0 |
| JRN-10 FX/intercompany/consolidation | deferred - outside both | Finance policy owner; Stage 37; depth decision DEC-01 | 0 | 0 | 0 |
| JRN-11 revenue schedules | deferred - outside both | Accountant; existing contract engine; separate from SaaS billing | 0 | 0 | 0 |
| JRN-12 India tax | REF-RETAIL-IN, REF-WAREHOUSE-IN | Tax/legal owner; 26.2/26.6/47.16 | 2 | 0 | 2 |
| JRN-13 manufacturing | deferred - outside both | Factory/cost owner; 26.9/Stage 37 | 0 | 0 | 0 |
| JRN-14 quality/maintenance | deferred - outside both | Quality/maintenance owner; chosen industry scope | 0 | 0 | 0 |
| JRN-15 CRM/loyalty | REF-RETAIL-IN | Marketing/privacy/store; 26.7 | 2 | 0 | 2 |
| JRN-16 HR/payroll | deferred - outside both | Qualified HR/payroll owner; 26.8 | 0 | 0 | 0 |
| JRN-17 assets | deferred - outside both | Fixed-asset accountant; Stage 37 | 0 | 0 | 0 |
| JRN-18 expenses | deferred - outside both | Finance/employee owner; existing expenses scope | 0 | 0 | 0 |
| JRN-19 projects/service | deferred - outside both | Service/project owner; Experimental scope preserved | 0 | 0 | 0 |
| JRN-20 reports/analysis | REF-RETAIL-IN, REF-WAREHOUSE-IN | Data/finance/QA; 26.10/47.10 | 0 | 0 | 0 |
| JRN-21 platform/integrations | REF-RETAIL-IN, REF-WAREHOUSE-IN | Integrator/security; Stage 38/49.8 | 8 | 1 | 0 |
| JRN-22 identity/approval/audit | REF-RETAIL-IN, REF-WAREHOUSE-IN | Tenant admin/security; 47.1/47.7/Stage 49 | 38 | 3 | 34 |
| JRN-23 knowledge/implementation | REF-RETAIL-IN, REF-WAREHOUSE-IN | Documentation/implementation; Stage 39/47.15/48.7 | 6 | 0 | 6 |
| Cross-journey acceptance and audits | - | - | 39 | 2 | 27 |
| Engineering housekeeping | - | - | 3 | 0 | 3 |
| Unmapped - place these stages | - | - | 73 | 8 | 17 |

**Unmapped:** Stage 143 (2), Stage 146 (5), Stage 149 (1), Stage 154 (2), Stage 158 (1), Stage 162 (3), Stage 171 (1), Stage 176 (1), Stage 177 (6), Stage 184 (1), Stage 185 (1), Stage 194 (2), Stage 195 (1), Stage 203 (2), Stage 205 (22), Stage 206 (5), Stage 209 (1), Stage 211 (1), Stage 212 (2), Stage 213 (4), Stage 214 (5), Stage 215 (2), Stage 217 (1), Stage 219 (1) - add each to `stageJourney` in `cmd/gendocs/journeys.go`.

## JRN-01 POS/store

- **47.6** Real mobile/RF WMS/POS shell and physical reference journey *(blocked)*
- **102.1.2** Camera scanning *(blocked)*
- **102.1.3** Push alerts *(blocked)*
- **102.5.2** Real phones *(blocked)*
- **31.1.8** Live verification against a real QZ Tray and a real printer — partially closed 2026-0... *(blocked)*
- **31.1.9** Replace QZ Tray with our own lightweight agent (PrintBridge) — Windows deployable; ma... *(decision)*
- **53.15** Deploy *(blocked)*

## JRN-02 OMS/order-to-cash

- **99.8.1** End to end on a real Shopify development store *(blocked)*
- **99.9.1** Guides and release gate *(blocked)*

## JRN-04 inventory

- **121.1** "GRN 7" not in Print stickers' Document list *(blocked)*
- **106.2** Square and Ring label designs from the client's old PRN files *(blocked)*
- **113.3** A ready design for the Seznik 100 × 14.3 tag *(blocked)*
- **119.1** Four Seznik built-ins: horizontal × 1, horizontal × 3, vertical × 1, vertical × 3 *(blocked)*
- **58.7** Physical printer check
- **119.4** A clean browser print *(blocked)*
- **137.3** Alignment set once per printer
- **137.4** One PrintBridge installer from the ERP *(blocked)*
- **137.5** PrintBridge wakes with the ERP (after installation) *(blocked)*
- **137.6** Quit from the tray comes back after a restart *(blocked)*
- **106.3** POS bill print format from the client's video *(blocked)*

## JRN-07 source-to-pay

- **129.5** Docs, tests, live check *(blocked)*
- **98.5.2** Walk-through *(blocked)*

## JRN-08 PIM/catalog/DAM

- **47.13** Master-data migration, stewardship and legal product attributes

## JRN-12 India tax

- **66.9** Confirm every live client has its industry set *(blocked)*
- **47.16** India-first data lifecycle and compliance-control implementation *(blocked)*

## JRN-15 CRM/loyalty

- **101.3.1** Senders *(blocked)*
- **131.2** Settings → Platform → Data Console does nothing on live *(blocked)*

## JRN-21 platform/integrations

- **56.6** Prove multi-tenant hosting end to end — this is the item that makes the money
- **56.1** Boot on Railway with the security baseline in warn mode
- **56.2** SB-015 — encrypted database link
- **56.3** SB-012 — proxy trust against Railway's edge
- **56.5** Flip `ENV=production` and confirm a clean boot
- **109.12** Smart cleanup bot every 10 days *(decision)*
- **56.10** Stand up the backup job itself on Railway
- **56.13** Docs

## JRN-22 identity/approval/audit

- **49.2** Identity, authentication, recovery and machine identity *(blocked)*
- **49.2.3** MFA assurance tiers *(blocked)*
- **47.1** Deny-by-default authorization, privacy and segregation of duties *(blocked)*
- **49.3** Deny-by-default authorization, tenant isolation and segregation of duties *(blocked)*
- **49.3.3** Database isolation defense *(blocked)*
- **47.8** Session containment and XSS-resistant browser boundary *(blocked)*
- **49.4** Browser, session, API and request-boundary hardening *(blocked)*
- **49.4.6** Canonical input validation *(blocked)*
- **49.4.8** Browser/API security regression suite *(blocked)*
- **49.7** Database, host, network and deployment hardening *(blocked)*
- **49.7.8** Hardened-deployment test *(blocked)*
- **49.10** Layered security verification and break-the-ERP program *(blocked)*
- **49.10.4** Authenticated application/API DAST *(blocked)*
- **49.10.7** Independent penetration test and retest *(blocked)*
- **49.13** Availability, abuse resistance, backup integrity and ransomware resilience *(blocked)*
- **49.13.4** Backup threat model *(blocked)*
- **49.13.5** Restore is a security control
- **49.13.6** Ransomware/destructive-admin drill *(blocked)*
- **49.12** Incident response, forensic readiness, breach workflow and safe recovery *(blocked)*
- **49.12.5** Notification decision workflow *(blocked)*
- **49.12.8** Tabletop and live drills *(blocked)*
- **49.14** Secure human operations, support, devices and anti-social-engineering controls *(blocked)*
- **49.14.5** Governed break-glass *(decision)*
- **49.14.6** Secure device/browser baseline *(decision)*
- **49.15** Vulnerability disclosure, triage, remediation and security-update lifecycle *(blocked)*
- **49.15.6** Emergency update drill *(blocked)*
- **49.16** Privacy, legal, regulatory and customer security assurance *(blocked)*
- **49.16.1** Applicability register *(blocked)*
- **49.16.4** Contract truth gate *(blocked)*
- **49.17** Security release gate, scorecard and continuous improvement loop *(blocked)*
- **49.17.1** Per-change security gate *(decision)*
- **49.17.3** Configuration go-live gate *(blocked)*
- **49.17.5** Fixed operating cadence *(blocked)*
- **49.17.6** Canary and rollback *(blocked)*
- **49.17.7** Independent challenge *(blocked)*
- **49.18** Security weight, performance and storage budget ledger *(blocked)*
- **49.18.1** Per-control before/after measurement: *(blocked)*
- **49.18.7** Multi-year forecast and cleanup drill *(blocked)*

## JRN-23 knowledge/implementation

- **91.4** Run it on app.wholeops.in and minn.wholeops.in *(blocked)*
- **124.1** PO setup guide *(blocked)*
- **48.6** Security, privacy, compliance and legal document set *(blocked)*
- **48.6.6** Legal document register *(blocked)*
- **48.11** Documentation CI, health reporting and completion gate *(blocked)*
- **48.11.4** Quarterly sampled walkthroughs *(blocked)*

## Cross-journey acceptance and audits

- **55.11** PIM/catalog
- **73.B.11** Physical RF acceptance *(blocked)*
- **55.9** OMS/marketplace/delivery *(blocked)*
- **55.10** Finance/close *(blocked)*
- **73.4.3** Finance: *(blocked)*
- **74.4.1** Eligibility engine *(blocked)*
- **74.4.2** Pre-submission validation *(blocked)*
- **74.4.3** Provider adapter on the existing pattern *(blocked)*
- **74.4.4** Status models kept apart from the document's own status *(blocked)*
- **74.4.5** Locks and corrections *(blocked)*
- **74.4.7** GST Compliance workbench and reconciliation *(blocked)*
- **55.14** HR/payroll
- **73.4.5** Manufacturing / quality: *(blocked)*
- **55.18** Service/projects *(blocked)*
- **55.19** Stickers/printing
- **73.C.5** Remove the throwaway client `batchc55f` *(blocked)*
- **50.12** SaaS and separately sellable modules *(decision)*
- **47.9** Representative-volume performance laboratory and enforced budgets *(blocked)*
- **47.18** Server-space and dependency budget ledger *(blocked)*
- **50.14** Lightweight server and frontend proof
- **55.2** Shared shell/list/form/Help contract
- **47.14** Human-language, state clarity and action-oriented product shell *(blocked)*
- **50.13** UI and accessibility acceptance *(blocked)*
- **50.19** Scoped release and customer handoff *(blocked)*
- **134.11** Railway staging *(blocked)*
- **50.15** Recovery, operations, privacy and security acceptance *(blocked)*
- **50.11** Verification that can support a release *(decision)*
- **74.U.1** Carry the BRD's §10 cases into the UAT run sheet as each module ships: *(blocked)*
- **74.U.2** Add E2E-01..16 to 47.17.1's reference journeys and `docs/operations/uat_run_sheet.md`:
- **47.17** Reference-journey assurance and release evidence pack *(blocked)*
- **50.17** Validate implemented ERP breadth end to end *(blocked)*
- **55.0F** Functional audit and repair gate
- **55.22** Cross-module acceptance
- **74.5.7** Pilot gates *(blocked)*
- **57.12** Full-ERP walkthrough video with segment-level re-recording
- **57.12a** SOP videos to the client - kept open for the end (user, 2026-10-17)
- **50.16** Finish existing Stage 48 *(blocked)*
- **74.0.1** Decisions DEC-01-DEC-22 *(blocked)*
- **135.9** Phases P02-P12 stay with their owners *(blocked)*

## Engineering housekeeping

- **132.9** Guide learns the whole ERP *(blocked)*
- **132.12** Guide expert in every module's setup and inside each module, and the help manual *(blocked)*
- **132.4** Whole ERP, every screen and form, each role; production read-only pass when the user says *(blocked)*

## Unmapped

- **146.5.12** Typed product relationships *(blocked)*
- **149.2** PIM ▸ Workbench does not load
- **146.3.9** Weighing scale at pack *(blocked)*
- **215.16** Online order billed in POS, automatically *(decision)*
- **214.2** A till left on a completed sale takes no scans when opened again
- **214.5** Scanning the same barcode a second time adds 2 units
- **214.3** Returns › Refund says "No refund is pending" once there are over 200 refunds
- **215.19** A B2B till bill gets an IRN
- **176.7** Finite capacity scheduling *(blocked)*
- **214.4** Catalog reports file JSON event stamps under their UTC date
- **143.6** Barcode print sorted: select the GRN and print *(blocked)*
- **154.2** Fix: roll held straight, then Printer preset on Jewellery *(blocked)*
- **154.5** Printer preset: Turn (°) - straighten a print that comes out tilted on the tag *(blocked)*
- **184.1** A new sticker printer works without knowing its language *(blocked)*
- **194.1** Line up a sticker printer in 3 taps, by the user alone
- **194.3** A sticker printer manual the user can follow alone
- **203.1** Print asks once to install PrintBridge
- **203.6** Code-signing certificate, so Windows does not warn at the first download
- **209.1** "PrintBridge is already running" never pops up by itself
- **212.1** "Did not print" in seconds when PrintBridge is down
- **212.3** PrintBridge security review, and fixes
- **213.1** Change the print for this one document before it prints
- **213.3** Choose at save as well as at print
- **213.4** Reprint can switch too
- **213.5** One switch maps the rest *(decision)*
- **146.5.1** Shopify gets the approved PIM content *(blocked)*
- **146.4.1** A real Unicommerce integration *(blocked)*
- **146.5.2** Marketplace templates from their own file, per category, with validation *(blocked)*
- **185.3** Razorpay payment links *(blocked)*
- **205.2** Decisions for API access (D1-D14 in the plan) *(decision)*
- **205.3** Module scopes
- **205.4** Key owner and ceiling
- **205.6** Document-type exposure list
- **205.7** Records API
- **205.8** `changed_since` on the shared document list
- **205.9** `/records/{doctype}/schema` and `/me`
- **205.10** API access list
- **205.11** New API access page
- **205.12** Created page (shown once)
- **205.13** Key detail page
- **205.14** Actions API
- **205.15** Reports API
- **205.16** Webhooks for every module
- **205.17** OpenAPI 3.1 per company and per key
- **205.18** Docs viewer
- **205.19** Sharing
- **205.20** Rotation overlap and expiry reminders
- **205.21** Failure alert with one-click fix
- **205.22** Clear errors
- **205.23** Tests
- **205.24** Guides and live check
- **211.2** Ask-207 loose ends: docs check clean, route security list refreshed now
- **162.17** MFA tiers: when to require? *(decision)*
- **206.7** A wrong current password signs the user out
- **162.16** Cross-origin write check: when to enforce? *(decision)*
- **206.1** Sign-in and session refusals stay out of System Errors
- **206.2** System Errors lists real faults, grouped
- **206.3** Resolve and Copy for fix
- **206.4** Real faults reach the fix queue
- **162.5** Meet the browser and server budgets
- **177.1** The full test suite runs on GitHub's machines *(blocked)*
- **177.2** The Release click stops running the tests on the laptop *(blocked)*
- **177.3** Deploy waits for the GitHub run *(blocked)*
- **177.4** ci.yml has been red on every push *(blocked)*
- **177.5** The click's own gates must take about a minute *(blocked)*
- **177.7** Guard tests first
- **219.1** Push and deploy everything in the tree
- **158.2** Is `OPS_ALERT_WEBHOOK_URL` set on production? *(decision)*
- **171.4** Scratch databases of finished sessions *(blocked)*
- **214.1** Guide `journey`: the whole store journey, watched and recorded at human speed
- **143.3** The same file on minn *(decision)*
- **195.2** Guide: no "--no-sandbox" banner; key kept masked
- **217.1** Knowledge Center size budget is nearly full *(decision)*
