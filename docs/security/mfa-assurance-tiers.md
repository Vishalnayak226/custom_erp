---
doc_id: SEC-MFA-TIERS-001
title: MFA assurance tiers
type: normative
status: draft
owner: security-owner
approvers: [security-owner, tenant-owner]
audience: [tenant-admins, security, engineering]
applies_to: security.mfa_tier_policy per tenant; off by default
authority: proposed-policy
confidentiality: internal
last_verified: 2026-11-10
review_by: 2027-02-10
supersedes: none
superseded_by: none
verification_scope: tiers built and tested on a scratch database on 2026-11-10 (erp-5c); passkeys not built
---

# MFA assurance tiers (Stage 49.2.3)

Setting: `security.mfa_tier_policy` per tenant. Values: `off` (default), `report`, `enforce`. Code: `engines/mfa_tiers.go`; enforced once in `apiMiddleware` (`internal/server/middleware.go`) and at sign-in (`handleLogin`).

## Tiers

| Tier | Actions it covers |
|---|---|
| admin | Full-access roles. Gated at sign-in (`RequiresMFA`), not per action. |
| finance_approval | Approval decide and bulk-decide, refund approve, payment-proposal execute and record-UTR, vendor-invoice pay and pay-with-TDS, expense pay. Explicit list `mfaFinanceApprovalRoutes`. |
| payroll | Any route with capability `hr.payroll`. |
| connector_secrets | Capability `admin.integrations` or `admin.api-credentials`, or a route containing `/credentials`. |
| bulk_export | Anything `isBulkExport` recognises. |
| break_glass | Freeze controls, sign out all users, reset a user's MFA. |

## Behaviour

- Off: no change. Report: a would-refuse action runs and writes one audit row per user, tier and status per hour (`MFA_TIER`, `REPORT_WOULD_REFUSE`). Enforce: a person with no authenticator gets 403 with a plain message; the audit row status is `REFUSED`.
- Enforce also makes an enrolled person answer the code at sign-in (a session without it would otherwise carry no proof of a second factor).
- The security posture shows the policy and the 14-day logged count.
- Recommendation: Report only for two weeks, then Required once the count is 0 (see ADMIN_GUIDE).
- Limit: the check is "has an authenticator enrolled", and the enrolled user passed the code at sign-in. There is no per-action step-up prompt (re-entering a code before each approval). That is a design option, not built.

## Recovery codes

Ten codes of ten characters from a 32-character alphabet (no modulo bias). Stored only as SHA-256 of the normalised code (`mfa_recovery_codes.code_hash`); consuming one is a single conditional UPDATE (`used_at IS NULL`), so it works once. Delivery: returned once in the API response at enrollment, activation or re-enrol confirm, and shown on screen for the person to save. They are never emailed, never re-shown; a lost set is replaced by re-enrolling. Tests: `TestMFARecoveryCodeLogin`, `TestMFARecoveryCodesStoredHashedOnly`.

## Passkeys / WebAuthn: NOT BUILT

Design note only. A passkey tier would add a second factor type (and a phishing-resistant one) beside TOTP. It needs: a reviewed WebAuthn library or a careful in-house CBOR/COSE verifier (no dependency was added); a credentials table (credential id, public key, sign count, transports); HTTPS origin and RP-ID binding per tenant host; an enrol and manage UI; a fallback and lost-device path that does not weaken the tiers; and browser/device compatibility testing. Until then TOTP plus recovery codes is the only second factor.
