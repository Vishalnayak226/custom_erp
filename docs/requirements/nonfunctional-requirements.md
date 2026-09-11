---
doc_id: DOC-REQ-NFR
title: Nonfunctional requirements
type: normative
status: draft
owner: engineering-owner
approvers: [product-owner, security-owner, operations-owner, qa-owner]
audience: [engineering, QA, operations, security]
applies_to: declared reference workload and configuration
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Nonfunctional requirements

These are Stage 47 acceptance targets, not current measurements. QA records dataset,
tenants, concurrency, hardware, network and release. Owners approve exceptions with expiry.

| ID | Measurable acceptance | Owner and evidence |
|---|---|---|
| NFR-SEC-001 | Zero unauthorized tenant/entity/location/owner/self/field outcomes across API/jobs/import/export | Security; negative matrix |
| NFR-COR-001 | Retried sale/return/payment/stock command has zero or one reconciled effect under boundary failures | Domain/QA; fault and concurrency totals |
| NFR-PERF-001 | Interactive server p95 ≤150 ms, p99 ≤500 ms at approved representative load | Engineering; workload and distribution |
| NFR-UX-001 | Reference p75 LCP ≤1.5 s, INP ≤200 ms; cached view p95 ≤250 ms | UX/QA; device/browser measurement |
| NFR-COST-001 | Stripped binary ≤25 MiB; idle small-tenant RSS ≤80 MiB; cold core ≤180 KiB compressed; initial core JS ≤120 KiB gzip | Engineering; reproducible artifact/runtime measurements |
| NFR-DOC-001 | KB ≤2 MiB; search ≤250 KiB; ordinary topic ≤120 KiB; zero check-time writes | Documentation; generation safety/size report |
| NFR-ACC-001 | Reference tasks/devices meet the selected accessibility acceptance standard with keyboard, screen reader, reflow, contrast and interruption evidence | Accessibility/QA; token tests alone cannot establish conformance |
| NFR-OPS-001 | Restore meets business-approved RTO/RPO for each deployment class | Operations; values pending before Production; isolated DR drill |
| NFR-DATA-001 | Lists/exports/jobs are bounded; growth, retention, legal hold and recovery have accountable owners | Data/privacy/operations; representative volume and lifecycle tests |
| NFR-COMP-001 | Supported browsers/devices, APIs and extensions have tested compatibility and deprecation contracts | Engineering/implementation; contract matrix |
| NFR-AUD-001 | Protected events remain attributable and verifiable through concurrency, retention and export | Security/auditor; 47.7 design/verifier acceptance pending |
| NFR-DEP-001 | No new mandatory docs runtime, external build download or infrastructure service without measured need and reviewed ADR | Engineering; clean-build/deployment proof |

## Evidence rules

Define sample/window, percentile method and error treatment before measurement. Record
failures, owner, remediation and expiry. Qualified reviewers decide retention/applicability.
Acceptance never transfers automatically across country, owner model, device or deployment.
