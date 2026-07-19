# Business Requirements Document (BRD)

Business goals, target market, and scope for this ERP — the "why are we building this" document. For "what to build, module by module," see [PRD.md](PRD.md). For "what's actually built today," see [`../micro_checklist.md`](../micro_checklist.md) and [`../ERP_BLUEPRINT.md`](../ERP_BLUEPRINT.md).

**Sources**: this document is grounded in three planning references maintained alongside this repo (`Generic_Inhouse_ERP_Platform_Approach_Blueprint.docx`, `Inhouse_ERP_Master_Blueprint_Generic.docx`, `Inhouse_ERP_Platform_Functionality_Blueprint...docx`), plus this repo's own [`../architecture/architecture_evaluation.md`](../architecture/architecture_evaluation.md) and [`../specs/industry_plugs.md`](../specs/industry_plugs.md). Those three references describe the **full target platform** — a much larger scope than what exists in code today. Every claim in this BRD about current build status is cross-checked against [`../micro_checklist.md`](../micro_checklist.md), not assumed from the planning references.

---

## 1. Business Problem

Small-to-mid-size retail and distribution businesses need core operational software — point-of-sale, inventory, procurement, accounting, and (increasingly) an online storefront presence — but face a gap in the market:

- **Large incumbent ERPs** (SAP, Netsuite, Odoo Enterprise) are priced, licensed, and architected for enterprise scale, with implementation costs and complexity out of proportion to a smaller operation's needs.
- **Point-solution SaaS tools** (a POS app here, an inventory spreadsheet there, a separate accounting package) solve individual problems but don't share a single source of truth, forcing manual reconciliation and creating data-integrity gaps between systems.
- **Industry-specific needs vary widely** (a jewelry retailer's making-charge/purity/design-combination tracking has nothing in common with a food & beverage outlet's KOT/table-management needs), but most affordable software is built for one vertical and awkwardly repurposed for others.

## 2. Executive Decision (why this architecture, not a simpler one)

The core business decision, carried through every planning reference consistently: **build a configurable ERP platform, not a fixed ERP application for one business.**

| Decision Area | Decision | Why It Matters |
|---|---|---|
| Product direction | Metadata-driven ERP platform | Avoids hardcoded screens; one system serves many business models without a code fork per client. |
| First vertical | Implement one complete vertical end-to-end before expanding | Proves the full stock → finance → tax → report reconciliation actually works, before betting on breadth. |
| Backend approach | API-first, service-oriented Go backend | Lightweight, scalable, cheap to run as SaaS — see §3. |
| Database | PostgreSQL with tenant isolation | Relational integrity, JSONB for metadata flexibility, transactional consistency. |
| Frontend | Schema-driven SPA | Forms/grids/labels/workflows render from metadata, not static per-screen code. |
| Customization | Configuration, feature flags, metadata, and scoped hooks | A client's customization must never be able to break the shared core. |
| Control model | Audit log, maker-checker, approvals, status transitions | Makes the system enterprise-grade and audit-ready from day one, not bolted on later. |

**Core principle**: build the platform once, configure it many times. Keep core logic clean, reusable, auditable, and protected from client-specific customization.

## 3. Business Goals

1. **One system of record** — POS, inventory, procurement, finance/GL, and (as it grows) HR and manufacturing, all against the same underlying data, so a sale, a stock movement, and a ledger entry are never three separate systems that can silently disagree.
2. **Low total cost of ownership** — a Go/PostgreSQL stack was chosen specifically because a Go binary's footprint (~15-30MB deployed, ~10-15MB idle RAM) lets one small server host dozens of tenants where a Python/Node equivalent would exhaust memory on a handful — see [`../architecture/architecture_evaluation.md`](../architecture/architecture_evaluation.md) §2 for the comparison. This is a direct, deliberate lever on making many small-margin clients economically viable to serve.
3. **Multi-industry from one codebase** — a single ERP Kernel with industry-specific behavior loaded as metadata packages, not a fork per vertical (see [PRD.md](PRD.md) §1 for how this is structured). Target verticals per [`../specs/industry_plugs.md`](../specs/industry_plugs.md) and the planning references: Jewelry, Food & Beverage, Automotive, Clothing/Apparel (built today), with Pharma, Construction, Metal/Steel, Medical Devices, Semiconductors, and Agriculture specified for future expansion.
4. **Omnichannel-ready** — a business that sells in-store and wants to also sell on Shopify/BigCommerce/Magento shouldn't need a second inventory system; this ERP is the source of truth and pushes to those channels, not the reverse.
5. **Safe to trust with real money movement** — double-entry accounting, an approval/maker-checker workflow for anything above a configurable threshold, and an immutable audit trail, since this is not just an operations tool but a financial system of record.
6. **Error-proof by construction, not by discipline** — the planning references are explicit that the system itself, not user carefulness, must prevent duplicate invoices, over-receipt, negative stock, unauthorized edits, and incomplete transactions. This shows up in the PRD as an explicit error-proofing requirement per module, not an afterthought.

