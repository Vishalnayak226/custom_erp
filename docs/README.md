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
last_verified: 2026-10-19
review_by: 2027-01-19
supersedes: none
superseded_by: none
---

# ERP documentation

Find the row that matches what you want to do. Each link opens the one page that answers it.
The [folder map](#folder-map) at the end explains what every folder holds.

## I use the ERP

| I want to… | Read |
|---|---|
| Learn the ERP, screen by screen | [User guide](guides/USER_GUIDE.md) |
| Learn my own daily job (cashier, store manager, warehouse, finance, category manager, admin) | [Role journeys](kb/role-journeys/role-journeys-overview.md) |
| Open a shop and make the first sale | [Getting started](kb/getting-started/open-a-shop-and-make-your-first-sale.md) |
| Follow a step-by-step procedure | [User SOP](guides/USER_SOP.md), [video SOPs](sop-video/readme.md) |
| Understand an error message | [Reading an error code](kb/troubleshooting/error-codes.md), [all error codes](guides/ERROR_CODES.md), [troubleshooting](kb/troubleshooting/troubleshooting-index.md) |
| Find the right report | [Report catalog](guides/REPORT_CATALOG.md) |
| Print stickers, labels and documents | [Printing setup](guides/QZ_PRINTING_SETUP.md) |
| Look up a term or abbreviation | [Glossary](kb/reference/glossary.md), [abbreviations](kb/reference/abbreviations.md), [FAQ](kb/reference/faq.md) |
| Print or read the whole manual offline | [User manual](user/user-manual.html) |

The same help topics open inside the app from **Help** on every screen.

## I set up the ERP for my company

| I want to… | Read |
|---|---|
| Configure the company, stores, users, roles and approvals | [Admin guide](guides/ADMIN_GUIDE.md), [Admin SOP](guides/ADMIN_SOP.md) |
| Understand who can approve what | [Security and approvals](kb/module-handbooks/security-approvals.md) |
| Print or read the admin manual offline | [Tenant admin manual](user/tenant-admin-manual.html) |
| Bring in a new customer and their data | [Onboarding workbook](implementation/onboarding-workbook.md), [data handbook](data/data-handbook.md) |
| Plan cutover, training and support | [Delivery workbook](implementation/delivery-workbook.md) |
| Run acceptance tests before go-live | [UAT checklist](guides/UAT_CHECKLIST.md), [UAT run sheet](operations/uat_run_sheet.md) |

## I build or change the ERP

| I want to… | Read |
|---|---|
| Get the code running on my machine | [Developer setup](engineering/developer-setup.md), [contributing](engineering/contributing.md) |
| Pick up where the last session stopped | [Handover, section 6](ai_handover.md#6-version-control) |
| See what is left to build | [Micro checklist](micro_checklist.md) (the live backlog) |
| See what was built, and when | [Project ledger](project_ledger.md) |
| Find where a piece of code lives | [Project brain](brain/README.md) |
| Understand how the system is built | [Current architecture](architecture/current-architecture.md), [data and runtime views](architecture/data-and-runtime-views.md), [decisions](architecture/decisions/decision-register.md) |
| Integrate through the public API | [API overview](api/overview.md), [API contract](api/public-api-v1.md), [OpenAPI file](api/generated/public-v1.json) |
| Look up a table or field | [Data dictionary](data/generated/dictionary.md) |
| Plan tests | [Verification strategy](qa/verification-strategy.md), [device matrix](qa/control-and-device-matrix.md) |
| Let an outside developer build a client-only extension | [Extension hooks checklist](engineering/extension-hooks-checklist.md) |
| Keep the GitHub repository locked down | [GitHub security checklist](engineering/github-security-checklist.md) |

## I run the servers

| I want to… | Read |
|---|---|
| Deploy a release | [Deployment](../deploy/README.md), [running the service](operations/service-operations.md) |
| Back up or restore | [Backup and restore runbook](operations/backup_restore.md), [restore drill log](operations/restore_drill_log.md) |
| Handle an outage or incident | [Incident runbook](operations/incident_runbook.md) |
| Check what was decided for go-live | [Go-live decisions](operations/go_live_decisions.md), [hypercare plan](operations/hypercare_plan.md) |
| Prove a marketplace or courier connector works | [Live connector verification](operations/connector_live_verification.md) |
| Scope a penetration test | [Pentest scope](operations/pentest_scope.md) |

## I decide what gets built

| I want to… | Read |
|---|---|
| Know why the product exists and where it is going | [Vision](product/vision.md), [roadmap](product/roadmap.md) |
| Know what is supported today | [Capability catalog](generated/capability-catalog.md), [requirements traceability](generated/requirements-traceability.md) |
| Read the requirements | [Business requirements](requirements/business-requirements.md), [product requirements](requirements/product-requirements.md), [non-functional requirements](requirements/nonfunctional-requirements.md) |
| Read market and competitor research | [Market](product/research/market-intelligence.md), [OMS benchmark](product/research/oms-benchmark.md), [WMS benchmark](product/research/wms-benchmark.md) |

## Security, privacy and legal

| I want to… | Read |
|---|---|
| See the security programme, threats and risks | [Security overview](security/README.md), [threat model](security/threat_model.md), [risk register](security/risk_register.md) |
| Answer a customer's security questionnaire | [Customer security pack](security/customer-security-pack.md) |
| Find a contract, policy or privacy decision | [Legal register](legal/document-register.md), [privacy applicability](legal/privacy-applicability.md) |

## Old material

Past audits, superseded plans and specifications, earlier handovers and closed checklist stages
are in the [archive](archive/readme.md). Its table also says where every moved file went.
Nothing there describes the system as it is today.

## Folder map

| Folder | What is inside |
|---|---|
| `guides/` | The user and admin guides, SOPs, UAT checklist, error codes, report catalog, printing setup |
| `kb/` | Help topics shown inside the app (built into the server) |
| `user/` | Printable user and admin manuals, generated from `kb/` |
| `sop-video/` | Video walk-throughs of common tasks |
| `implementation/` | Workbooks for bringing a new customer live |
| `product/` | Vision, roadmap, capability register, feature plans and `research/` |
| `requirements/` | Business, product, control and non-functional requirements, one page per module in `modules/` |
| `architecture/` | How the system is built, design specifications and `decisions/` |
| `api/` | Public API guide, contract, message catalog and the OpenAPI file |
| `data/` | Data handbook, business definitions and the generated data dictionary |
| `engineering/` | Developer setup, contributing and engineering checklists |
| `qa/` | Test strategy and the audit and browser-test scripts |
| `operations/` | Runbooks for deploy, backup, incidents, go-live and hypercare |
| `security/` | Security programme, threat model, risk registers and policies |
| `legal/` | Legal register, privacy decisions and contract templates |
| `specs/reports/` | The report catalog's source data (read by `cmd/gencatalog`) |
| `governance/` | Rules for writing and checking these docs, and the document register |
| `generated/` | Machine-made pages; never edit them by hand |
| `brain/` | The code map: which file belongs to which part of the system |
| `archive/` | Everything dated or superseded, kept for the record |

The three files at the top of this folder are the project's working records: the
[micro checklist](micro_checklist.md) (what is still to do), the [project ledger](project_ledger.md)
(what was built) and the [AI handover](ai_handover.md) (the state of the working tree).

## Keeping these docs healthy

Writing rules are in the [documentation standard](governance/documentation-standard.md). After changing
docs, run `pwsh docs/update-docs.ps1` to rebuild generated pages and `go run ./cmd/doclint` to check
links. Pages under `generated/`, `brain/`, `user/`, `data/generated/` and `api/generated/` and the two
catalogs in `guides/` (error codes, report catalog) are rebuilt by tools; change their source instead.
