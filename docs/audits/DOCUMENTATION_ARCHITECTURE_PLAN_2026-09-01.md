# ERP documentation architecture and reorganization plan — 2026-09-01

**Status:** proposed documentation governance, rebuild, migration, archive, and deletion plan. No document has been moved, rebuilt, archived, or deleted as part of this planning pass.

**Snapshot:** repository state was changing concurrently during this assessment. The inventory covers the documentation tree visible on 2026-09-01; implementation must begin from a fresh inventory after active Stage 37 work settles.

**Related evidence:** [ERP deep persona audit](ERP_DEEP_PERSONA_AUDIT_2026-09-01.md) and [lightweight smoothness plan](LIGHTWEIGHT_SMOOTHNESS_PLAN_2026-09-01.md).

## 1. Executive decision

The repository does not primarily suffer from too few documents. It suffers from **unclear document authority**.

There are good BRD, PRD, architecture, operations, user-guide, SOP, KB, audit, tracker, and generated-reference beginnings. But living requirements, current-state snapshots, aspirational plans, generated output, immutable evidence, and historical records sit next to one another without a universal owner/status/validity model. A reader cannot reliably tell which one is normative, current, proposed, generated, superseded, or merely historically interesting.

The correct program is therefore:

1. Establish governance and a document register before moving folders.
2. Rebuild the high-authority core—BRD, PRD, supported-product statement, non-functional requirements, process catalog, architecture overview, and traceability—before polishing secondary material.
3. Separate living documents, generated references, operational records, audits, templates, and archives.
4. Make the embedded Knowledge Center the canonical user-help source; generate printable manuals from it instead of hand-maintaining parallel prose.
5. Fix generator safety and link portability before path migration.
6. Move documents in small `git mv` batches with redirects/stubs and link checks.
7. Delete only reproducible generated duplicates or expired transition stubs. Preserve audit, legal, UAT, restore, incident, and decision evidence.

This should remain a plain-Markdown docs-as-code system. Do **not** introduce Docusaurus, MkDocs, a documentation database, a Node build, or another hosted service. The production server should continue to embed only the Knowledge Center output it actually serves.

## 2. Current-state inventory

### 2.1 Footprint

| Item | Current result |
|---|---:|
| Files under `docs/` | 98 |
| Markdown documents | 78 |
| Top-level documentation groups | 11 including root |
| Total `docs/` footprint | 6,131,830 bytes |
| Markdown footprint | 2,755,161 bytes |
| Screenshot footprint | 3,189,908 bytes across 13 PNGs |
| Generated embedded KB output | 476,610 bytes across 26 files |
| Markdown with YAML front matter | 24, primarily KB articles |
| Documents with a status near the top | 14 |
| Documents with an explicit owner near the top | 0 |
| Documents with an explicit audience near the top | 24 |
| Documents with a top-level last-verified field | 1 outside the KB metadata pattern |
| Non-portable `file:///` Markdown links | 31, concentrated in `ai_handover.md` |

The repository size is not the problem. The embedded KB is under 0.5 MB and should remain modest even after the missing handbooks are written. The risk is cognitive and governance weight, not disk usage.

### 2.2 What is already strong

- A central [documentation index](../README.md) exists and is readable.
- BRD and PRD are separated conceptually into “why” and “what”.
- Live backlog, build ledger, and handover have explicit standing conventions.
- Closed tracker history is already separated into archives.
- Knowledge Center articles already have structured front matter and a generator/check workflow.
- Error codes, report catalog, permission matrix, OpenAPI, project brain, KB HTML, and screenshots have some generated-source discipline.
- Operations already include backup/restore, restore evidence, incident response, go-live, UAT, hypercare, connector verification, and pentest scope.
- Point-in-time audits record real defects instead of erasing uncomfortable history.

These are assets to reorganize, not reasons for a rewrite.

## 3. Principal documentation loopholes

### DOC-01 — BRD and PRD contain obsolete current-state assertions

**Priority:** P0 documentation integrity.

The BRD and PRD were last committed on 2026-07-21. They still say full bin/putaway/pick-pack WMS, many industry profiles, notification depth, report depth, and e-invoice/e-way functionality are unbuilt or narrowly scoped. Later stages materially changed several of those areas. More importantly, the new persona audit proved some older “BUILT” control assertions unsafe—for example, server-side RBAC and POS/return integrity.

A BRD should not carry volatile implementation status. A PRD may state target requirements and release scope, but should link to a separately governed capability/support register for actual availability.

**Plan:** rebuild both with stable business/product content, requirement IDs, owners, approval history, assumptions, out-of-scope statements, and traceability. Remove mutable “BUILT/PARTIAL/SPEC” claims from prose unless they are generated from a capability register.

### DOC-02 — Multiple files answer “what is built” differently

**Priority:** P0 documentation integrity.

`ERP_BLUEPRINT.md`, `micro_checklist.md`, `project_ledger.md`, `PRD.md`, `modules_overview.md`, parity plans, maturity plans, old audits, and guides all contain current-state statements. Some are dated snapshots, some are task trackers, some are target designs, and some claim resolved risks that the 2026-09-01 live audit reproduced.

**Plan:** one authority per question; see §8. The capability/support register becomes the customer/product availability source. The task tracker records work state. Audits remain immutable evidence. Requirements state intended behavior. None impersonates another.

### DOC-03 — Historical audits look like current assurance

**Priority:** P1.

`ERP_LOOPHOLES_ANALYSIS.md`, `QC_EXHAUSTIVE_REPORT.md`, `UX_MANUAL_AUDIT.md`, and `DURABILITY_AUDIT_2026-07-31.md` are valuable point-in-time evidence but sit at `docs/` root. The loopholes document contains many “FIXED” banners, while the later audit found critical new or remaining issues. A reader can easily treat an old assurance report as the current security position.

