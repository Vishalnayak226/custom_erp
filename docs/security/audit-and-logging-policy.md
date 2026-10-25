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
last_verified: 2026-10-25
review_by: 2026-11-25
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

## Security event taxonomy (Stage 49.11.1)

Every class below is an `audit_logs` row (`engines.LogAuditEvent(tenant, user, action, status, details)`), so one query on `action` / `status` finds it. A refused request whose catalog entry is `AuditRequired` is logged with `action` = its error code (`[GLOBAL-0011] …`). Read 2026-10-25 from source.

| Class | `action` / `status` as stored | Where |
|---|---|---|
| Sign-in success / failure / lock | `LOGIN` SUCCESS, PIN_SUCCESS, PIN_FAILED, PIN_LOCKED, ACCOUNT_LOCKED, LOCK_PASSED_REMEMBERED_DEVICE, BOOTSTRAP_CREDENTIAL_EXPIRED; a wrong password as `USERAC-0021` | `handlers_auth.go`, store-app PIN |
| Login abuse | `LOGIN_ABUSE` PASSWORD_SPRAY_SUSPECTED, DISTRIBUTED_GUESSING_SUSPECTED | `login_abuse.go` (134.5) |
| MFA / credential / session lifecycle | `LOGIN` MFA_ENROLLMENT_REQUIRED, MFA_CHALLENGE_ISSUED, MFA_VERIFIED, MFA_ENROLLED_AND_VERIFIED, MFA_RECOVERY_CODES_ISSUED, MFA_RECOVERY_CODE_USED, MFA_DEVICE_REMEMBERED; `PROFILE` PASSWORD_CHANGED, MFA_REENROLL_*, MFA_RECOVERY_CODES_REGENERATED; `AUTH` PASSWORD_RESET_REQUESTED / COMPLETED; `USER_MANAGEMENT` PASSWORD_RESET_BY_ADMIN, PASSWORD_RESET_REQUESTED_DUAL_CONTROL, MFA_RESET_BY_ADMIN | auth, profile, password reset, admin identity |
| Authorization denial | `GLOBAL-0011` (route capability, scope); an out-of-scope record read is answered as not found and still logged as `GLOBAL-0011` (49.4.7) | `apiMiddleware`, `handleGenericDoc` |
| Privilege change | `ROLE_PERMISSIONS` GRANT_UPDATED; `USER_MANAGEMENT` USER_CREATED, ROLE_CREATED, USER_STATUS_CHANGED, USER_STORES_CHANGED, ROLE_STORES_CHANGED, USER_LOCATION_CHANGED, USER_DEPARTMENT_CHANGED, USER_SUPPLIER_LINK_SET | admin identity, users, roles |
| Configuration change | `SETTINGS` UPDATED (each key = value, 47.18.3) | `handlers_settings.go` |
| Secret / integration change | `INTEGRATION` SHOPIFY_*; `API_CREDENTIAL_ISSUED` SUCCESS | connectors, API access |
| Approval / override | the approval engine's `approval_log` (every submit and decision, `[decided by the submitter]` marked); `POS_PRICE_OVERRIDE` | `approval.go`, `pos_quote.go` |
| Tenant-routing failure | `TENANT_ROUTING` REFUSED - a session used on another client's address, on that client's log | `apiMiddleware` |
| Job / webhook give-up | `JOB` DEAD_LETTERED - a job that exhausted its attempts | `jobrunner.go` |
| Operator support | `LOGIN` OPERATOR_OPEN_ISSUED, OPERATOR_SESSION | platform support (125) |
| Deployment / migration | the release ledger (`public.deployments`, Control Panel → Releases) | `-record-release` |
| Bulk export (2026-10-26) | `DATA_EXPORT` SUCCESS (a delivered export or report download), REFUSED_FROZEN | `apiMiddleware` (`isBulkExport`) |
| Detection (2026-10-26) | `SECURITY_ALERT` URGENT / REVIEW - one row per fired rule, naming the rows behind it | `security_detection.go` (49.11.4) |
| Emergency control (2026-10-26) | `USER_MANAGEMENT` SESSIONS_REVOKED, ALL_SESSIONS_REVOKED; `SECURITY_CONTROL` EXPORTS_FROZEN / EXPORTS_UNFROZEN; `EVIDENCE_EXPORTED` SUCCESS | `security_emergency.go`, `security_posture.go` (49.12) |

Not emitted as single events: other sensitive reads, and fraud / invariant exceptions beyond the till-reversal detection. Audit-verifier failure is now the `audit_verify_failed` detection.

**Event schema (Stage 49.11.2).** Each row carries trusted server time (UTC for signed rows), the client (its schema), the actor, `action` / `status` / `details` and, for a request, its **correlation id** (`LogAuditEventCtx`) - the same id as its system-error rows, jobs and outbox events, so one lookup (Activity Log → Security → Trace a request) finds them all instead of copying payloads. `details` passes the same redaction as system logs (passwords, tokens, keys, bearer values) and is bounded to 16 KiB before it is signed. The software version is not repeated per row: it is the release live at that time (`public.deployments`), which the evidence pack includes. An attack is told from a user's mistake by the catalog: only `AuditRequired` refusals (permission, scope, sign-in) are audited; validation errors a user corrects are not. A lost audit write is counted and paged (`AUDIT_WRITE_FAILED`, 49.13.7), never only logged.

For investigations preserve the relevant release, configuration, query/selector, source hashes and collection identity. Record corrections as dated addenda. A hash identifies captured bytes; it does not independently prove completeness, trustworthy time, custody or tamper resistance. Qualified control owners accept evidence sufficiency for the intended use.
