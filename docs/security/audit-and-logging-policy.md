---
doc_id: SEC-LOG-001
title: Audit and operational logging boundaries
type: normative
status: draft
owner: security-owner
approvers: [security-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Audit and operational logging boundaries

Use [the threat model](threat_model.md) and [risk register](risk_register.md) for current control gaps. Operational diagnostics and business audit evidence have different purposes, readers and retention. This draft does not establish regulated-audit readiness; Stage 47.7 and Stage 49 evidence controls remain applicable.

| Record class | Minimum context | Handling |
|---|---|---|
| Business transition | Actor, tenant/scope, object, command, action, result, reason where required, timestamp | Authorized audit review; reconcile with committed business state |
| Authentication/privileged action | Identity reference, event/result, correlation and relevant scope | Security monitoring; exclude passwords, recovery secrets, tokens and MFA seeds |
| Operational failure | Correlation ID, component, error category, time, retry/outcome | Restricted diagnostics; no raw payloads or secret-bearing URLs in routine logs |
| Export or evidence package | Scope, selector, producer, timestamp, artifact hash, recipient authority | Controlled evidence storage and release decision |

Define a per-class retention trigger/duration and legal-hold treatment with the data/legal owner. Do not enable blanket deletion using an undocumented number of days. Restrict log readers, redact known sensitive fields, test failed and replayed commands, and verify whether an audit write participates in the business transaction. Logs alone do not prove a financial postcondition.

Stage 47.7 now implements independently signed events and chained range checkpoints in
[`engines/audit_evidence.go`](../../engines/audit_evidence.go). Transactional callers can use
`WriteAuditEvidenceTx`; the presence of that helper does not prove every business path uses it.
Verification distinguishes legacy sealed rows from independently verified new events, checks
tampering and missing rows in sealed ranges, and reports missed verification jobs. The versioned
retention and legal-hold schema defaults to 365 days hot with automatic deletion disabled.
The encrypted archive, manifest-before-delete path and query/export/restore drill remain open
under 47.7.6. This records the current implementation; qualified retention and evidence acceptance
are still required.

For investigations preserve the relevant release, configuration, query/selector, source hashes and collection identity. Record corrections as dated addenda. A hash identifies captured bytes; it does not independently prove completeness, trustworthy time, custody or tamper resistance. Qualified control owners accept evidence sufficiency for the intended use.