**Plan:** move all point-in-time assessments under `assurance/audits/YYYY/`; add immutable metadata for audit date, commit, scope, method, author/reviewer, limitations, and `superseded_for_current_state_by`. Never rewrite old findings to match current reality.

### DOC-04 — Generated outputs have hidden cross-directory side effects

**Priority:** P0 documentation-tooling safety.

`docs/guides/update-guides.ps1 -Check` stages the guide output, but `cmd/gendocs` independently defaults its KB and OpenAPI outputs back into live repository directories. The check therefore is not a pure comparison. During this audit it attempted to write `docs/kb/troubleshooting/error-code-reference.md` and failed with Access Denied. `docs/brain/update-brain.ps1 -Check` also copies generated files back into the documentation tree even in check mode.

**Plan:** every generator writes all outputs under one supplied staging root; `-Check` never writes anywhere under the repository; a wrapper compares a declared output manifest; a normal update copies atomically only after every generation step succeeds. This must be fixed before any folder migration.

### DOC-05 — User guidance is maintained in overlapping forms

**Priority:** P1.

`USER_GUIDE`, `USER_SOP`, `ADMIN_GUIDE`, `ADMIN_SOP`, role journeys, getting-started articles, module handbooks, report catalogs, and error references overlap. Their audiences and purposes differ, but the repository does not define which is canonical and which is a projection. This guarantees semantic drift as the product grows.

**Plan:** the Knowledge Center becomes the topic-level source for tutorials, how-to tasks, explanations, troubleshooting, and reference. Long-form User/Admin manuals become generated curated assemblies. Customer-specific SOPs become templates with explicit local approval, not universal product truth.

### DOC-06 — A permission snapshot is presented like a universal permission model

**Priority:** P0 for onboarding/security documentation.

`PERMISSION_MATRIX.md` is generated from one tenant's current database grants and committed as a guide. Roles differ by tenant and can change after generation. The current live audit also showed dangerous Cashier access. A dated default-tenant snapshot must not be read as the normative security policy.

**Plan:** create a normative access-control/SoD policy and approved baseline role templates. Generate tenant-specific matrices on demand as evidence, tagged with tenant, environment, release, timestamp, and checksum. Do not commit a mutable live-tenant matrix as general user guidance.

### DOC-07 — The handover document has become a second system manual

**Priority:** P1.

`ai_handover.md` is 874 lines and contains absolute local links. Stable architecture/setup/run commands, volatile current-session state, and historical narrative are mixed. A new agent is instructed to read only one section because the document is too large.

**Plan:** split stable developer setup, architecture navigation, and operating commands into their canonical documents. Reduce handover to a generated or tightly maintained ≤150-line snapshot: release/commit, working-tree hazards, current objective, active services/ports, pending migrations, failing checks, and next safe action.

### DOC-08 — Naming and paths do not communicate lifecycle

**Priority:** P2.

Uppercase filenames, spaces, a capitalized `Contract/` folder, dates on some records but not others, and audits at root make linking and scanning harder. `file:///` links are machine-specific. Some relative links are broken after archival moves; others are custom KB slug links that a generic checker cannot interpret.

**Plan:** lowercase kebab-case for new living documents, ISO dates only for immutable records, no spaces, portable repo-relative links, and a link checker aware of both filesystem links and KB slug links.

### DOC-09 — Plans and reference research are stored as active specifications

**Priority:** P1.

`erp_maturity_master_plan.md`, `parity_master_plan.md`, `wms_parity_plan.md`, retired-project OMS/WMS reference notes, market-intelligence notes, and old PDF gap analysis all have useful research but overlap the live backlog and product roadmap.

**Plan:** extract still-open decisions and requirements into the canonical roadmap/PRD/module requirements; move benchmark material to `product/research/`; archive completed/superseded plans with no active-checkbox role.

### DOC-10 — Requirements are not traceable to design, tests, help, and release evidence

**Priority:** P1.

Stages and checklist numbers show build history, but they are not stable business/functional/non-functional requirement identifiers. There is no complete path from a business outcome to the feature requirement, control, architecture decision, route/record, automated test, user help, and release evidence.

**Plan:** introduce a small requirement-ID scheme and a generated traceability report; see §11.

### DOC-11 — Standard enterprise artifacts are missing or only implicit

**Priority:** P1/P2 by release claim.

Notably incomplete or absent: personas/jobs, supported-configuration matrix, business-process catalog, NFR/SLO specification, architecture views, domain/data dictionary, master-data governance, threat model, normative access/SoD policy, audit-trail design, privacy/data lifecycle, compliance applicability/control matrix, secure SDLC, release/versioning policy, dependency/license/SBOM policy, migration/cutover guide, support/service policy, accessibility standard/evidence, and a release evidence pack.

These should be created in priority order, not all at once.

### DOC-12 — Screenshots and visual proof have no reliable release contract

**Priority:** P1 for user/manual evidence.

The screenshot manifest identifies a date and server, but the capture harness can report success on unauthorized/error pages and preserve overlays across later captures. Images are over half of `docs/` storage and can become confidently wrong.

**Plan:** repair the browser contract before recapture; tag every screenshot set with product release, role, viewport, locale, theme, and data fixture. Keep only the current supported manual set in the living tree; archive signed/release evidence separately.

## 4. Standards and documentation model

This plan tailors established practices; it does not claim formal certification.

