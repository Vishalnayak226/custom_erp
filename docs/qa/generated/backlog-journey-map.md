# Backlog Journey Map

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: `docs/micro_checklist.md` open items, `docs/product/erp-build-checklist.md` JRN table
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from `docs/micro_checklist.md` open items, `docs/product/erp-build-checklist.md` JRN table, for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

BLD-002: the 341 open checklist items reconciled into the business journeys they serve. Acceptance environment for every journey: Chromium at three viewports, en-IN, tenant tiers Small / Mid / Large (BLD-002 §9). Reference configurations: REF-RETAIL-IN (PIM, OMS, CRM) and REF-WAREHOUSE-IN (WMS, procurement, OMS); a journey in neither is deferred until a customer needs it, which is its exclusion rationale. Item IDs are kept as they are in the checklist.

| Journey | Reference configuration | Owner | Open | Needs decision | Blocked |
|---|---|---|---:|---:|---:|
| JRN-01 POS/store | REF-RETAIL-IN | Store/finance; 47.2–47.4/47.6, Stage 31 | 9 | 2 | 0 |
| JRN-02 OMS/order-to-cash | REF-RETAIL-IN, REF-WAREHOUSE-IN | OMS/operations; Stage 35 | 2 | 0 | 1 |
| JRN-03 returns/refunds | REF-RETAIL-IN | Finance/store; 47.3/49.5 | 8 | 1 | 0 |
| JRN-04 inventory | REF-RETAIL-IN, REF-WAREHOUSE-IN | Inventory controller; Stage 36/47.5 | 19 | 0 | 1 |
| JRN-05 WMS | REF-WAREHOUSE-IN | Warehouse owner; Stage 36/47.6 | 0 | 0 | 0 |
| JRN-06 warehouse billing/3PL | REF-WAREHOUSE-IN | Warehouse/finance; 47.5; mixed owner excluded | 0 | 0 | 0 |
| JRN-07 source-to-pay | REF-WAREHOUSE-IN | Buyer/AP; Stage 37/procurement | 12 | 0 | 0 |
| JRN-08 PIM/catalog/DAM | REF-RETAIL-IN | Product data steward; 26.4/47.13 | 6 | 0 | 1 |
| JRN-09 record-to-report | REF-RETAIL-IN, REF-WAREHOUSE-IN | Qualified accountant; 26.6/Stage 37 | 8 | 0 | 0 |
| JRN-10 FX/intercompany/consolidation | deferred - outside both | Finance policy owner; Stage 37; depth decision DEC-01 | 0 | 0 | 0 |
| JRN-11 revenue schedules | deferred - outside both | Accountant; existing contract engine; separate from SaaS billing | 0 | 0 | 0 |
| JRN-12 India tax | REF-RETAIL-IN, REF-WAREHOUSE-IN | Tax/legal owner; 26.2/26.6/47.16 | 4 | 0 | 0 |
| JRN-13 manufacturing | deferred - outside both | Factory/cost owner; 26.9/Stage 37 | 0 | 0 | 0 |
| JRN-14 quality/maintenance | deferred - outside both | Quality/maintenance owner; chosen industry scope | 0 | 0 | 0 |
| JRN-15 CRM/loyalty | REF-RETAIL-IN | Marketing/privacy/store; 26.7 | 2 | 0 | 2 |
| JRN-16 HR/payroll | deferred - outside both | Qualified HR/payroll owner; 26.8 | 0 | 0 | 0 |
| JRN-17 assets | deferred - outside both | Fixed-asset accountant; Stage 37 | 0 | 0 | 0 |
| JRN-18 expenses | deferred - outside both | Finance/employee owner; existing expenses scope | 0 | 0 | 0 |
| JRN-19 projects/service | deferred - outside both | Service/project owner; Experimental scope preserved | 0 | 0 | 0 |
| JRN-20 reports/analysis | REF-RETAIL-IN, REF-WAREHOUSE-IN | Data/finance/QA; 26.10/47.10 | 1 | 0 | 0 |
| JRN-21 platform/integrations | REF-RETAIL-IN, REF-WAREHOUSE-IN | Integrator/security; Stage 38/49.8 | 22 | 3 | 0 |
| JRN-22 identity/approval/audit | REF-RETAIL-IN, REF-WAREHOUSE-IN | Tenant admin/security; 47.1/47.7/Stage 49 | 93 | 14 | 0 |
| JRN-23 knowledge/implementation | REF-RETAIL-IN, REF-WAREHOUSE-IN | Documentation/implementation; Stage 39/47.15/48.7 | 34 | 0 | 1 |
| Cross-journey acceptance and audits | - | - | 70 | 2 | 8 |
| Engineering housekeeping | - | - | 15 | 3 | 0 |
| Unmapped - place these stages | - | - | 36 | 0 | 0 |

