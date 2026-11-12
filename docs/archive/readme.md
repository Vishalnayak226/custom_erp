---
doc_id: DOC-ARCHIVE-001
title: Archive
type: reference
status: active
owner: documentation-maintainer
approvers: [engineering-owner]
audience: [everyone]
applies_to: dated and superseded documentation
authority: navigation
confidentiality: internal
last_verified: 2026-10-19
review_by: 2027-01-19
supersedes: [DOC-GOV-MOVE-REVIEW]
superseded_by: none
---

# Archive

Everything here is kept for the record. None of it describes the system as it is today:
for that, start at the [documentation portal](../README.md).

| Folder or file | What is inside |
|---|---|
| [`micro_checklist_closed_stages.md`](micro_checklist_closed_stages.md) | Checklist stages that are fully closed, moved out of the live [micro checklist](../micro_checklist.md) |
| [`project_ledger_sections_4_62.md`](project_ledger_sections_4_62.md) | Ledger sections 4-62, moved out of the live [project ledger](../project_ledger.md) |
| `handovers/` | Earlier AI handover snapshots (the current one is [`ai_handover.md`](../ai_handover.md)) |
| `audits/` | Dated audits, reviews, verification evidence and their raw data files; on-demand permission snapshots land in `audits/permissions/` |
| `legacy/` | Specifications, plans and architecture papers that newer documents have replaced |

## Rules

- Do not rewrite a record. A correction is a new, dated document that links back to the old one.
- When a checklist stage or ledger section closes completely, move it into the matching file
  above instead of letting the live tracker grow.
- Fixing a broken link inside a record is fine; changing what it says is not.

## Where did it go?

On 2026-10-19 (checklist Stage 97) the docs folder was reorganised. Paths below are relative to
`docs/`. Some older code comments and records still mention the old path: look it up here.

