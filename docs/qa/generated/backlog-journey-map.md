# Backlog Journey Map

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: `docs/micro_checklist.md` open items, `docs/product/erp-build-checklist.md` JRN table
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from `docs/micro_checklist.md` open items, `docs/product/erp-build-checklist.md` JRN table, for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

BLD-002: the 298 open checklist items reconciled into the business journeys they serve. Acceptance environment for every journey: Chromium at three viewports, en-IN, tenant tiers Small / Mid / Large (BLD-002 §9). Reference configurations: REF-RETAIL-IN (PIM, OMS, CRM) and REF-WAREHOUSE-IN (WMS, procurement, OMS); a journey in neither is deferred until a customer needs it, which is its exclusion rationale. Item IDs are kept as they are in the checklist.

| Journey | Reference configuration | Owner | Open | Needs decision | Blocked |
|---|---|---|---:|---:|---:|
| JRN-01 POS/store | REF-RETAIL-IN | Store/finance; 47.2–47.4/47.6, Stage 31 | 8 | 2 | 0 |
| JRN-02 OMS/order-to-cash | REF-RETAIL-IN, REF-WAREHOUSE-IN | OMS/operations; Stage 35 | 2 | 0 | 1 |
| JRN-03 returns/refunds | REF-RETAIL-IN | Finance/store; 47.3/49.5 | 4 | 2 | 0 |
| JRN-04 inventory | REF-RETAIL-IN, REF-WAREHOUSE-IN | Inventory controller; Stage 36/47.5 | 13 | 0 | 1 |
| JRN-05 WMS | REF-WAREHOUSE-IN | Warehouse owner; Stage 36/47.6 | 0 | 0 | 0 |
| JRN-06 warehouse billing/3PL | REF-WAREHOUSE-IN | Warehouse/finance; 47.5; mixed owner excluded | 0 | 0 | 0 |
| JRN-07 source-to-pay | REF-WAREHOUSE-IN | Buyer/AP; Stage 37/procurement | 4 | 0 | 0 |
| JRN-08 PIM/catalog/DAM | REF-RETAIL-IN | Product data steward; 26.4/47.13 | 1 | 0 | 0 |
| JRN-09 record-to-report | REF-RETAIL-IN, REF-WAREHOUSE-IN | Qualified accountant; 26.6/Stage 37 | 1 | 0 | 0 |
| JRN-10 FX/intercompany/consolidation | deferred - outside both | Finance policy owner; Stage 37; depth decision DEC-01 | 0 | 0 | 0 |
| JRN-11 revenue schedules | deferred - outside both | Accountant; existing contract engine; separate from SaaS billing | 0 | 0 | 0 |
| JRN-12 India tax | REF-RETAIL-IN, REF-WAREHOUSE-IN | Tax/legal owner; 26.2/26.6/47.16 | 3 | 0 | 0 |
| JRN-13 manufacturing | deferred - outside both | Factory/cost owner; 26.9/Stage 37 | 0 | 0 | 0 |
| JRN-14 quality/maintenance | deferred - outside both | Quality/maintenance owner; chosen industry scope | 0 | 0 | 0 |
| JRN-15 CRM/loyalty | REF-RETAIL-IN | Marketing/privacy/store; 26.7 | 2 | 0 | 2 |
| JRN-16 HR/payroll | deferred - outside both | Qualified HR/payroll owner; 26.8 | 0 | 0 | 0 |
| JRN-17 assets | deferred - outside both | Fixed-asset accountant; Stage 37 | 0 | 0 | 0 |
| JRN-18 expenses | deferred - outside both | Finance/employee owner; existing expenses scope | 0 | 0 | 0 |
| JRN-19 projects/service | deferred - outside both | Service/project owner; Experimental scope preserved | 0 | 0 | 0 |
| JRN-20 reports/analysis | REF-RETAIL-IN, REF-WAREHOUSE-IN | Data/finance/QA; 26.10/47.10 | 1 | 0 | 0 |
| JRN-21 platform/integrations | REF-RETAIL-IN, REF-WAREHOUSE-IN | Integrator/security; Stage 38/49.8 | 18 | 3 | 0 |
| JRN-22 identity/approval/audit | REF-RETAIL-IN, REF-WAREHOUSE-IN | Tenant admin/security; 47.1/47.7/Stage 49 | 69 | 14 | 0 |
| JRN-23 knowledge/implementation | REF-RETAIL-IN, REF-WAREHOUSE-IN | Documentation/implementation; Stage 39/47.15/48.7 | 33 | 0 | 1 |
| Cross-journey acceptance and audits | - | - | 65 | 2 | 10 |
| Engineering housekeeping | - | - | 9 | 1 | 0 |
| Unmapped - place these stages | - | - | 65 | 6 | 6 |

