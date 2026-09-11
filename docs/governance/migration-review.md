---
doc_id: DOC-GOV-MOVE-REVIEW
title: Documentation migration review
type: reference
status: draft
owner: documentation-maintainer
approvers: [engineering-owner, product-owner, legal-owner]
audience: [documentation and domain reviewers]
applies_to: proposed Stage 48 migration batches
authority: navigation
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Documentation migration review

This is a concrete proposal, not an approved migration or commit. The isolated
`stage48-migration-review-20260909` checkout contains the changes for inspection.
The main workspace retains the existing paths. The [machine-readable plan](migration-plan.json)
records the predecessor SHA-256 for each of 24 moves across five batches. Its 2026-09-10 amendment preserves the concurrent blueprint update and directs legacy product/design entry points to their canonical replacements while retaining the full historical source.

Every original Markdown path is retained as a small navigation page with its existing
section headings. Full original content moves to the stated destination; archival
records remain historical. The user/admin manual moves require the accountable reviewer
to accept unique-content parity before applying the proposal. The legal template remains
draft and unusable without qualified counsel/business review after its proposed move.

## Proposed moves

| Batch | Published path retained | Full-content destination | Approval |
|---:|---|---|---|
| 1 | [requirements/BRD.md](<../requirements/BRD.md>) | `docs/archive/requirements/business-requirements-legacy.md` | Pending |
| 1 | [requirements/PRD.md](<../requirements/PRD.md>) | `docs/archive/requirements/product-requirements-legacy.md` | Pending |
| 1 | [ERP_BLUEPRINT.md](<../ERP_BLUEPRINT.md>) | `docs/archive/product/erp-blueprint-legacy.md` | Pending |
| 1 | [specs/modules_overview.md](<../specs/modules_overview.md>) | `docs/archive/product/modules-overview-legacy.md` | Pending |
| 1 | [specs/erp_maturity_master_plan.md](<../specs/erp_maturity_master_plan.md>) | `docs/archive/product/erp-maturity-plan.md` | Pending |
| 1 | [specs/implementation_plan.md](<../specs/implementation_plan.md>) | `docs/archive/product/implementation-plan.md` | Pending |
| 1 | [specs/parity_master_plan.md](<../specs/parity_master_plan.md>) | `docs/archive/product/parity-plan.md` | Pending |
| 1 | [specs/wms_parity_plan.md](<../specs/wms_parity_plan.md>) | `docs/archive/product/wms-parity-plan.md` | Pending |
| 1 | [specs/pdf_blueprint_gap_analysis.md](<../specs/pdf_blueprint_gap_analysis.md>) | `docs/archive/product/pdf-blueprint-gap-analysis.md` | Pending |
| 1 | [specs/market_intelligence_reference.md](<../specs/market_intelligence_reference.md>) | `docs/product/research/market-intelligence.md` | Pending |
| 1 | [specs/oms_master_blueprint_reference.md](<../specs/oms_master_blueprint_reference.md>) | `docs/product/research/oms-benchmark.md` | Pending |
| 1 | [specs/wms_master_blueprint_reference.md](<../specs/wms_master_blueprint_reference.md>) | `docs/product/research/wms-benchmark.md` | Pending |
| 2 | [architecture/architecture_evaluation.md](<../architecture/architecture_evaluation.md>) | `docs/archive/architecture/evaluation-legacy.md` | Pending |
| 2 | [architecture/framework_architecture.md](<../architecture/framework_architecture.md>) | `docs/archive/architecture/framework-legacy.md` | Pending |
| 2 | [architecture/pos_architecture.md](<../architecture/pos_architecture.md>) | `docs/archive/architecture/pos-legacy.md` | Pending |
| 2 | [specs/public_api_v1.md](<../specs/public_api_v1.md>) | `docs/api/public-api-v1.md` | Pending |
| 2 | [specs/message_catalog.md](<../specs/message_catalog.md>) | `docs/api/message-catalog.md` | Pending |
| 3 | [operations/backup_restore.md](<../operations/backup_restore.md>) | `docs/operations/backup-and-restore.md` | Pending |
| 3 | [operations/incident_runbook.md](<../operations/incident_runbook.md>) | `docs/operations/incident-response.md` | Pending |
| 3 | [operations/connector_live_verification.md](<../operations/connector_live_verification.md>) | `docs/implementation/connector-verification.md` | Pending |
| 3 | [operations/hardening_roadmap.md](<../operations/hardening_roadmap.md>) | `docs/archive/operations/hardening-roadmap.md` | Pending |
| 4 | [guides/USER_GUIDE.md](<../guides/USER_GUIDE.md>) | `docs/archive/user/user-guide-legacy.md` | Pending |
| 4 | [guides/ADMIN_GUIDE.md](<../guides/ADMIN_GUIDE.md>) | `docs/archive/user/admin-guide-legacy.md` | Pending |
| 5 | [Contract/Developer Contract.md](<../Contract/Developer Contract.md>) | `docs/legal/templates/developer-agreement.md` | Pending |

## Review and application gates

1. Review the preserved [baseline](../assurance/stage48-baseline-2026-09-09.json), the working-tree diff and concurrent Stage 49 files. Approve a concrete pre-migration commit/tag.
2. For each batch verify content identity/unique requirements, scope and owner acceptance. Inspect changed links and the legacy headings; keep the explicit source/target list in the review.
3. Apply and validate one batch per reviewed commit. Run doclint, content generation/checks, the appropriate help/application checks and brain redraw for that batch. The prepared combined preview does not replace this per-commit gate.
4. Preserve both published-path navigation and required evidence for at least two supported releases. No deletion is proposed in this package.

Record reviewer identity, approval reference, baseline/batch commit, exact checks and findings
in the [migration/retention register](migration-and-retention-register.md). An isolated preview
and passing technical checks cannot self-approve product, legal, customer UAT or physical-device evidence.