**Unmapped:** Stage 140 (28), Stage 142 (4), Stage 143 (4) - add each to `stageJourney` in `cmd/gendocs/journeys.go`.

## JRN-01 POS/store

- **31.1.8** Live verification against a real QZ Tray and a real printer — partially closed 2026-0...
- **31.1.9** Replace QZ Tray with our own lightweight agent (PrintBridge) — Windows deployable; ma... *(decision)*
- **47.6** Real mobile/RF WMS/POS shell and physical reference journey
- **53.15** Deploy
- **53.16** Found live, not caused by this Stage, needs a product decision: the initial-JS release... *(decision)*
- **102.1.2** Camera scanning
- **102.1.3** Push alerts
- **102.4.2** Basket to the till
- **102.5.2** Real phones

## JRN-02 OMS/order-to-cash

- **99.8.1** End to end on a real Shopify development store *(blocked)*
- **99.9.1** Guides and release gate

## JRN-03 returns/refunds

- **49.5** Server-authoritative business controls, fraud resistance and invariant security
- **49.5.1** Command catalog
- **49.5.2** Close Stage 47 atomicity work
- **49.5.3** Workflow transition enforcement
- **49.5.4** Abuse/fraud patterns
- **49.5.5** Trusted totals and reconciliation
- **49.5.6** Safe overrides *(decision)*
- **49.5.7** Adversarial invariant tests

## JRN-04 inventory

- **58.7** Physical printer check
- **106.1** Tail tag 100 × 14.3 preset
- **106.2** Square and Ring label designs from the client's old PRN files
- **106.3** POS bill print format from the client's video
- **106.4** Stock Locations > Transactions: every row editable
- **106.5** From / To stock location at the top of every transaction
- **113.3** A ready design for the Seznik 100 × 14.3 tag
- **113.4** Seznik tag is a 3-up roll, tags standing vertically
- **119.1** Four Seznik built-ins: horizontal × 1, horizontal × 3, vertical × 1, vertical × 3
- **119.2** Print direction on every template
- **119.3** Labels down
- **119.4** A clean browser print
- **119.5** Remember the template for a product type from the Print screen
- **119.6** Guide: silent printing and where each print setting lives
- **121.1** "GRN 7" not in Print stickers' Document list *(blocked)*
- **137.3** Alignment set once per printer
- **137.4** One PrintBridge installer from the ERP
- **137.5** PrintBridge wakes with the ERP (after installation)
- **137.6** Quit from the tray comes back after a restart

## JRN-07 source-to-pay

- **71.7** Modules the pack needs that the ERP does not have
- **78.4** Release of this session's work
- **84.5** Local test data from the walk
- **94.6** MRP per batch at the POS
- **94.12** Release
- **94.22** New PO: an existing plain SKU went on without its colour / size matrix
- **94.25** New PO: Add put typed text on as a line without picking or creating an item
- **94.26** New PO: the combinations table left out the design-level attributes
- **94.28** New PO: the item entry as one compact table - parent row, then variant rows
- **98.2.2** Page size and pictures kept
- **98.5.2** Walk-through
- **129.5** Docs, tests, live check

## JRN-08 PIM/catalog/DAM

- **47.13** Master-data migration, stewardship and legal product attributes
- **81.7** Later, if wanted
- **89.4.4** Hidden fields leave the forms, data kept *(blocked)*
- **89.6.3** Walk-through
- **95.0** Decisions
- **95.9** Design and Combination folders in the Image drive settings

## JRN-09 record-to-report