**Unmapped:** Stage 143 (2), Stage 146 (6), Stage 148 (1), Stage 149 (2), Stage 154 (3), Stage 158 (2), Stage 162 (3), Stage 164 (1), Stage 170 (42), Stage 171 (1), Stage 173 (1), Stage 174 (1) - add each to `stageJourney` in `cmd/gendocs/journeys.go`.

## JRN-01 POS/store

- **47.6** Real mobile/RF WMS/POS shell and physical reference journey
- **102.1.2** Camera scanning
- **102.1.3** Push alerts
- **102.5.2** Real phones
- **31.1.8** Live verification against a real QZ Tray and a real printer — partially closed 2026-0...
- **31.1.9** Replace QZ Tray with our own lightweight agent (PrintBridge) — Windows deployable; ma... *(decision)*
- **53.16** Found live, not caused by this Stage, needs a product decision: the initial-JS release... *(decision)*
- **53.15** Deploy

## JRN-02 OMS/order-to-cash

- **99.8.1** End to end on a real Shopify development store *(blocked)*
- **99.9.1** Guides and release gate

## JRN-03 returns/refunds

- **49.5** Server-authoritative business controls, fraud resistance and invariant security
- **49.5.5** Trusted totals and reconciliation
- **49.5.5b** Persist money / UOM / tax versions on posted lines? *(decision)*
- **49.5.6** Safe overrides *(decision)*

## JRN-04 inventory

- **106.5** From / To stock location at the top of every transaction
- **121.1** "GRN 7" not in Print stickers' Document list *(blocked)*
- **106.2** Square and Ring label designs from the client's old PRN files
- **113.3** A ready design for the Seznik 100 × 14.3 tag
- **119.1** Four Seznik built-ins: horizontal × 1, horizontal × 3, vertical × 1, vertical × 3
- **58.7** Physical printer check
- **119.4** A clean browser print
- **137.3** Alignment set once per printer
- **137.4** One PrintBridge installer from the ERP
- **137.5** PrintBridge wakes with the ERP (after installation)
- **137.6** Quit from the tray comes back after a restart
- **106.3** POS bill print format from the client's video
- **119.6** Guide: silent printing and where each print setting lives

## JRN-07 source-to-pay

- **129.5** Docs, tests, live check
- **98.5.2** Walk-through
- **84.5** Local test data from the walk
- **78.4** Release of this session's work

## JRN-08 PIM/catalog/DAM

- **47.13** Master-data migration, stewardship and legal product attributes

## JRN-09 record-to-report

- **93.11** Old long codes are cleaned once

## JRN-12 India tax

- **66.7** Rates for the six codes shipped without one
- **66.9** Confirm every live client has its industry set
- **47.16** India-first data lifecycle and compliance-control implementation

## JRN-15 CRM/loyalty

- **101.3.1** Senders *(blocked)*
- **131.2** Settings → Platform → Data Console does nothing on live *(blocked)*

## JRN-20 reports/analysis

- **47.10** Bound queries, tables, search and browser work

## JRN-21 platform/integrations

- **56.6** Prove multi-tenant hosting end to end — this is the item that makes the money
- **90.1** The rule and the stamp
- **90.3** Map 840909b
- **90.5** The Releases note says what changed
- **90.6** More interesting, precise text; the old rows rewritten *(decision)*
- **49.8** Files, imports/exports, webhooks, connectors, jobs and outbound-side-effect security
- **49.8.6** Job/outbox boundary
- **49.8.7** Egress and external-side-effect control *(decision)*
- **56.1** Boot on Railway with the security baseline in warn mode
- **56.2** SB-015 — encrypted database link
- **56.3** SB-012 — proxy trust against Railway's edge
- **56.5** Flip `ENV=production` and confirm a clean boot
- **56.7** Measure the meter for 5 days
- **56.8** Measure India latency
- **56.12** Cutover — only if 56.11 says go
- **109.12** Smart cleanup bot every 10 days *(decision)*
- **56.10** Stand up the backup job itself on Railway
- **56.13** Docs

