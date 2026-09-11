---
doc_id: DOC-GOV-002
title: Documentation authority matrix
type: reference
status: draft
owner: documentation-maintainer
approvers: [product-owner, engineering-owner]
audience: [everyone]
applies_to: repository documentation transition
authority: navigation
confidentiality: internal
last_verified: 2026-09-06
review_by: 2026-10-06
supersedes: none
superseded_by: none
---

# Documentation authority matrix

One authority per question is the target. A missing approved authority remains missing;
neither completed checklist items nor this draft create a support promise.

| Reader question | Authority | Current entry / limitation | Accountable role |
|---|---|---|---|
| Why build this product? | Vision + BRD | [Vision](../product/vision.md), [rebuilt BRD](../requirements/business-requirements.md); approval pending | Product owner |
| What behavior is required? | PRD + domain requirements | [PRD core and 14 domains](../requirements/product-requirements.md); approval pending | Product/process owners |
| What is supported in my configuration? | Capability register + accepted release evidence | [Generated catalog](../generated/capability-catalog.md); Preview/Experimental only; release evidence pending | Product owner + QA |
| What is planned? | Outcome roadmap + live work tracker | [Micro-checklist](../micro_checklist.md); old parity plans are proposals/history | Product owner |
| What does the API expose? | Generated OpenAPI + compatibility guidance | [OpenAPI](../specs/openapi_public_v1.json), [API guide](../specs/public_api_v1.md) | API owner |
| How is it designed? | Current architecture + governing ADRs | [Current architecture](../architecture/current-architecture.md); remaining ADR/reconciliation work in 48.4 | Engineering owner |
| How do I perform a user task? | KB topic | [Role journeys](../kb/role-journeys/role-journeys-overview.md); manuals remain transition copies | Documentation + module owners |
| How do I operate or recover it? | Runbook | [Incident](../operations/incident_runbook.md), [backup](../operations/backup_restore.md) | Operations owner |
| How do I implement it for a customer? | Implementation guides + accepted configuration workbook | [Onboarding/cutover workbook](../implementation/onboarding-workbook.md); customer execution/approval pending | Implementation owner |
| What actually happened? | Dated scoped immutable record | [Audits](../audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md), [drill log](../operations/restore_drill_log.md) | Evidence owner |
| Is it secure/compliant or legally suitable? | Qualified applicability/control evidence | [Security register](../security/README.md); no blanket certification | Security/privacy/legal owners |
| Where do I resume development? | Short handover + developer procedures | [Handover](../ai_handover.md); split pending 48.8 | Engineering owner |

The [document register](document-register.json) records provisional ownership, lifecycle,
authority, replacement and disposition for all inventoried documents and assets. Directory
indexes route readers; they do not independently restate product support.