- **93.1** A new location gets its cost center
- **93.3** Postings carry the cost center
- **93.6** Cost Center is mandatory on a location, and suggested
- **93.7** Legal Entity is mandatory on a location; postings carry the entity
- **93.8** GSTIN is mandatory on a legal entity
- **93.9** Every person has a department, from day one
- **93.10** Postings carry the department
- **93.11** Old long codes are cleaned once

## JRN-12 India tax

- **47.16** India-first data lifecycle and compliance-control implementation
- **66.7** Rates for the six codes shipped without one
- **66.8** Back-fill the HSN Code list for clients whose industry was set before Stage 66
- **66.9** Confirm every live client has its industry set

## JRN-15 CRM/loyalty

- **101.3.1** Senders *(blocked)*
- **131.2** Settings → Platform → Data Console does nothing on live *(blocked)*

## JRN-20 reports/analysis

- **47.10** Bound queries, tables, search and browser work

## JRN-21 platform/integrations

- **49.8** Files, imports/exports, webhooks, connectors, jobs and outbound-side-effect security
- **49.8.2** Import/export safety
- **49.8.5** Connector credential and scope
- **49.8.6** Job/outbox boundary
- **49.8.7** Egress and external-side-effect control *(decision)*
- **49.8.8** Integration abuse lab
- **56.1** Boot on Railway with the security baseline in warn mode
- **56.2** SB-015 — encrypted database link
- **56.3** SB-012 — proxy trust against Railway's edge
- **56.4** Volume for `media_store`
- **56.5** Flip `ENV=production` and confirm a clean boot
- **56.6** Prove multi-tenant hosting end to end — this is the item that makes the money
- **56.7** Measure the meter for 5 days
- **56.8** Measure India latency
- **56.10** Stand up the backup job itself on Railway
- **56.12** Cutover — only if 56.11 says go
- **56.13** Docs
- **90.1** The rule and the stamp
- **90.3** Map 840909b
- **90.5** The Releases note says what changed
- **90.6** More interesting, precise text; the old rows rewritten *(decision)*
- **109.12** Smart cleanup bot every 10 days *(decision)*

## JRN-22 identity/approval/audit

- **47.1** Deny-by-default authorization, privacy and segregation of duties
- **47.8** Session containment and XSS-resistant browser boundary
- **49.0** Security charter, threat model and accountable risk register
- **49.0.6** Risk register and acceptance authority *(decision)*
- **49.2** Identity, authentication, recovery and machine identity
- **49.2.1** Identity lifecycle *(decision)*
- **49.2.3** MFA assurance tiers
- **49.2.6** Machine/service identities
- **49.2.7** OAuth2/API-key convergence
- **49.3** Deny-by-default authorization, tenant isolation and segregation of duties
- **49.3.1** One authorization contract
- **49.3.2** No confused-deputy path *(decision)*
- **49.3.3** Database isolation defense
- **49.3.4** Object/field/action negative matrix
- **49.3.5** SoD and maker-checker invariants
- **49.3.6** Privilege-change safety
- **49.3.7** Tenant migration and customization proof
- **49.3.8** Generated authorization tests
- **49.4** Browser, session, API and request-boundary hardening
- **49.4.1** Session topology ADR
- **49.4.2** CSRF/cross-origin boundary
- **49.4.3** XSS and content boundary
- **49.4.6** Canonical input validation
- **49.4.7** API misuse controls
- **49.4.8** Browser/API security regression suite
- **49.6** Data classification, privacy, cryptography, keys and secrets
- **49.6.2** Data minimization
- **49.6.3** Encryption in transit *(decision)*
- **49.6.4** Encryption at rest and field-level decision *(decision)*
- **49.6.6** Secret lifecycle
- **49.6.7** Safe telemetry and support *(decision)*
- **49.6.8** Privacy rights and lifecycle tests *(decision)*
- **49.7** Database, host, network and deployment hardening
- **49.7.7** Patch and configuration cadence *(decision)*
- **49.7.8** Hardened-deployment test
- **49.10** Layered security verification and break-the-ERP program
- **49.10.3** Full authorization/tenant harness
- **49.10.4** Authenticated application/API DAST
- **49.10.5** Resource-exhaustion tests
- **49.10.6** Insider/red-team scenarios
- **49.10.7** Independent penetration test and retest
- **49.10.8** Finding discipline *(decision)*
- **49.11** Security events, tamper evidence, detection and response-ready observability
- **49.11.1** Event taxonomy
- **49.11.2** High-quality event schema
- **49.11.4** Actionable detections
- **49.11.5** Alert engineering
- **49.11.6** Optional export adapter
- **49.11.7** Security operations views
- **49.12** Incident response, forensic readiness, breach workflow and safe recovery
- **49.12.1** Incident taxonomy and authority
- **49.12.3** Emergency controls
- **49.12.4** Forensic readiness
- **49.12.5** Notification decision workflow
- **49.12.8** Tabletop and live drills
- **49.13** Availability, abuse resistance, backup integrity and ransomware resilience
- **49.13.1** Availability threat/SLO model
- **49.13.2** Bounded admission control
- **49.13.3** Query/storage safety
- **49.13.4** Backup threat model
- **49.13.5** Restore is a security control
- **49.13.6** Ransomware/destructive-admin drill
- **49.13.7** Graceful dependency failure
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
- **49.18** Security weight, performance and storage budget ledger
- **49.18.1** Per-control before/after measurement:
- **49.18.2** Bounded state everywhere
- **49.18.7** Multi-year forecast and cleanup drill
- **92.1** The submitter may approve or reject their own document
- **92.2** Flows whose whole point is a second admin keep it, whatever the setting:
- **92.3** My work → Approvals shows you your own submissions
- **92.4** Send to Vendor never needs a vendor email