## JRN-22 identity/approval/audit

- **49.2** Identity, authentication, recovery and machine identity
- **49.2.3** MFA assurance tiers
- **49.2.7** OAuth2/API-key convergence
- **47.1** Deny-by-default authorization, privacy and segregation of duties
- **49.3** Deny-by-default authorization, tenant isolation and segregation of duties
- **49.3.1** One authorization contract
- **49.3.2** No confused-deputy path *(decision)*
- **49.3.3** Database isolation defense
- **49.3.4** Object/field/action negative matrix *(decision)*
- **49.3.6** Privilege-change safety
- **49.3.7** Tenant migration and customization proof
- **47.8** Session containment and XSS-resistant browser boundary
- **49.4** Browser, session, API and request-boundary hardening
- **49.4.3** XSS and content boundary
- **49.4.6** Canonical input validation
- **49.4.8** Browser/API security regression suite
- **49.6** Data classification, privacy, cryptography, keys and secrets
- **49.6.3** Encryption in transit *(decision)*
- **49.6.4** Encryption at rest and field-level decision *(decision)*
- **49.6.7** Safe telemetry and support *(decision)*
- **49.6.8** Privacy rights and lifecycle tests *(decision)*
- **49.7** Database, host, network and deployment hardening
- **49.7.7** Patch and configuration cadence *(decision)*
- **49.7.8** Hardened-deployment test
- **49.10** Layered security verification and break-the-ERP program
- **49.10.4** Authenticated application/API DAST
- **49.10.6** Insider/red-team scenarios
- **49.10.7** Independent penetration test and retest
- **49.10.8** Finding discipline *(decision)*
- **49.13** Availability, abuse resistance, backup integrity and ransomware resilience
- **49.13.1** Availability threat/SLO model
- **49.13.4** Backup threat model
- **49.13.5** Restore is a security control
- **49.13.6** Ransomware/destructive-admin drill
- **49.11** Security events, tamper evidence, detection and response-ready observability
- **49.0** Security charter, threat model and accountable risk register
- **49.0.6** Risk register and acceptance authority *(decision)*
- **49.12** Incident response, forensic readiness, breach workflow and safe recovery
- **49.12.1** Incident taxonomy and authority
- **49.12.5** Notification decision workflow
- **49.12.8** Tabletop and live drills
- **49.14** Secure human operations, support, devices and anti-social-engineering controls
- **49.14.2** Privileged/admin UX
- **49.14.3** Floor/shared-device posture
- **49.14.5** Governed break-glass *(decision)*
- **49.14.6** Secure device/browser baseline *(decision)*
- **49.14.7** Role-specific practice *(decision)*
- **49.14.8** Personnel controls *(decision)*
- **49.15** Vulnerability disclosure, triage, remediation and security-update lifecycle
- **49.15.6** Emergency update drill
- **49.16** Privacy, legal, regulatory and customer security assurance
- **49.16.1** Applicability register
- **49.16.4** Contract truth gate
- **49.16.5** Certification decision by market value
- **49.17** Security release gate, scorecard and continuous improvement loop
- **49.17.1** Per-change security gate *(decision)*
- **49.17.2** Release-candidate gate
- **49.17.3** Configuration go-live gate
- **49.17.4** Security scorecard with evidence, not one magic number
- **49.17.5** Fixed operating cadence
- **49.17.6** Canary and rollback
- **49.17.7** Independent challenge
- **49.17.8** Honest disclosure and improvement
- **92.1** The submitter may approve or reject their own document
- **92.2** Flows whose whole point is a second admin keep it, whatever the setting:
- **92.3** My work → Approvals shows you your own submissions
- **49.18** Security weight, performance and storage budget ledger
- **49.18.1** Per-control before/after measurement:
- **49.18.7** Multi-year forecast and cleanup drill

## JRN-23 knowledge/implementation