- [ISO/IEC/IEEE 29148:2018](https://www.iso.org/cms/%20render/live/en/sites/isoorg/contents/data/standard/07/20/72089.html) defines requirements-engineering information and lifecycle traceability. It was confirmed in 2024 and is scheduled for revision, so the document register should record the adopted edition and monitor replacement.
- [ISO/IEC/IEEE 42010:2022](https://www.iso.org/standard/74393.html) structures architecture descriptions around stakeholders, concerns, viewpoints, and models. This ERP needs a few useful views, not a massive architecture book.
- [NIST SP 800-218 SSDF 1.1](https://csrc.nist.gov/pubs/sp/800/218/final) provides outcome-based secure-development practices and evidence. NIST lists a newer revision as draft; use the final edition until the project deliberately adopts a final successor.
- [Diátaxis](https://diataxis.fr/_/downloads/en/latest/pdf/) separates user documentation into tutorials, how-to guides, reference, and explanation. This is the right remedy for the current Guide/SOP/KB overlap.
- The [C4 model](https://c4model.com/diagrams) supplies lightweight system-context, container, deployment, and only-when-useful component views.
- The [OpenAPI specification](https://spec.openapis.org/oas/latest.html) is the machine-readable HTTP contract; prose should explain policy and journeys rather than duplicate every field.
- [WCAG 2.2](https://www.w3.org/TR/WCAG22/) is the product/user-document accessibility target, including consistent help and accessible authentication.

### Tailoring rule

“Standard” does not mean creating every acronym as a separate file. This project does **not** need duplicate SRS, FRS, FSD, HLD, LLD, SDD, and TDD monoliths describing the same facts.

- BRD + PRD + module requirements + NFR + traceability satisfy the requirements-information need.
- Architecture overview + views + domain designs + ADRs satisfy high/low-level design needs.
- A feature design is created only for cross-domain, high-risk, irreversible, integration, security, accounting, or data-migration work.
- OpenAPI and generated data dictionaries are machine contracts; prose links to them.

## 5. Target information architecture

The final paths can be introduced gradually. The tree below is the target, not permission to move files immediately.

```text
docs/
├── README.md                         # audience/question-based portal
├── governance/
│   ├── documentation-standard.md
│   ├── document-register.md          # generated view of metadata
│   ├── ownership-raci.md
│   └── terminology-and-style.md
├── product/
│   ├── vision-and-strategy.md
│   ├── personas-and-jobs.md
│   ├── supported-configurations.md
│   ├── capability-catalog.md
│   ├── roadmap.md
│   ├── release-notes/
│   └── research/                     # competitor/benchmark/reference notes
├── requirements/
│   ├── brd.md
│   ├── prd.md
│   ├── non-functional-requirements.md
│   ├── business-process-catalog.md
│   ├── modules/                      # POS, finance, OMS, WMS, PIM, etc.
│   └── traceability-matrix.md        # generated view
├── architecture/
│   ├── overview.md
│   ├── system-context.md
│   ├── containers-and-deployment.md
│   ├── domain-map.md
│   ├── data-and-multitenancy.md
│   ├── integration-architecture.md
│   ├── runtime-and-jobs.md
│   └── decisions/                    # ADR-0001-*.md
├── data/
│   ├── master-data-governance.md
│   ├── data-dictionary.md            # generated where possible
│   ├── data-quality-and-lineage.md
│   ├── import-migration-standard.md
│   └── lifecycle-retention-archive.md
├── api/
│   ├── public-api.md
│   ├── authentication.md
│   ├── idempotency.md
│   ├── webhooks.md
│   ├── compatibility-and-deprecation.md
│   └── generated/openapi-public-v1.json
├── security/
│   ├── security-architecture.md
│   ├── threat-model.md
│   ├── access-control-and-sod.md
│   ├── audit-trail-and-security-logging.md
│   ├── secure-development.md
│   ├── vulnerability-management.md
│   ├── privacy-and-data-protection.md
│   └── compliance-control-matrix.md
├── engineering/
│   ├── developer-setup.md
│   ├── contributing.md
│   ├── coding-and-review-standard.md
│   ├── test-strategy.md
│   ├── database-migrations.md
│   ├── release-and-versioning.md
│   ├── dependency-and-license-policy.md
│   └── handover.md
├── operations/
│   ├── deployment.md
│   ├── configuration-reference.md
│   ├── observability-slos-capacity.md
│   ├── backup-restore-dr.md
│   ├── incident-response.md
│   ├── upgrade-rollback.md
│   └── runbooks/
├── implementation/
│   ├── discovery-and-scope.md
│   ├── tenant-onboarding.md
│   ├── configuration-workbook.md
│   ├── master-data-migration.md
│   ├── integration-and-device-readiness.md
│   ├── cutover-plan.md
│   ├── business-uat.md
│   ├── training-plan.md
│   └── hypercare.md
├── user/
│   ├── knowledge-base/               # source for embedded in-app help
│   │   ├── tutorials/
│   │   ├── how-to/
│   │   ├── explanation/
│   │   ├── reference/
│   │   └── troubleshooting/
│   ├── role-guides/
│   ├── module-handbooks/
│   ├── admin/
│   ├── sop-templates/
│   └── assets/screenshots/
├── qa/
│   ├── quality-strategy.md
│   ├── regression-and-test-matrix.md
│   ├── performance-test-plan.md
│   ├── accessibility-and-device-matrix.md
│   ├── security-test-plan.md
│   ├── test-data-standard.md
│   └── release-acceptance.md
├── assurance/
│   ├── audits/YYYY/
│   ├── release-evidence/YYYY/
│   ├── drills/YYYY/
│   ├── uat/YYYY/
│   └── penetration-tests/YYYY/
├── legal/
│   ├── README.md                     # counsel-owned document register
│   ├── templates/
│   └── notices/
├── project/
│   ├── active-backlog.md
│   ├── build-ledger.md
│   └── decisions-and-blockers.md
├── generated/
│   ├── brain/
│   └── reference/
└── archive/YYYY/
```

This is a classification system, not a requirement to create empty folders. A directory appears only when its first approved document exists.

## 6. Complete ERP document set and priority

### 6.1 Product and requirements

| Artifact | Why it exists | Current coverage | Priority/action |
|---|---|---|---|
| Product vision and strategy | Target customer, value, focus industries, principles, economic model | Scattered across BRD/architecture/plans | P1 create concise canonical document. |
| BRD | Business problem, outcomes, scope, stakeholders, constraints, success measures | Exists but stale | P0 rebuild. |
| PRD | Product behavior, personas, journeys, acceptance, dependencies, exclusions | Exists but stale/status-heavy | P0 rebuild core and split module depth. |
| Personas and jobs-to-be-done | CEO, cashier, floor worker, finance, admin, integrator needs | Mostly in audits/KB | P1 create from validated research; label assumptions. |
| Business-process catalog | Lead-to-cash, procure-to-pay, receive-to-ship, record-to-report, hire-to-retire, etc. | Implicit | P1 create with process owners and reference journeys. |
| Module requirement specifications | Stable FRs and acceptance by POS/OMS/WMS/PIM/finance/etc. | Mixed in PRD/specs/checklists | P1 build incrementally by supported configuration. |
| Non-functional requirements | Security, performance, availability, accessibility, retention, capacity, recovery, supportability | Scattered across audits/plans | P0 create measurable NFRs. |
| Supported configurations | Exact industries, modules, deployment, devices, integrations and limits supported | Missing | P0 create; prevents overclaiming. |
| Capability catalog | Experimental/Preview/Production/Certified availability with evidence | Status scattered everywhere | P0 create; single availability authority. |
| Product roadmap | Outcome-based future direction | Multiple overlapping plans | P1 consolidate; task details remain in backlog. |
| Release notes/changelog | What changed for customers/admins/integrators | Missing as a formal series | P1 create per release. |
| Requirements traceability | Goal → requirement → design → test → help → release | Missing | P1 generate. |

### 6.2 Architecture, data, and integration

| Artifact | Current coverage | Priority/action |
|---|---|---|
| Architecture overview | Several overlapping documents | P0 rebuild as current architecture only. |
| System context and container/deployment views | Textual descriptions; no stable stakeholder views | P1 create small C4-style views. |
| Domain map and ownership | Implied by engines/routes | P1 create; define authoritative command per domain event. |
| Data/multi-tenancy architecture | Partly covered | P0 update with current isolation, ownership, audit, and failure boundaries. |
| Data dictionary and record lifecycle | Metadata exists; no authoritative human/generated dictionary | P1 generate from metadata plus business definitions. |
| Master-data governance | Missing as a complete policy | P0/P1 create before self-onboarding claim. |
| Data quality, lineage, migration | Import docs exist, governance incomplete | P1 create. |
| Integration architecture | Scattered connector/outbox prose | P1 consolidate contracts, retries, idempotency, jobs, secrets. |
| Public API contract | Prose + generated OpenAPI exists | Keep, relocate after generator fix, add version/deprecation. |
| Webhook/job architecture | Planned/open | Create with implementation; do not publish aspirational behavior as current. |
| Architecture Decision Records | Decisions buried in ledger | P1 start ADRs prospectively; backfill only major active decisions. |

### 6.3 Security, privacy, compliance, and legal

| Artifact | Current coverage | Priority/action |
|---|---|---|
| Security architecture | Scattered, some assertions contradicted by audit | P0 rebuild after authorization design is settled. |
| Threat model | Missing | P0 create for tenants, auth, POS, imports, files, connectors, jobs, admin. |
| Access control and segregation of duties | Generated tenant snapshot only | P0 create normative policy and approved role templates. |
| Audit trail/security logging design | Scattered | P0 create with evidence, retention, tamper and verifier model. |
| Privacy/data protection | Audit notes only | P1 create inventory/purpose/request/breach/control mapping. |
| Retention, legal hold, erasure, archive | Missing as one policy | P0/P1 create with counsel/auditor input. |
| Compliance applicability/control matrix | Audit notes only | P1 create India-first, configuration-specific, review-expiring. |
| Secure development standard | CLAUDE and CI practices, not formal evidence | P1 map to final NIST SSDF baseline. |
| Vulnerability/dependency/license/SBOM policy | Partial CI/checklist | P1 create before external distribution. |
| Penetration-test scope and records | Scope exists, engagement open | Move scope to QA/security; records to assurance. |
| Terms, privacy notice, DPA, SLA, support policy, subprocessor list | Not present | Conditional P0 before commercial customer handling; counsel-owned, not AI-approved. |
| Developer/contractor agreement | Draft template exists | Move to legal templates, label unapproved, obtain counsel review. |

### 6.4 Engineering, QA, and operations

| Artifact | Current coverage | Priority/action |
|---|---|---|
| Developer setup and contributing | Root README + oversized handover | P1 split/rebuild. |
| Coding/review standard | Mostly CLAUDE conventions | P1 publish human-facing standard; keep agent instructions concise. |
| Test/quality strategy | Tests and UAT lists exist, strategy implicit | P0/P1 create risk-layered strategy. |
| Regression/test matrix | UAT checklist exists | Rebuild as route/journey/control matrix. |
| Performance/capacity plan | New audit/roadmap only | P1 formalize datasets, SLOs, test evidence. |
| Accessibility/device matrix | Missing | P1 for POS/WMS/mobile support. |
| Test-data standard | Missing; shared DB causes historical confusion | P0 create isolation, synthetic data, cleanup, side-effect rules. |
| Database migration standard | Strong conventions in CLAUDE, no standalone runbook | P0/P1 create for production safety. |
| Release/version/compatibility policy | Partial deployment docs | P1 create. |
| Deployment/configuration/upgrade | Partial | Consolidate into current runbooks. |
| Observability/SLO/capacity | Partial system screens and audit plan | P1 create. |
| Backup/restore/DR | Strong runbook and evidence | Keep; separate procedure from drill record. |
| Incident response | Exists | Rebuild contacts as external/configured secret; add evidence template. |
| Job/outbox operations | Scattered | Create with durable runner. |
| Release acceptance/evidence pack | Missing as one artifact | P0 before production claim. |

### 6.5 Implementation, training, support, and user help

| Artifact | Current coverage | Priority/action |
|---|---|---|
| Discovery/scope questionnaire | Missing | P0/P1 for solo onboarding. |
| Tenant onboarding/implementation guide | Admin Guide is developer/operator focused | P0 rebuild. |
| Configuration workbook | Setup screens and go-live decisions, no canonical workbook | P1 create templates per supported configuration. |
| Master-data migration guide/mapping workbook | Imports exist, end-to-end control absent | P0/P1 create. |
| Cutover and rollback plan | Pieces exist | P1 consolidate. |
| Business UAT plan and signed record | Run sheet exists | Separate reusable template from immutable execution record. |
| Training plan and role curriculum | Role journeys exist | P1 create from KB paths. |
| Hypercare/support handoff | Exists as draft | Keep template, create signed instance per go-live. |
| Tutorials | Getting-started articles exist | Expand high-traffic reference journeys first. |
| How-to guides | Scattered across SOP/manual/KB | Make KB canonical and task-oriented. |
| Reference | Error/report/phone/API references exist | Keep generated, clearly mark source and scope. |
| Explanation | Architecture/product concepts scattered | Add plain-language explanations where users need “why”. |
| Troubleshooting | Good early KB set | Expand by symptom; link every product error to safe action. |
| Admin guide | Exists but mixes developer and customer admin | Split platform-operator, tenant-admin, and implementation roles. |
| SOP templates | Existing SOPs are product-generic | Convert to templates; customers approve local controls/steps. |

## 7. Current-to-target disposition

| Current material | Target | Planned disposition |
|---|---|---|
| `requirements/BRD.md` | `requirements/brd.md` | Rebuild; preserve prior Git version and archive only if it was formally approved/signed. |
| `requirements/PRD.md` | `requirements/prd.md` + `requirements/modules/` | Rebuild; remove volatile status; allocate requirements to module files. |
| `ERP_BLUEPRINT.md` | `product/capability-catalog.md` + `architecture/overview.md` + current audit links | Decompose. Archive the dated snapshot after facts are mapped. |
| `specs/implementation_plan.md` | Product principles, requirements, architecture, historical archive | Extract canonical content; archive the mixed plan. |
| `specs/modules_overview.md` | Capability catalog + module requirements | Reconcile and archive; do not retain as a third module authority. |
| `specs/industry_plugs.md` | Supported configurations + module requirements + architecture/config design | Rebuild current/proposed distinction. |
| Maturity/parity/WMS plans | Product research + roadmap + archive | Extract open decisions/requirements, then archive completed/superseded plans. |
| OMS/WMS blueprint and market-intelligence reference notes | `product/research/` | Keep as non-normative research with provenance/date. |
| `pdf_blueprint_gap_analysis.md`, old hardening roadmap | `archive/YYYY/` | Archive as historical snapshots, never active backlog. |
| Architecture evaluation/framework docs | `architecture/overview.md`, views and ADRs | Reconcile current facts; split decisions from obsolete projections. |
| Forward-looking POS architecture | Module requirement/design proposal | Keep but label Proposed; never describe current runtime without evidence. |
| `micro_checklist.md` | `project/active-backlog.md` | Move only after concurrent work stops and CLAUDE/link/generator references are updated. |
| `project_ledger.md` and archive | `project/build-ledger.md`, `archive/` | Preserve history; reduce new entries to release/outcome summaries and ADR links. |
| `ai_handover.md` | `engineering/handover.md` plus stable setup/architecture docs | Rebuild short; remove absolute links and historical bulk. |
| User/Admin guides and SOPs | `user/knowledge-base`, generated manuals, SOP templates | Make KB source canonical; generate assemblies; split audiences. |
| Generated error/report catalogs | `generated/reference/` and KB projections | Retain generator authority; avoid hand edits; consolidate output manifest. |
| Generated permission matrix | Tenant-specific assurance export | Remove from universal guide after normative access policy exists. |
| OpenAPI JSON and prose | `api/` | Keep generated contract; add compatibility/deprecation policy. |
| KB Markdown and generated HTML | `user/knowledge-base/` and `internal/kb/content/` | Move only after generator accepts configurable source root and all screens/help mappings update. |
| Brain map/docs | `generated/brain/` | Keep map source, generator, and outputs together; label generated versus hand-owned files. |
| Screenshot set | `user/assets/screenshots/<release>/` | Recapture after browser harness is trustworthy; keep current supported set only. |
| Operations docs | `operations/`, `implementation/`, `assurance/` | Separate reusable procedure/template from dated execution evidence. |
| Audit/QC/UX/durability reports | `assurance/audits/2026/` | Preserve unchanged; add metadata and current-audit pointer. |
| Deep persona and smoothness audit/plan | Current audit + product remediation roadmap | Audit under assurance; plan under product roadmap/remediation after approval. |
| Extension/GitHub checklists | Engineering extension standard / security access checklist | Move and add owner/review cadence. |
| Developer contract | `legal/templates/` | Rename, label draft/unapproved, counsel review. |

## 8. One authority for each question

| Reader's question | Authoritative document/system |
|---|---|
| Why does this product exist and for whom? | Vision/strategy and approved BRD |
| What should the product do? | PRD, module requirements, NFRs |
| What is actually supported in this release? | Supported-configurations and capability catalog tied to release evidence |
| What remains planned? | Product roadmap; implementation details in active backlog |
| What was built and why did it change? | Release notes, ADRs, concise build ledger |
| What does the HTTP API expose? | Generated OpenAPI for the declared API version |
| What does the data model mean? | Generated data dictionary plus master-data/business definitions |
| How is the system designed? | Architecture description/views and ADRs |
| How do developers change it safely? | Engineering standards, test strategy, migration/release policy |
| How is it deployed and operated? | Current operations runbooks |
| How does a customer implement it? | Implementation, migration, cutover, UAT and training documents |
| How does a user complete a task? | In-app Knowledge Center |
| What happened during an audit, drill, UAT or incident? | Immutable assurance record for that date/release |
| Is it legally compliant? | Applicability/control matrix plus qualified counsel/auditor evidence—not marketing prose |

The code/runtime remains evidence of implemented behavior, but code alone is not product approval or customer support scope.

## 9. Document types and lifecycle

### 9.1 Types

| Type | Meaning | Mutation rule |
|---|---|---|
| `normative` | Approved requirement, policy, architecture or standard | Versioned review; changes require owner/approver. |
| `procedure` | Repeatable operational/user process | Update with product/process; verify by rehearsal. |
| `reference` | Lookup facts or research | Cite authority/date; may be generated or non-normative. |
| `record` | What happened at a date/release | Immutable except correction addendum. |
| `generated` | Projection of code/metadata/registry | Never hand-edit; generator and source named. |
| `template` | Blank reusable structure | Execution produces a separate dated record. |

### 9.2 Status lifecycle

`draft → in-review → approved → active → deprecated → superseded → archived`

Records use `complete` or `invalidated-with-addendum`, not `active`. Generated files use `current` or `stale`. A document cannot claim `approved` without a named approver/date.

### 9.3 Required metadata

All Markdown except tiny directory READMEs should start with machine-readable front matter:

```yaml
---
doc_id: REQ-PRD-CORE
title: Product Requirements Document
type: normative
status: draft
owner: Product Head
approvers: [Business Owner, Engineering Head]
audience: [product, engineering, qa, implementation]
applies_to: product-main
authority: product-intent
confidentiality: internal
last_verified: 2026-09-01
review_by: 2026-10-01
supersedes: []
superseded_by: null
generated_from: null
---
```

Git is the document version history. Add a human `version`/`effective_date` only for released customer, policy, legal, or signed documents.

### 9.4 Naming

- Lowercase kebab-case paths: `access-control-and-sod.md`.
- No spaces or uppercase names in new paths.
- Living documents do not include dates in filenames.
- Immutable records use `YYYY-MM-DD-short-description.md` inside a year folder.
- ADRs use `adr-0001-short-decision.md`; requirement IDs do not change when headings do.
- Use repository-relative links; never `file:///`, machine usernames, local ports, or secret-bearing URLs.
- One H1; start with purpose/status/audience; use business language before internal type names.

## 10. Ownership and review cadence

| Document family | Accountable owner | Required reviewers | Review trigger/cadence |
|---|---|---|---|
| Vision, BRD, roadmap, supported scope | Product Head/CEO | Business owners, architecture, implementation | Quarterly and material strategy/scope change |
| PRD/process/module requirements | Product Head/BA | Process owner, engineering, QA, UX, compliance as relevant | Every planned release/change |
| Architecture/ADRs/NFR technical | Engineering Head/Architect | Security, data, SRE, affected domain owner | Architecture or SLO change |
| Data/MDM/migration | Data/MDM owner | Finance, operations, implementation, privacy | Schema/master/process change; quarterly |
| Security/privacy/compliance | Security/Privacy lead | Architect, SRE, legal/auditor as applicable | Quarterly; incident/law/threat change |
| Engineering/test/release | Engineering/QA leads | Architect, security, SRE | Every process/tool/release change |
| Operations/runbooks | SRE/Platform operator | Support, security, engineering | Quarterly and after every incident/drill |
| Implementation/cutover/UAT/training | Implementation lead | Customer process owners, product, QA | Per implementation/release |
| User/KB/SOP | Documentation lead + module owner | Support, UX, QA, real user | Every affected release; top journeys monthly |
| Legal/commercial | Qualified counsel/business owner | Privacy/security/finance as relevant | Law/contract/product change; stated annual review |
| Assurance records | Test/audit/drill owner | Named approver | Immutable on completion; addendum only |

An AI or developer may draft any non-privileged document, but cannot self-approve business scope, legal compliance, statutory accounting, security acceptance, UAT, or production readiness.

## 11. Requirements and evidence traceability

### 11.1 Identifier scheme

- `BR-###` — business requirement/outcome.
- `FR-<DOMAIN>-###` — functional requirement, e.g. `FR-POS-014`.
- `NFR-<AREA>-###` — performance, availability, accessibility, recovery, etc.
- `SEC-###`, `DATA-###`, `OPS-###` — cross-cutting control requirements.
- Existing Stage/checklist IDs remain implementation-work identifiers, not requirements.
- `ADR-####` — architecture decision.
- Test/evidence IDs use existing test names plus stable scenario IDs where needed.

### 11.2 Trace path

```text
Business goal / legal need
    → persona and business process
    → BR / FR / NFR / SEC / DATA requirement
    → supported configuration and release
    → architecture view / ADR / data rule / API operation
    → implementation Stage or issue
    → automated test + human scenario
    → Knowledge Center task/help
    → release/UAT/security/audit evidence
```

The matrix should be generated from lightweight metadata or a small reviewed manifest, not copied manually across documents. Missing downstream evidence keeps a capability at Experimental/Preview.

## 12. Generator and validation architecture

### 12.1 One pure documentation command

Target interface:

```powershell
pwsh docs/update-docs.ps1          # generate to temp, validate, then atomically copy
pwsh docs/update-docs.ps1 -Check   # generate to temp and compare; zero repository writes
```

All existing generators accept explicit source and output roots. They emit a manifest:

```text
source → generated file → generator version → content checksum → release/schema/tenant scope
```

Generated permission evidence must not silently read an arbitrary default database. Its command requires an explicit environment/tenant and writes a dated assurance record, not a universal guide.

### 12.2 Dependency-free doc lint

A small Go or PowerShell checker using existing tooling should enforce:

- required metadata by document type;
- valid lifecycle status and named owner;
- lowercase/kebab path rules for new files;
- no `file:///`, local credentials, local absolute paths, or known secret patterns;
- valid repository-relative file links and headings;
- KB slug links checked against KB article IDs rather than filesystem assumptions;
- no orphan document outside registered archive/generated categories;
- no expired `review_by` for active critical documents;
- generated files match their sources and are not hand-edited;
- requirement/ADR/document IDs are unique;
- every user-facing view has help ownership and every Production capability has evidence;
- every archived/superseded document points to its replacement where one exists;
- document and embedded-KB size budgets.

External-link checking should run on a schedule, not block every offline build. Cache no web content in the production server.

### 12.3 Documentation budgets

| Budget | Target |
|---|---:|
| Mandatory docs runtime service | 0 |
| Embedded KB raw output | ≤ 2 MiB until measured need changes it |
| KB search index | ≤ 250 KiB |
| Ordinary article | ≤ 120 KiB raw; split by task if larger |
| Handover | ≤ 150 lines |
| Unowned active normative/procedure docs | 0 |
| Expired critical-doc reviews | 0 |
| Non-portable file links | 0 |
| `-Check` repository writes | 0 |
| Hand-edited generated files | 0 |

The full `docs/` tree should be excluded from the production deployment artifact except assets or generated KB content intentionally served. Repository screenshots and audit history must not inflate server disk.

## 13. Safe migration plan

### Phase D0 — Freeze, inventory, and safety repair

1. Wait for active Stage 37 concurrent edits to settle; take a fresh `git status`, file inventory, and link graph.
2. Create a dedicated documentation-reorganization branch/worktree from a known commit.
3. Record a pre-migration tag/commit for recoverability.
4. Fix all generator `-Check` paths so checks are provably read-only.
5. Generate a document register from the current tree and classify every file by type/status/authority/owner/proposed disposition.
6. Add temporary “current authority” banners to the highest-risk stale docs before any move: BRD, PRD, old loophole/QC audits, permission matrix, blueprint and forward-looking architecture.

**Exit:** no generator writes during `-Check`; every current document has a proposed disposition and no unknown owner category.

### Phase D1 — Governance skeleton and portal

1. Approve documentation standard, lifecycle, naming, RACI, and authority matrix.
2. Build the question/audience-based docs portal.
3. Add the document register and doc lint in warning mode.
4. Define confidentiality classes: public, customer, internal, restricted, secret-never-in-docs.
5. Keep existing paths intact during this phase.

**Exit:** a reader can identify current/superseded/generated/record status without relying on folder intuition.

### Phase D2 — Rebuild the authoritative core

Order matters:

1. Product vision and two chosen reference configurations.
2. Supported-configurations and capability catalog grounded in current audit/release evidence.
3. BRD with stable business goals/success measures.
4. Personas/JTBD and business-process catalog.
5. PRD core plus risk-critical module requirements: authorization, POS, returns, WMS owner/mobile, finance close, audit.
6. NFRs and compliance/data lifecycle requirements.
7. Architecture overview/views, threat model, access/SoD, audit design, MDM.
8. Traceability matrix and release acceptance.

Do not migrate stale status prose into the new documents. Every “supported” claim needs evidence; every unverified claim is Preview/Proposed.

**Exit:** one approved answer exists for why, intended behavior, current support, design, risk/control, and release evidence.

### Phase D3 — User documentation consolidation

1. Repair screenshot/browser contract tests.
2. Map every user-facing view/task to KB owner/article/status.
3. Classify KB content into tutorial/how-to/reference/explanation/troubleshooting.
4. Write the highest-risk/highest-frequency journeys first: onboarding, POS day, return, receiving-to-putaway, pick-to-load, period close, approval, error recovery.
5. Split tenant admin, platform operator, implementation consultant, and developer content.
6. Convert User/Admin manuals into generated curated assemblies.
7. Convert SOPs into customer-approvable templates with local roles/control points.
8. Rebuild screenshots against a declared release and supported roles/devices.

**Exit:** a normal owner/floor user can find current task help from the exact product state; no hand-maintained manual contradicts the KB.

### Phase D4 — Technical, operations, implementation, and assurance separation

1. Split stable developer setup from handover.
2. Reconcile architecture docs and begin ADRs.
3. Relocate API/OpenAPI after generator support is safe.
4. Separate operation procedures from drill/incident/UAT execution records.
5. Build implementation/cutover/migration/training templates.
6. Move audits and historical plans into dated assurance/archive folders with replacement pointers.
7. Move research/reference material out of active specs.

**Exit:** procedure, template and executed record cannot be confused; proposed architecture cannot be confused with current architecture.

### Phase D5 — Controlled path migration

Move one family per reviewed commit using `git mv`:

1. governance/product/requirements;
2. architecture/data/API/security;
3. engineering/operations/implementation/QA;
4. user/KB/guides/assets;
5. assurance/legal/project/generated/archive.

For each batch:

- update code, scripts, generators, CLAUDE instructions, root README, docs portal, KB mappings and Markdown links;
- create a small deprecated-path stub where external/internal links are likely;
- run doc lint, generator checks, KB check, application build/test in proportion to code-path changes, and in-app help smoke tests;
- stage only reviewed files—never blanket-add in the shared worktree;
- regenerate the project brain only after the move is complete and its check is safe.

**Exit:** zero broken links, zero unknown generated outputs, clean help/build checks, and approved review for that family.

### Phase D6 — Archive and deletion gate

Deletion is last. See §14.

### Phase D7 — Enforcement

1. Turn doc lint warnings into CI failures by class: generated drift and broken links first, metadata/review/traceability later.
2. Add PR checklist questions for affected requirements, help, migration, operations, release notes and evidence.
3. Publish monthly document-health report: stale, unowned, orphaned, broken, unsupported claims, KB coverage.
4. Conduct quarterly sample walkthroughs with real roles.

## 14. Rebuild, archive, and deletion policy

### Rebuild in preference to patching

Rebuild these because their information model is wrong or too entangled:

- BRD and PRD;
- ERP Blueprint/current capability snapshot;
- AI handover;
- architecture overview;
- role/permission documentation;
- User/Admin manual assemblies;
- product roadmap from overlapping parity/maturity plans;
- root/documentation indexes after target paths stabilize.

Rebuilding means mapping every still-valid statement to the replacement, not discarding content wholesale.

### Archive, do not delete

- audits, QC reports, penetration tests and security assessments;
- signed UAT, go-live, cutover and approval records;
- incidents, restore/DR drills and compliance evidence;
- architectural/product decisions that governed a release;
- historical build ledgers and completed plans with provenance;
- superseded legal/contract/policy versions where retention requires them.

Archived files are read-only historical records with replacement/current-state pointers. They are excluded from normal search/navigation unless the user asks for history.

### Legitimate deletion candidates

Only these categories should normally be deleted from the living tree:

1. Reproducible generated duplicate projections after one canonical generator/output model replaces them.
2. Obsolete screenshot assets after a verified replacement set exists and no released document references them.
3. Temporary migration copies, staging artifacts and abandoned empty folders.
4. Deprecated path stubs after at least two supported release cycles and a clean inbound-link search.
5. Exact duplicates whose authoritative copy, history and references are proven.

Git history is helpful but is not a substitute for legally required record retention or a customer-facing redirect.

### Deletion checklist

A file can be deleted only when all answers are yes:

- Is its type known and is it legally/operationally safe to delete?
- Is it generated/reproducible or fully superseded?
- Is every unique requirement, decision, procedure and evidence item mapped to a retained document?
- Does the replacement have an owner, approval and effective release?
- Are code, KB, Markdown, external published, and in-app links updated or intentionally redirected?
- Do generator/link/build/help checks pass from a clean checkout?
- Has the transition window expired?
- Is the deletion recorded in release notes/migration log?

No mass deletion command should be used. Deletions are explicit file lists in a reviewed commit.

## 15. First execution backlog

| Order | Work package | Priority | Relative effort | Dependency |
|---:|---|---|---:|---|
| 1 | Make all documentation `-Check` paths read-only and output-manifest-driven | P0 | M | Stable worktree |
| 2 | Generate full document register and assign type/status/authority/disposition | P0 | M | 1 |
| 3 | Add warning banners to stale high-risk current docs | P0 | S | 2 |
| 4 | Approve documentation standard, metadata, RACI and authority matrix | P0 | S–M | 2 |
| 5 | Create supported-configurations and capability catalog | P0 | L | Current release/audit evidence |
| 6 | Rebuild BRD | P0 | M | 5 + business owner |
| 7 | Create personas/JTBD and business-process catalog | P1 | L | Real user validation |
| 8 | Rebuild PRD core and risk-critical module requirements | P0 | XL | 5–7 |
| 9 | Create NFRs and release-evidence/traceability model | P0 | L | Audit budgets/control requirements |
| 10 | Rebuild current architecture, threat, access/SoD, audit and MDM documents | P0 | XL | Domain/control decisions |
| 11 | Repair route/view/help/screenshot contract testing | P1 | L | Generator safety |
| 12 | Consolidate KB/manual/SOP model and complete high-risk journeys | P1 | XL | 8 + 11 |
| 13 | Split handover/developer setup and operations procedure/evidence | P1 | M | Governance paths approved |
| 14 | Start ADRs and migrate API/data/security/engineering documentation | P1 | L | Core rebuild |
| 15 | Move audits/research/history to assurance/archive | P1 | M | Replacement pointers exist |
| 16 | Controlled folder migration with stubs and link updates | P1 | XL | 1–15 |
| 17 | Delete eligible generated duplicates/stale screenshots/stubs | P2 | S–M | Two-release gate |
| 18 | Turn documentation health checks into CI release gates | P1 | M | False positives resolved |

## 16. Completion criteria

The reorganization is complete only when:

- 100% of living normative/procedure/template documents have type, owner, audience, status, authority, review date and approval state.
- One source answers each question in §8; no conflicting living source remains.
- BRD, PRD, NFRs, supported configurations, capability catalog and risk-critical module requirements are approved and traceable.
- Every Production capability links to requirement, design/control, test, user help and release evidence.
- Every user-facing view/task has current contextual help or an explicit documented exception.
- All historical audits, decisions, UAT and drill evidence remain recoverable and are clearly non-current.
- There are zero portable-link errors, unregistered living documents, unknown generated outputs, and repository writes during `-Check`.
- The Knowledge Center stays within its binary/server budget and no documentation service/build framework has been added.
- A new CEO, normal user, floor worker, admin, developer, auditor and implementation consultant can each reach their required authoritative documents from the portal in two decisions or fewer.
- The old folder paths are removed only after their transition and deletion gates pass.

The result should feel smaller than the current documentation even if it contains more coverage: fewer competing truths, shorter living documents, generated references, explicit ownership, and history kept out of the normal path.
