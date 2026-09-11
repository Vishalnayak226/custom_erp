---
doc_id: QA-MAT-001
title: Regression, performance and device acceptance matrix
type: template
status: draft
owner: qa-owner
approvers: [qa-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Regression, performance and device acceptance matrix

Use this matrix with the [verification strategy](verification-strategy.md). Record executable tests and dated results separately; this is not a passed test report.

| Area | Required sample / negative case | Evidence and release gate |
|---|---|---|
| Sale/return economics | Original price/tax, replay, concurrent tender/refund, rollback, wrong role/scope | Reconciled sale/stock/GL/payment/loyalty and Stage 47.2–47.4 tests |
| Warehouse ownership | Concurrent assignment, second-owner refusal, reassignment with stock, lot/serial and alternate write paths | Verify the default 47.5 single-owner guard; flag unsupported mixed mode; no mixed-owner isolation claim |
| Identity and SoD | Revoked/old token, wrong tenant/location, self-approval, import/export/report alternatives | No protected effect/disclosure; Stage 49 controls |
| Data lifecycle/audit | Retention/hold, replay/failure event, export and restore scope | Qualified owner review; no inferred compliance |
| Performance/capacity | Representative data size, concurrent roles, cold/warm state and growth | p50/p95/p99, errors, server/browser bytes, memory, DB/lock/queue observations against approved NFR |
| Resilience | Process/database restart, provider timeout, uncertain outcome and retry | One durable business outcome and reconciled external effects |
| Accessibility | Keyboard-only navigation, focus, headings/labels, screen reader, zoom and contrast | Real task completion; record browser/OS/assistive technology |
| Floor devices | Scanner keyboard wedge, touch gloves where relevant, printer, offline/reconnect and RF viewport | Physical reference devices; desktop simulation cannot sign off hardware |
| Help/manuals | Portal → role/topic in two decisions; task help, search, error recovery, print links | Observed findability and task outcome, source/release date and reviewer |

## Isolated test data

Use synthetic fixtures with explicit tenant/location/owner labels and deterministic business identities. Record seed/migration version and reset/cleanup scope. Do not use production exports, personal records or live payment credentials. Serialize packages that share legacy development fixtures; use supported test timeouts to expose a stalled database/provider operation. Cleanup must target only test-owned records.

## Result record

Fill in configuration, release/artifact hash, test case ID, role/device, fixture, expected/actual outcome, timing/counts, evidence location, failure severity/owner and approval. Record blocked or unexecuted cases explicitly. Apply the [release acceptance gate](../governance/release-acceptance.md); a test-file link is not an execution result.