- **91.4** Run it on app.wholeops.in and minn.wholeops.in *(blocked)*
- **138.1** Remove the product switcher
- **47.15** Executable documentation and in-product learning
- **124.1** PO setup guide
- **48.7** Canonical user Knowledge Center and generated manuals
- **48.0** Pure generators, recoverable baseline and complete document register
- **48.0.1** Fresh inventory in an isolated docs branch/worktree
- **48.2** Product truth: vision, supported configurations, capability and roadmap
- **48.2.1** Product vision and strategy
- **48.2.2** Supported-configurations matrix
- **48.2.3** Capability catalog
- **48.2.4** Outcome roadmap
- **48.2.5** Release notes/changelog series
- **48.3** Requirements suite: BRD, PRD, processes, NFRs and traceability
- **48.3.2** Personas/JTBD
- **48.3.3** Business-process catalog
- **48.4** Current architecture description and decisions
- **48.4.5** Reconcile old architecture documents
- **48.5** Data, MDM and API reference system
- **48.6** Security, privacy, compliance and legal document set
- **48.6.6** Legal document register
- **48.9** Controlled target-folder migration
- **48.9.1** Create folders only when first approved document exists:
- **48.9.2** Batch 1:
- **48.10** Evidence-preserving archive and explicit deletion gate
- **48.10.3** Per-file deletion proof:
- **48.10.4** Delete via explicit reviewed file list only
- **48.10.5** Repository/server cleanup:
- **48.11** Documentation CI, health reporting and completion gate
- **48.11.4** Quarterly sampled walkthroughs
- **48.11.5** Completion metrics:
- **48.11.6** Update standing repository conventions
- **48.8** Engineering, operations, implementation, QA and record boundaries

## Cross-journey acceptance and audits

- **55.11** PIM/catalog
- **73.B.11** Physical RF acceptance
- **55.9** OMS/marketplace/delivery *(blocked)*
- **73.4.2** OMS / logistics:
- **73.4.2.8** Route / load planning
- **55.10** Finance/close
- **73.4.3** Finance:
- **73.4.8** Expenses / projects / service:
- **20.30** e-invoice/IRN
- **74.4.1** Eligibility engine *(blocked)*
- **74.4.2** Pre-submission validation *(blocked)*
- **74.4.3** Provider adapter on the existing pattern *(blocked)*
- **74.4.4** Status models kept apart from the document's own status *(blocked)*
- **74.4.5** Locks and corrections *(blocked)*
- **20.31** e-way bill
- **73.4.10** India / data:
- **74.4.7** GST Compliance workbench and reconciliation *(blocked)*
- **73.4.7** HR / HCM:
- **55.14** HR/payroll
- **73.4.5** Manufacturing / quality:
- **73.4.6** Assets:
- **55.18** Service/projects *(blocked)*
- **55.19** Stickers/printing
- **135.25** One marketplace order can be settled under two settlement IDs
- **55.21** Integrations/admin/ops
- **73.B.13** Local dev data cleanup
- **73.C.5** Remove the throwaway client `batchc55f`
- **47.12** Scope-led solo onboarding and evidence-based readiness
- **47.12.5** Bootstrap governance
- **50.12** SaaS and separately sellable modules
- **55.3** Core/master data/industry setup
- **55.5a** Cashier access breadth and return segregation of duties
- **73.4.9** Workflow / platform / integration:
- **47.11** Graceful request control, worker ownership and side-effect safety
- **135.26** Two postings still commit apart from their document
- **47.9** Representative-volume performance laboratory and enforced budgets
- **47.18** Server-space and dependency budget ledger
- **50.14** Lightweight server and frontend proof
- **55.2** Shared shell/list/form/Help contract
- **47.14** Human-language, state clarity and action-oriented product shell
- **50.13** UI and accessibility acceptance
- **50.19** Scoped release and customer handoff
- **134.11** Railway staging *(blocked)*
- **50.15** Recovery, operations, privacy and security acceptance
- **50.11** Verification that can support a release
- **74.U.1** Carry the BRD's §10 cases into the UAT run sheet as each module ships:
- **74.U.2** Add E2E-01..16 to 47.17.1's reference journeys and `docs/operations/uat_run_sheet.md`:
- **135.8** One connected organization, end to end
- **47.0** Audit reproduction, support boundary and mutation map
- **47.17** Reference-journey assurance and release evidence pack
- **50.17** Validate implemented ERP breadth end to end
- **50.18** Decide optional domain expansion
- **55.0F** Functional audit and repair gate
- **55.22** Cross-module acceptance
- **57.26** Open observations from the SOP recording - each needs a decision before any change *(decision)*
- **73.C.13** Stage 55 Done-bar walks still open in batch C's items:
- **74.5.7** Pilot gates *(blocked)*
- **57.12** Full-ERP walkthrough video with segment-level re-recording
- **57.12a** SOP videos to the client - kept open for the end (user, 2026-10-17)
- **135.15** Knowledge Center: a bullet's wrapped line falls out of its list
- **50.16** Finish existing Stage 48
- **73.0.1** Policy decisions D01-D12
- **73.0.2** One capability register, not two *(decision)*
- **74.0.1** Decisions DEC-01-DEC-22
- **135.9** Phases P02-P12 stay with their owners