## 4. Target Market

- **Primary**: independent and small-chain retailers/distributors in the verticals listed above, currently underserved by both enterprise ERPs (too expensive/complex) and single-purpose SaaS tools (too fragmented).
- **Deployment model**: multi-tenant SaaS — one shared platform instance serving many independent client businesses (tenants), each with fully isolated data (schema-per-tenant in PostgreSQL).
- **Buyer profile**: a business owner or operations lead who needs the system to be reliable and affordable more than they need deep customizability — the product's job is to make the common case simple, not to expose every possible configuration.

## 5. Scope

### In scope (see [PRD.md](PRD.md) for the full functional breakdown, module by module, with built-vs-specified status per item)
Core kernel and multi-tenancy; POS/checkout; inventory and warehouse operations; procurement through to vendor payment; finance/GL with GST; HR foundation and payroll export; fixed assets; expense management; a scoped CRM/loyalty program; a scoped manufacturing module (single-level BOM); PIM (Product Information Management) with real e-commerce channel connectors; role-based access control with MFA for privileged roles; an operational deployment/backup/incident-response toolchain.

### Explicitly out of scope (stated, not just absent)
- **AI Assist** — repeatedly and explicitly excluded from every module pass pending a dedicated product/design conversation (see `../micro_checklist.md`'s Stage 15/16 snapshot notes). Not to be silently folded into any module.
- **Full statutory payroll/compliance processing** — HR module exports payroll data; it does not run payroll or handle statutory filings itself.
- **Full e-invoicing (GST IRN/e-way bill generation)** — specified in detail in the planning references (§12.4 of the Master Blueprint) but not implemented; GST is calculated and enforced at posting only.
- **MRP/production routing/QC** — manufacturing is a scoped single-level BOM + linear production order MVP, not the full job-work/costing system the planning references describe.
- **Offline-first POS** — the current POS is a single synchronous online screen; offline queueing/cash-drawer sessions/KOT (specified in detail in the planning references' POS Architecture section) are not built.
- **Full warehouse operations** (bin management, putaway, pick/pack workflows) — specified but not built; current inventory/warehouse scope is barcoded stock counts, availability, and transfers only.

## 6. Success Criteria

1. A pilot tenant can run their full daily operation (sell at POS, receive stock via a purchase order, close their books) inside this system without falling back to a spreadsheet or a second tool for any of those three flows.
2. The system survives a real, non-synthetic transaction volume without data-integrity incidents (unbalanced ledger entries, lost orders, double-counted stock) — see [`../ERP_BLUEPRINT.md`](../ERP_BLUEPRINT.md) §1 for the current honest status: this has not yet been tested against real volume.
3. Onboarding a new tenant (provisioning, industry profile selection, initial data load) takes hours, not the weeks/months typical of an enterprise ERP implementation.
4. Per-tenant hosting cost stays low enough that serving a small-margin retail client is economically viable — the entire technology stack choice (§2 above) exists to serve this criterion.
5. The system can answer, from its own audit/ledger data without manual reconstruction, the "final success questions" the Master Blueprint poses as the real test of an ERP: *Where is this barcode now? Who approved this payment? Why was this vendor paid? Why is stock different? Which integration failed today?* — each answerable from a report or log, not a manual investigation.

## 7. Stakeholders

- **Business owner/operator (the tenant)**: needs reliability, low cost, and a system that matches how their specific industry actually works.
- **End users at the tenant** (cashiers, warehouse staff, accountants, managers): need role-appropriate access and a UI they can use without extensive training — see [`../guides/USER_GUIDE.md`](../guides/USER_GUIDE.md).
- **Platform operator** (whoever runs this SaaS): needs multi-tenant isolation, a real deployment/rollback/backup pipeline, and incident visibility — see [`../guides/ADMIN_GUIDE.md`](../guides/ADMIN_GUIDE.md) and [`../operations/incident_runbook.md`](../operations/incident_runbook.md).
- **3rd-party developers** (future): a scoped, read-only extension framework already exists (`extension-sdk/`) for hooking into the platform without full core access.

## 8. Operating Model (who owns what, once this is a going concern)

Per the planning references, this should be governed like a product, not a one-time project:

| Role | Responsibility |
|---|---|
| Product Owner | Roadmap, scope, priority, user acceptance, module sequencing. |
| Engineering Head / Architect | Architecture, technical standards, code quality, data model, extensibility. |
| Business Analyst | This BRD, process mapping, validations, reports, UAT scenarios. |
| QA | Test cases, regression, UAT support, concurrency/integration testing. |
| Implementation/Support | Configuration, migration, training, go-live, incident response (see [`../operations/incident_runbook.md`](../operations/incident_runbook.md)). |

Client/industry-specific customization is handled through configuration first, scoped extension hooks second, and core code changes only when a genuinely reusable platform capability is required — never a per-client fork.
