---
doc_id: DOC-REQ-PRD
title: Product requirements core
type: normative
status: draft
owner: product-owner
approvers: [product-owner, engineering-owner, qa-owner]
audience: [product, process-owners, engineering, QA]
applies_to: proposed reference configurations
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Product requirements core

Intended behavior is separate from availability. The [capability catalog](../generated/capability-catalog.md)
is the scoped maturity projection. The [legacy PRD](PRD.md) is retained for content mapping;
its old built/spec counts are not current truth.

## Shared behavior

Every command carries explicit tenant, actor and business scope. The server validates
permission, workflow state, required data and authoritative economic inputs before mutation.
Controlled transitions enforce their approved reason/checker policy. Posted economic history
is corrected through attributable reversal or compensation. Retries reconcile to one outcome;
errors state what saved and the safe recovery action. Generic APIs, imports, jobs and
integrations obey the same controls as the UI.

## Domain allocation

| Domain | Intended behavior and acceptance |
|---|---|
| Checkout and cashier close | [POS requirements](modules/pos.md) |
| Eligibility, QC and refunds | [Returns requirements](modules/returns.md) |
| Owners, traceability and floor tasks | [WMS requirements](modules/wms.md) |
| Intake, fulfillment and settlement | [OMS requirements](modules/oms.md) |
| Golden records, quality and publish | [PIM/MDM requirements](modules/pim.md) |
| Approved demand and purchase-to-pay | [Procurement requirements](modules/procurement.md) |
| Quantities, custody and correction | [Inventory requirements](modules/inventory.md) |
| Posting, close, tax and payments | [Finance requirements](modules/finance.md) |
| Customer purpose, points and campaigns | [CRM requirements](modules/crm.md) |
| Identity, sensitive data and payroll | [HR requirements](modules/hr.md) |
| Plans, production and scheduling | [Manufacturing requirements](modules/manufacturing.md) |
| Work, cost, billing and resolution | [Projects/service requirements](modules/service.md) |
| Authorization, delivery and lifecycle | [Platform/API requirements](modules/platform.md) |
| Canonical, contextual and accessible help | [Knowledge Center requirements](modules/knowledge.md) |

## Reference journeys and evidence

[Personas and processes](personas-and-processes.md) identify boundaries and reconciliations.
[NFRs](nonfunctional-requirements.md) define shared measures. [Traceability](../generated/requirements-traceability.md)
connects capability to requirement, design, Stage work, test, help and release evidence.
A test filename is a location; only a scoped executed result with its owner can support
a release claim. Stage numbers remain work IDs, distinct from BR/FR/NFR/SEC/DATA/OPS IDs.

## Support and change model

Experimental requires controlled evaluation. Preview is a bounded evaluation candidate with
published limits. Production requires accepted configuration-specific requirements,
design/control, automated and human evidence, help, operations and release approval.
Certified additionally requires applicable independent qualified certification evidence.
Checklist completion and version numbers cannot establish either level.

## Open gates

The warehouse reference uses one owner per warehouse under the 47.5 guard; mixed-owner use
is unsupported. Physical devices await 47.6.6; audit archive/restore awaits 47.7.6 and qualified review. No country/industry/device configuration inherits
acceptance from another. Full product/process approval and signed release evidence remain open.