## Engineering housekeeping

- **103.4.1** Rest of the dead code *(decision)*
- **132.9** Guide learns the whole ERP
- **132.11** Guide an ERP expert: business flows, numbers checked in screens, tables and reports
- **132.12** Guide expert in every module's setup and inside each module, and the help manual
- **132.14** Guide as the client set-up operator
- **132.15** What the ERP could add for Guide (to release)
- **132.16** Future: a text helper that sets a client up
- **132.23** Guide tests GRN printing
- **132.4** Whole ERP, every screen and form, each role; production read-only pass when the user says

## Unmapped

- **146.5.12** Typed product relationships
- **149.2** PIM ▸ Workbench does not load
- **173.1** A step receipt's bill rounding leaves inventory valuation a few paise off *(decision)*
- **146.3.10** License plates at receipt *(blocked)*
- **146.3.9** Weighing scale at pack *(blocked)*
- **148.4b** Remap receivable and payable by group? *(blocked)*
- **170.1** A GL line names its source line
- **170.2** Part-match a bank line
- **170.3** Bank statement file import
- **170.4** Bank acknowledgement file
- **170.29** Expense limits by category
- **170.30** Employee advances settled against claims
- **170.35** GSTR-1 JSON
- **170.36** GSTR-2B import and match
- **170.37** Exports under LUT
- **170.17** Positions and reporting lines
- **170.18** Recruitment
- **170.19** Login at onboarding, clearance before exit
- **170.20** Disciplinary case
- **170.21** Skills and certificate expiry
- **170.22** Appraisal calibration
- **170.28** Holiday calendar and weekly offs
- **170.23** Salary TDS by slab, LWF, PF ECR and Form 24Q
- **170.24** Benefits
- **170.25** Salary revision through approval
- **170.26** My team for managers
- **170.27** People analytics with privacy
- **170.10** Operation confirmation with quantities and time
- **170.11** Scrap and rework move stock and cost
- **170.14** Backward lot trace
- **170.12** By-product cost share
- **170.13** WIP accounting and variance
- **170.5** Leases (IND AS 116)
- **170.6** Capital work in progress
- **170.7** Impairment and revaluation
- **170.8** Maintenance order cost
- **170.9** Meter-based maintenance
- **170.31** Timesheets cost the project
- **170.32** Project milestones and budget
- **170.33** Project revenue recognition
- **170.34** Service ticket SLA timers and escalation
- **170.15** AQL sampling
- **170.16** Supplier quality: SCAR and defects per million
- **143.6** Barcode print sorted: select the GRN and print
- **154.2** Fix: roll held straight, then Printer preset on Jewellery
- **154.4** Same settings on minn
- **154.5** Printer preset: Turn (°) - straighten a print that comes out tilted on the tag *(blocked)*
- **146.5.1** Shopify gets the approved PIM content
- **146.4.1** A real Unicommerce integration *(blocked)*
- **146.5.2** Marketplace templates from their own file, per category, with validation
- **170.40** Masked copy of a client
- **170.42** Import load order
- **170.41** Data-quality queue
- **162.17** MFA tiers: when to require? *(decision)*
- **162.16** Cross-origin write check: when to enforce? *(decision)*
- **170.39** Privacy rights for employees and vendors
- **170.38** Retention per record type
- **162.5** Meet the browser and server budgets
- **164.4** Split `app.js` for a smaller first paint
- **149.1** Remove every descriptive / instructional blurb from dialogs, panels and section intros,...
- **158.4** Clicked end-to-end checks are not a release gate *(decision)*
- **174.1** Release and deploy everything in the tree
- **158.2** Is `OPS_ALERT_WEBHOOK_URL` set on production? *(decision)*
- **171.4** Scratch databases of finished sessions *(blocked)*
- **143.3** The same file on minn *(decision)*
