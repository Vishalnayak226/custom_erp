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
| Understand the business purpose | [BRD](requirements/BRD.md), [PRD](requirements/PRD.md) | Product leadership; legacy drafts awaiting rebuild |
| Know what my configuration supports | [Support authority and gaps](governance/authority-matrix.md) | Buyer / product owner; approved capability catalog pending 48.2 |
| Learn the everyday workflow for my role | [Role journeys](kb/role-journeys/role-journeys-overview.md) | Cashier, store manager, warehouse, finance, category manager, admin |
| Set up a shop and make a first sale | [Onboarding tutorial](kb/getting-started/open-a-shop-and-make-your-first-sale.md) | Business owner / tenant admin |
| Find a task or recovery instruction | [User guide](guides/USER_GUIDE.md), [troubleshooting](kb/troubleshooting/troubleshooting-index.md) | Users; KB topics are the canonical task source |
| Configure users or operations | [Admin guide](guides/ADMIN_GUIDE.md), [security and approvals](kb/module-handbooks/security-approvals.md) | Tenant admin; operator content split pending 48.8 |
| Run user acceptance checks | [UAT checklist](guides/UAT_CHECKLIST.md), [run-sheet template](operations/uat_run_sheet.md) | QA / implementation; blank checks are not signed acceptance |
| Use a procedural template | [User SOP](guides/USER_SOP.md), [Admin SOP](guides/ADMIN_SOP.md) | Process owner; customer approval required before local use |

## Build, integrate, deploy or recover

| I need to… | Start here | Audience / authority |
|---|---|---|
| Resume development | [Handover §6](ai_handover.md#6-version-control--handover-status) | Developer; shared-tree state and next action |
| Locate a subsystem | [Project brain](brain/README.md), [framework architecture](architecture/framework_architecture.md) | Engineer; verify graph inferences in source |
| Integrate through the API | [Public API](specs/public_api_v1.md), [generated OpenAPI](specs/openapi_public_v1.json) | Integrator; generated contract plus authored usage |
| Read errors or reports | [Error reference](guides/ERROR_CODES.md), [report catalog](guides/REPORT_CATALOG.md) | Developer / support; generated registry projections |
| Deploy and operate | [Deployment](../deploy/README.md), [go-live decisions](operations/go_live_decisions.md) | Platform operator |
| Back up or recover | [Backup runbook](operations/backup_restore.md), [incident runbook](operations/incident_runbook.md) | Operations; [drill log](operations/restore_drill_log.md) is separate historical evidence |
| Verify a connector | [Live connector verification](operations/connector_live_verification.md) | Implementation / integration owner |
| Change documentation safely | [Documentation standard](governance/documentation-standard.md), [generation command](update-docs.ps1) | Author / reviewer; staged generation and read-only checks |

## Review risk, decisions or history

| I need to… | Start here | Authority / limitation |
|---|---|---|
| Review current security work | [Security program](security/README.md) | Owned risk/control work; no blanket certification |
| See remaining work | [Micro-checklist](micro_checklist.md) | Live Stage work, not release acceptance |
| Read engineering history | [Project ledger](project_ledger.md) | Build record; generated release excerpts require product review |
| Understand the system snapshot | [ERP blueprint](ERP_BLUEPRINT.md) | Legacy snapshot; authority banner explains limitations |
| Review the September audit | [Deep persona audit](audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md) | Dated observations; subsequent fixes need separate verification |
| Understand the documentation program | [Architecture plan](audits/DOCUMENTATION_ARCHITECTURE_PLAN_2026-09-01.md) | Dated proposal driving Stage 48 |
| Review performance priorities | [Smoothness plan](audits/LIGHTWEIGHT_SMOOTHNESS_PLAN_2026-09-01.md) | Proposed budgets and work |
| Review a legal template | [Developer contract](<Contract/Developer Contract.md>) | Draft; qualified legal/business review required |

Older audits, parity plans and closed history remain discoverable in the register.
Their presence does not establish current support. Existing paths are retained until
the Stage 48 baseline, review, migration and retention gates pass.