## JRN-23 knowledge/implementation

- **47.15** Executable documentation and in-product learning
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
- **48.7** Canonical user Knowledge Center and generated manuals
- **48.8** Engineering, operations, implementation, QA and record boundaries
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
- **83.2** Working-store switcher in the top bar
- **91.4** Run it on app.wholeops.in and minn.wholeops.in *(blocked)*
- **124.1** PO setup guide
- **138.1** Remove the product switcher

## Cross-journey acceptance and audits

- **20.30** e-invoice/IRN
- **20.31** e-way bill
- **47.0** Audit reproduction, support boundary and mutation map
- **47.9** Representative-volume performance laboratory and enforced budgets
- **47.11** Graceful request control, worker ownership and side-effect safety
- **47.12** Scope-led solo onboarding and evidence-based readiness
- **47.12.5** Bootstrap governance
- **47.14** Human-language, state clarity and action-oriented product shell
- **47.17** Reference-journey assurance and release evidence pack
- **47.18** Server-space and dependency budget ledger
- **50.11** Verification that can support a release
- **50.12** SaaS and separately sellable modules
- **50.13** UI and accessibility acceptance
- **50.14** Lightweight server and frontend proof
- **50.15** Recovery, operations, privacy and security acceptance
- **50.16** Finish existing Stage 48
- **50.17** Validate implemented ERP breadth end to end
- **50.18** Decide optional domain expansion
- **50.19** Scoped release and customer handoff
- **55.0F** Functional audit and repair gate
- **55.2** Shared shell/list/form/Help contract
- **55.3** Core/master data/industry setup
- **55.5a** Cashier access breadth and return segregation of duties
- **55.9** OMS/marketplace/delivery
- **55.10** Finance/close
- **55.11** PIM/catalog
- **55.14** HR/payroll
- **55.18** Service/projects
- **55.19** Stickers/printing
- **55.21** Integrations/admin/ops
- **55.22** Cross-module acceptance
- **57.12** Full-ERP walkthrough video with segment-level re-recording
- **57.12a** SOP videos to the client - kept open for the end (user, 2026-10-17)
- **57.26** Open observations from the SOP recording - each needs a decision before any change *(decision)*
- **73.0.1** Policy decisions D01-D12
- **73.0.2** One capability register, not two *(decision)*
- **73.4.1** Procurement:
- **73.4.2** OMS / logistics:
- **73.4.3** Finance:
- **73.4.4** PIM / merchandising:
- **73.4.5** Manufacturing / quality:
- **73.4.6** Assets:
- **73.4.7** HR / HCM:
- **73.4.8** Expenses / projects / service:
- **73.4.9** Workflow / platform / integration:
- **73.4.10** India / data:
- **73.B.11** Physical RF acceptance
- **73.B.13** Local dev data cleanup
- **73.C.5** Remove the throwaway client `batchc55f`
- **73.C.13** Stage 55 Done-bar walks still open in batch C's items:
- **74.0.1** Decisions DEC-01-DEC-22
- **74.1.1** Reason codes: one governed catalogue, required on every exception
- **74.4.1** Eligibility engine *(blocked)*
- **74.4.2** Pre-submission validation *(blocked)*
- **74.4.3** Provider adapter on the existing pattern *(blocked)*
- **74.4.4** Status models kept apart from the document's own status *(blocked)*
- **74.4.5** Locks and corrections *(blocked)*
- **74.4.7** GST Compliance workbench and reconciliation *(blocked)*
- **74.5.7** Pilot gates *(blocked)*
- **74.U.1** Carry the BRD's §10 cases into the UAT run sheet as each module ships:
- **74.U.2** Add E2E-01..16 to 47.17.1's reference journeys and `docs/operations/uat_run_sheet.md`:
- **134.9** Guides and handover under their budgets
- **134.11** Railway staging *(blocked)*
- **135.8** One connected organization, end to end
- **135.9** Phases P02-P12 stay with their owners
- **135.15** Knowledge Center: a bullet's wrapped line falls out of its list
- **135.24** A step GRN with a vendor credit note overpays by the note
- **135.25** One marketplace order can be settled under two settlement IDs
- **135.26** Two postings still commit apart from their document
- **136.3** Production link rejections: Vendor on Product Family, Legal Entity on Location

