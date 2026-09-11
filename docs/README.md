---
doc_id: DOC-NAV-001
title: Documentation portal
type: reference
status: active
owner: documentation-maintainer
approvers: [engineering-owner]
audience: [everyone]
applies_to: repository navigation
authority: navigation
confidentiality: internal
last_verified: 2026-09-06
review_by: 2026-10-06
supersedes: none
superseded_by: none
---

# Documentation portal

Choose the question that matches your task. The [authority matrix](governance/authority-matrix.md)
explains which source can answer it and where approved product evidence is still missing.
The [document register](governance/document-register.json) lists all inventoried material,
including provisional ownership, review status, replacements and historical records.

## Run the business or use the product

| I need to… | Start here | Audience / authority |
|---|---|---|
| Understand the business purpose | [Vision](product/vision.md), [rebuilt BRD](requirements/business-requirements.md), [PRD core](requirements/product-requirements.md) | Product leadership; concrete drafts awaiting owner approval |
| Know what my configuration supports | [Capability catalog](generated/capability-catalog.md), [evidence traceability](generated/requirements-traceability.md) | Buyer / product owner; scoped evaluation limits, no Production approval |
| Learn the everyday workflow for my role | [Role journeys](kb/role-journeys/role-journeys-overview.md) | Cashier, store manager, warehouse, finance, category manager, admin |
| Set up a shop and make a first sale | [Onboarding tutorial](kb/getting-started/open-a-shop-and-make-your-first-sale.md) | Business owner / tenant admin |
| Find a task or recovery instruction | [Curated user manual](user/user-manual.html), [troubleshooting](kb/troubleshooting/troubleshooting-index.md) | Users; KB topics are the canonical task source |
| Configure users or operations | [Tenant admin manual](user/tenant-admin-manual.html), [security and approvals](kb/module-handbooks/security-approvals.md) | Tenant admin; [manual sources and migration status](user/manuals.md) |
| Run user acceptance checks | [UAT checklist](guides/UAT_CHECKLIST.md), [run-sheet template](operations/uat_run_sheet.md) | QA / implementation; blank checks are not signed acceptance |
| Use a procedural template | [User SOP](guides/USER_SOP.md), [Admin SOP](guides/ADMIN_SOP.md) | Process owner; customer approval required before local use |

## Build, integrate, deploy or recover

| I need to… | Start here | Audience / authority |
|---|---|---|
| Resume development | [Developer setup](engineering/developer-setup.md), [handover §6](ai_handover.md#6-version-control--handover-status) | Developer; stable procedure and shared-tree state |
| Locate a subsystem | [Current architecture](architecture/current-architecture.md), [project brain](brain/README.md) | Engineer; verify graph inferences in source |
| Understand data and runtime boundaries | [Data/runtime views](architecture/data-and-runtime-views.md), [generated dictionary](data/generated/dictionary.md) | Engineer/data steward; scoped source facts |
| Integrate through the API | [API overview](api/overview.md), [generated OpenAPI](api/generated/public-v1.json) | Integrator; generated contract plus authored usage |
| Read errors or reports | [Error reference](guides/ERROR_CODES.md), [report catalog](guides/REPORT_CATALOG.md) | Developer / support; generated registry projections |
| Deploy and operate | [Deployment](../deploy/README.md), [go-live decisions](operations/go_live_decisions.md) | Platform operator |
| Monitor or upgrade a service | [Service operations](operations/service-operations.md), [upgrade/rollback](operations/upgrade-and-rollback.md) | Platform operator; approved environment required |
| Back up or recover | [Backup runbook](operations/backup_restore.md), [incident runbook](operations/incident_runbook.md) | Operations; [drill log](operations/restore_drill_log.md) is separate historical evidence |
| Verify a connector | [Live connector verification](operations/connector_live_verification.md) | Implementation / integration owner |
| Implement or migrate a customer | [Onboarding workbook](implementation/onboarding-workbook.md), [master data](data/master-data-governance.md) | Implementation/data owners; draft templates require project acceptance |
| Plan cutover, training and support | [Delivery workbook](implementation/delivery-workbook.md) | Customer/implementation owners; execution and sign-off separate |
| Review test coverage | [Verification strategy](qa/verification-strategy.md), [release acceptance](governance/release-acceptance.md) | QA / release reviewers; templates are not executed evidence |
| Verify controls and devices | [Regression/device matrix](qa/control-and-device-matrix.md) | QA/floor/security; physical and human verification required |
| Change documentation safely | [Documentation standard](governance/documentation-standard.md), [generation command](update-docs.ps1) | Author / reviewer; staged generation and read-only checks |
| Review documentation health or a move | [Health and walkthroughs](governance/health-and-walkthroughs.md), [migration/retention register](governance/migration-and-retention-register.md) | Documentation owners; review before committing moves or deleting |

## Review risk, decisions or history

| I need to… | Start here | Authority / limitation |
|---|---|---|
| Review current security work | [Security program](security/README.md) | Owned risk/control work; no blanket certification |
| Review audit or privacy obligations | [Audit/logging policy](security/audit-and-logging-policy.md), [legal register](legal/document-register.md) | Qualified control owners; drafts do not establish compliance |
| See remaining work | [Outcome roadmap](product/roadmap.md), [micro-checklist](micro_checklist.md) | Product outcomes and live work; not release acceptance |
| Read engineering history | [Project ledger](project_ledger.md) | Build record; generated release excerpts require product review |
| Understand the system snapshot | [ERP blueprint](ERP_BLUEPRINT.md) | Legacy snapshot; authority banner explains limitations |
| Review the September audit | [Deep persona audit](audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md) | Dated observations; subsequent fixes need separate verification |
| Understand the documentation program | [Architecture plan](audits/DOCUMENTATION_ARCHITECTURE_PLAN_2026-09-01.md) | Dated proposal driving Stage 48 |
| Review performance priorities | [Smoothness plan](audits/LIGHTWEIGHT_SMOOTHNESS_PLAN_2026-09-01.md) | Proposed budgets and work |
| Review a legal template | [Developer contract](<Contract/Developer Contract.md>) | Draft; qualified legal/business review required |

Review the [Stage 48 verification and open findings](assurance/stage48-verification-2026-09-10.md) and [proposed path migration](governance/migration-review.md).

Older audits, parity plans and closed history remain discoverable in the register.
Their presence does not establish current support. Existing paths are retained until
the Stage 48 baseline, review, migration and retention gates pass.