| Old path | What happened |
|---|---|
| `DURABILITY_AUDIT_2026-07-31.md` | moved to [`archive/audits/durability-audit-2026-07-31.md`](audits/durability-audit-2026-07-31.md) |
| `ERP_LOOPHOLES_ANALYSIS.md` | moved to [`archive/audits/erp-loopholes-analysis.md`](audits/erp-loopholes-analysis.md) |
| `QC_EXHAUSTIVE_REPORT.md` | moved to [`archive/audits/qc-exhaustive-report.md`](audits/qc-exhaustive-report.md) |
| `UX_MANUAL_AUDIT.md` | moved to [`archive/audits/ux-manual-audit.md`](audits/ux-manual-audit.md) |
| `audits/DOCUMENTATION_ARCHITECTURE_PLAN_2026-09-01.md` | moved to [`archive/audits/documentation-architecture-plan-2026-09-01.md`](audits/documentation-architecture-plan-2026-09-01.md) |
| `audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md` | moved to [`archive/audits/erp-deep-persona-audit-2026-09-01.md`](audits/erp-deep-persona-audit-2026-09-01.md) |
| `audits/LIGHTWEIGHT_SMOOTHNESS_PLAN_2026-09-01.md` | moved to [`archive/audits/lightweight-smoothness-plan-2026-09-01.md`](audits/lightweight-smoothness-plan-2026-09-01.md) |
| `audits/STAGE47_ENDPOINT_MUTATION_MAP_2026-09-03.md` | moved to [`archive/audits/stage47-endpoint-mutation-map-2026-09-03.md`](audits/stage47-endpoint-mutation-map-2026-09-03.md) |
| `assurance/erp-functional-audit-2026-10-05.md` | moved to [`archive/audits/erp-functional-audit-2026-10-05.md`](audits/erp-functional-audit-2026-10-05.md) |
| `assurance/erp-independent-audit-2026-09-16.md` | moved to [`archive/audits/erp-independent-audit-2026-09-16.md`](audits/erp-independent-audit-2026-09-16.md) |
| `assurance/erp-usability-observations-2026-10-04.json` | moved to [`archive/audits/erp-usability-observations-2026-10-04.json`](audits/erp-usability-observations-2026-10-04.json) |
| `assurance/erp-usability-screen-register-2026-10-04.md` | moved to [`archive/audits/erp-usability-screen-register-2026-10-04.md`](audits/erp-usability-screen-register-2026-10-04.md) |
| `assurance/release-budget-evidence-2026-10-04.md` | moved to [`archive/audits/release-budget-evidence-2026-10-04.md`](audits/release-budget-evidence-2026-10-04.md) |
| `assurance/release-budget-report-2026-10-04.json` | moved to [`archive/audits/release-budget-report-2026-10-04.json`](audits/release-budget-report-2026-10-04.json) |
| `assurance/stage48-baseline-2026-09-09.json` | moved to [`archive/audits/stage48-baseline-2026-09-09.json`](audits/stage48-baseline-2026-09-09.json) |
| `assurance/stage48-capture-2026-09-09.json` | moved to [`archive/audits/stage48-capture-2026-09-09.json`](audits/stage48-capture-2026-09-09.json) |
| `assurance/stage48-external-links-2026-09-09.json` | moved to [`archive/audits/stage48-external-links-2026-09-09.json`](audits/stage48-external-links-2026-09-09.json) |
| `assurance/stage48-verification-2026-09-09.md` | moved to [`archive/audits/stage48-verification-2026-09-09.md`](audits/stage48-verification-2026-09-09.md) |
| `assurance/stage48-verification-2026-09-10.md` | moved to [`archive/audits/stage48-verification-2026-09-10.md`](audits/stage48-verification-2026-09-10.md) |
| `assurance/stage55-shared-correctness-2026-10-04.json` | moved to [`archive/audits/stage55-shared-correctness-2026-10-04.json`](audits/stage55-shared-correctness-2026-10-04.json) |
| `qa/erp-audit-2026-09-16-summary.json` | moved to [`archive/audits/erp-audit-2026-09-16-summary.json`](audits/erp-audit-2026-09-16-summary.json) |
| `ERP_BLUEPRINT.md` | moved to [`archive/legacy/erp-blueprint.md`](legacy/erp-blueprint.md) |
| `specs/modules_overview.md` | moved to [`archive/legacy/modules-overview.md`](legacy/modules-overview.md) |
| `specs/erp_maturity_master_plan.md` | moved to [`archive/legacy/erp-maturity-master-plan.md`](legacy/erp-maturity-master-plan.md) |
| `specs/implementation_plan.md` | moved to [`archive/legacy/implementation-plan.md`](legacy/implementation-plan.md) |
| `specs/parity_master_plan.md` | moved to [`archive/legacy/parity-master-plan.md`](legacy/parity-master-plan.md) |
| `specs/wms_parity_plan.md` | moved to [`archive/legacy/wms-parity-plan.md`](legacy/wms-parity-plan.md) |
| `specs/pdf_blueprint_gap_analysis.md` | moved to [`archive/legacy/pdf-blueprint-gap-analysis.md`](legacy/pdf-blueprint-gap-analysis.md) |
| `architecture/architecture_evaluation.md` | moved to [`archive/legacy/architecture-evaluation.md`](legacy/architecture-evaluation.md) |
| `architecture/framework_architecture.md` | moved to [`archive/legacy/framework-architecture.md`](legacy/framework-architecture.md) |
| `architecture/pos_architecture.md` | moved to [`archive/legacy/pos-architecture.md`](legacy/pos-architecture.md) |
| `requirements/BRD.md` | moved to [`archive/legacy/brd.md`](legacy/brd.md) |
| `requirements/PRD.md` | moved to [`archive/legacy/prd.md`](legacy/prd.md) |
| `operations/hardening_roadmap.md` | moved to [`archive/legacy/hardening-roadmap.md`](legacy/hardening-roadmap.md) |
| `guides/PERMISSION_MATRIX.md` | moved to [`archive/legacy/permission-matrix.md`](legacy/permission-matrix.md) |
| `governance/migration-and-retention-register.md` | moved to [`archive/legacy/migration-and-retention-register.md`](legacy/migration-and-retention-register.md) |
| `archive/ai-handover-2026-09-09.md` | moved to [`archive/handovers/ai-handover-2026-09-09.md`](handovers/ai-handover-2026-09-09.md) |
| `archive/ai-handover-2026-10-03.txt` | moved to [`archive/handovers/ai-handover-2026-10-03.txt`](handovers/ai-handover-2026-10-03.txt) |
| `extension_hooks_checklist.md` | moved to [`engineering/extension-hooks-checklist.md`](../engineering/extension-hooks-checklist.md) |
| `github_checklist.md` | moved to [`engineering/github-security-checklist.md`](../engineering/github-security-checklist.md) |
| `specs/public_api_v1.md` | moved to [`api/public-api-v1.md`](../api/public-api-v1.md) |
| `specs/message_catalog.md` | moved to [`api/message-catalog.md`](../api/message-catalog.md) |
| `specs/market_intelligence_reference.md` | moved to [`product/research/market-intelligence.md`](../product/research/market-intelligence.md) |
| `specs/oms_master_blueprint_reference.md` | moved to [`product/research/oms-benchmark.md`](../product/research/oms-benchmark.md) |
| `specs/wms_master_blueprint_reference.md` | moved to [`product/research/wms-benchmark.md`](../product/research/wms-benchmark.md) |
| `specs/industry_plugs.md` | moved to [`architecture/industry-profiles.md`](../architecture/industry-profiles.md) |
| `Contract/Developer Contract.md` | moved to [`legal/templates/developer-agreement.md`](../legal/templates/developer-agreement.md) |
| `governance/adr-template.md` | moved to [`architecture/decisions/adr-template.md`](../architecture/decisions/adr-template.md) |
| `data/data-lifecycle.md` | merged into [`data/data-handbook.md`](../data/data-handbook.md#data-lifecycle-and-retention-decisions) |
| `data/dictionary-workflow.md` | merged into [`data/data-handbook.md`](../data/data-handbook.md#data-dictionary-capture-and-review) |
| `data/import-and-migration.md` | merged into [`data/data-handbook.md`](../data/data-handbook.md#import-and-data-migration-controls) |
| `data/master-data-governance.md` | merged into [`data/data-handbook.md`](../data/data-handbook.md#master-data-governance) |
| `operations/upgrade-and-rollback.md` | merged into [`operations/service-operations.md`](../operations/service-operations.md#upgrade-and-rollback) |
| `operations/jobs-and-outbox.md` | merged into [`operations/service-operations.md`](../operations/service-operations.md#job-and-outbox-recovery) |
| `security/secure-development.md` | merged into [`security/secure-development-lifecycle.md`](../security/secure-development-lifecycle.md#ssdf-practice-mapping-and-release-evidence) |
| `specs/openapi_public_v1.json` | deleted: identical copy of [`api/generated/public-v1.json`](../api/generated/public-v1.json) |
| `governance/migration-plan.json` | deleted: preview of a September migration that never ran; this table replaces it |
| `governance/migration-review.md` | deleted: preview of a September migration that never ran; this table replaces it |
| `micro_checklist.pdf` | deleted: a local PDF export, never in git |
| `engineering/contributing.md` | merged into [`CONTRIBUTING.md`](../../CONTRIBUTING.md) at the repository root, the project constitution (2026-11-10, item 159.1) |
| `brain/BRAIN.md` | renamed to [`brain/brain-map.md`](../brain/brain-map.md) (2026-11-11, item 160.1) |
| `guides/ADMIN_GUIDE.md` | renamed to [`guides/admin-guide.md`](../guides/admin-guide.md) (2026-11-11, item 160.1) |
| `guides/ADMIN_SOP.md` | renamed to [`guides/admin-sop.md`](../guides/admin-sop.md) (2026-11-11, item 160.1) |
| `guides/ERROR_CODES.md` | renamed to [`guides/error-codes.md`](../guides/error-codes.md) (2026-11-11, item 160.1) |
| `guides/QZ_PRINTING_SETUP.md` | renamed to [`guides/qz-printing-setup.md`](../guides/qz-printing-setup.md) (2026-11-11, item 160.1) |
| `guides/REPORT_CATALOG.md` | renamed to [`guides/report-catalog.md`](../guides/report-catalog.md) (2026-11-11, item 160.1) |
| `guides/UAT_CHECKLIST.md` | renamed to [`guides/uat-checklist.md`](../guides/uat-checklist.md) (2026-11-11, item 160.1) |
| `guides/USER_GUIDE.md` | renamed to [`guides/user-guide.md`](../guides/user-guide.md) (2026-11-11, item 160.1) |
| `guides/USER_SOP.md` | renamed to [`guides/user-sop.md`](../guides/user-sop.md) (2026-11-11, item 160.1) |
| `guides/img/MANIFEST.md` | renamed to [`guides/img/screenshot-manifest.md`](../guides/img/screenshot-manifest.md) (2026-11-11, item 160.1) |
| `operations/backup_restore.md` | renamed to [`operations/backup-restore.md`](../operations/backup-restore.md) (2026-11-11, item 160.1) |
| `operations/connector_live_verification.md` | renamed to [`operations/connector-live-verification.md`](../operations/connector-live-verification.md) (2026-11-11, item 160.1) |
| `operations/go_live_decisions.md` | renamed to [`operations/go-live-decisions.md`](../operations/go-live-decisions.md) (2026-11-11, item 160.1) |
| `operations/hypercare_plan.md` | renamed to [`operations/hypercare-plan.md`](../operations/hypercare-plan.md) (2026-11-11, item 160.1) |
| `operations/incident_runbook.md` | renamed to [`operations/incident-runbook.md`](../operations/incident-runbook.md) (2026-11-11, item 160.1) |
| `operations/pentest_scope.md` | renamed to [`operations/pentest-scope.md`](../operations/pentest-scope.md) (2026-11-11, item 160.1) |
| `operations/restore_drill_log.md` | renamed to [`operations/restore-drill-log.md`](../operations/restore-drill-log.md) (2026-11-11, item 160.1) |
| `operations/uat_run_sheet.md` | renamed to [`operations/uat-run-sheet.md`](../operations/uat-run-sheet.md) (2026-11-11, item 160.1) |
| `qa/audit-deploy-mock.py` | renamed to [`qa/audit_deploy_mock.py`](../qa/audit_deploy_mock.py) (2026-11-11, item 160.1) |
| `qa/audit-http.py` | renamed to [`qa/audit_http.py`](../qa/audit_http.py) (2026-11-11, item 160.1) |
| `qa/audit-restore.py` | renamed to [`qa/audit_restore.py`](../qa/audit_restore.py) (2026-11-11, item 160.1) |
| `qa/run-maturity-audit.py` | renamed to [`qa/run_maturity_audit.py`](../qa/run_maturity_audit.py) (2026-11-11, item 160.1) |
| `qa/run-maturity-suites.py` | renamed to [`qa/run_maturity_suites.py`](../qa/run_maturity_suites.py) (2026-11-11, item 160.1) |
| `qa/summarize-maturity-audit.py` | renamed to [`qa/summarize_maturity_audit.py`](../qa/summarize_maturity_audit.py) (2026-11-11, item 160.1) |
| `qa/stage55-shared-correctness.cjs` | renamed to [`qa/shared-correctness.cjs`](../qa/shared-correctness.cjs) (2026-11-11, item 160.1) |
| `security/outside_in_verification_2026-09-08.md` | renamed to [`security/outside-in-verification-2026-09-08.md`](../security/outside-in-verification-2026-09-08.md) (2026-11-11, item 160.1) |
| `security/risk_register.md` | renamed to [`security/risk-register.md`](../security/risk-register.md) (2026-11-11, item 160.1) |
| `security/threat_model.md` | renamed to [`security/threat-model.md`](../security/threat-model.md) (2026-11-11, item 160.1) |
| `../scripts/verify_connector_live.ps1` | renamed to [`../scripts/verify-connector-live.ps1`](../../scripts/verify-connector-live.ps1) (2026-11-11, item 160.1) |
| `../db/*.sql` | the 388 migrations moved into [`../db/migrations/`](../../db/migrations/), names unchanged (2026-11-11, item 103.3) |
| stage-named Go files | 454 renamed by what they do; old to new in [`archive/renamed-files-2026-11-11.md`](renamed-files-2026-11-11.md) (2026-11-11, item 103.3) |