## Engineering housekeeping

- **103.1** Free space (no code change)
- **103.2** Root tidy *(decision)*
- **103.3** Code layout
- **103.4** Dead code and unused assets *(decision)*
- **103.5** Git housekeeping *(decision)*
- **103.6** Verify and record
- **132.9** Guide learns the whole ERP
- **132.11** Guide an ERP expert: business flows, numbers checked in screens, tables and reports
- **132.12** Guide expert in every module's setup and inside each module, and the help manual
- **132.14** Guide as the client set-up operator
- **132.15** What the ERP could add for Guide (to release)
- **132.16** Future: a text helper that sets a client up
- **132.22** Finished goods came into stock with no cost
- **132.3** Configuration screens
- **132.4** Whole ERP, every screen and form, each role; production read-only pass when the user says

## Unmapped

- **140.12.1** Till close leaves cash advances out of expected cash
- **140.12.2** X-report shows advance redemptions as "Gift Card"
- **140.12.3** A return against a credit invoice is valued at 0
- **140.12.4** A credit invoice moves no stock and books no cost of goods
- **140.12.5** Customer receipts always go to cash
- **140.12.6** Advances only at the till
- **140.12.7** The customer ledger tells half the story
- **140.12.8** Small sales gaps
- **140.12.9** A Cashier can post and settle sales invoices
- **140.12.10** A Cashier reads the whole trial balance through the report
- **140.12.11** A till's cash shortage or excess never reaches the GL
- **140.12.12** Accounts automation: card / UPI clearing
- **140.12.13** No Day Book and no Z-report
- **140.12.14** Period close does not look at tills
- **140.12.15** A wave-picked order cannot be packed or billed
- **140.12.16** Return check cannot find a shipped OMS order
- **140.12.17** An own-website order without a connector stays in Push problems
- **140.12.18** RTO and OMS returns need their own screens
- **140.12.19** Picked units show up again as "waiting for put-away"
- **140.12.20** Pickers do not see the rack
- **140.12.21** Warehouse setup friction
- **140.12.22** Ready-made Shopify, Amazon and Myntra templates
- **140.12.23** A template cannot use one field in two columns
- **140.12.24** Feed export limits
- **140.12.25** Merchandisers cannot set up PIM
- **140.12.26** WhatsApp and SMS are not ready on app
- **140.12.27** Decisions from customer data
- **140.13.1** Release, deploy, re-run Guide on app
- **142.1** An ecom order is always visible on Pending Orders
- **142.2** Load a pending order by scan on Pick and Pack
- **142.3** Sales from one Excel file (Sales Invoice → Upload from Excel)
- **142.4** Item discount on the New Sales Invoice screen
- **143.1** The file on app through Guide
- **143.2** Barcode print on app
- **143.3** The same file on minn
- **143.4** Every print page shows its preview and has one Print button
