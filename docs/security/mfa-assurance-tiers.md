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
last_verified: 2026-11-15
review_by: 2027-02-10
supersedes: none
superseded_by: none
verification_scope: tiers built and tested on a scratch database on 2026-11-10 (erp-5c); passkeys built 2026-11-15 and tested with a software authenticator, not yet with a real browser and key
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
| privilege_change | Saving role permissions and rolling a change back (49.3.6). |

## Behaviour

- Off: no change. Report: a would-refuse action runs and writes one audit row per user, tier and status per hour (`MFA_TIER`, `REPORT_WOULD_REFUSE`). Enforce: a person with no authenticator gets 403 with a plain message; the audit row status is `REFUSED`.
- Enforce also makes an enrolled person answer the code at sign-in (a session without it would otherwise carry no proof of a second factor).
- The security posture shows the policy and the 14-day logged count.
- Recommendation: Report only for two weeks, then Required once the count is 0 (see ADMIN_GUIDE).
- Limit: the check is "has an authenticator enrolled", and the enrolled user passed the code at sign-in. There is no per-action step-up prompt (re-entering a code before each approval). That is a design option, not built.

## Recovery codes

Ten codes of ten characters from a 32-character alphabet (no modulo bias). Stored only as SHA-256 of the normalised code (`mfa_recovery_codes.code_hash`); consuming one is a single conditional UPDATE (`used_at IS NULL`), so it works once. Delivery: returned once in the API response at enrollment, activation or re-enrol confirm, and shown on screen for the person to save. They are never emailed, never re-shown; a lost set is replaced by re-enrolling. Tests: `TestMFARecoveryCodeLogin`, `TestMFARecoveryCodesStoredHashedOnly`.

## Passkeys / WebAuthn (49.2.3)

Built with the standard library only (Article 1: no dependency). `engines/webauthn.go` holds a small CBOR reader, the COSE key parser (ES256 and RS256) and the registration and assertion checks; `engines/webauthn_store.go` the tables `user_passkeys` and `passkey_challenges` (migration `49_2_3`).

- **What it is.** A passkey is another way to answer the MFA challenge at sign-in, beside the authenticator code and recovery codes. After the password, the sign-in screen offers it when the person has one (`passkey_available` in the login answer). It needs a device PIN or biometric (user verification is required), and the browser binds the answer to the site, so a look-alike site cannot relay it.
- **Routes.** `GET/POST /api/v1/me/passkeys`, `POST /api/v1/me/passkeys/options`, `DELETE /api/v1/me/passkeys/{id}` (a signed-in person, their own keys only); `POST /api/v1/auth/passkey/options` and `/verify` (the MFA challenge token only, listed in `purposeTokenRoutes`).
- **Checks.** One-time challenge, five minutes, deleted by its first answer (replay refused); ceremony type; origin equals the request's own host and the relying-party id is that host name; user presence and user verification flags; signature over authenticator data plus the hash of the client data; the sign counter must go up when either side counts (a rollback is a cloned authenticator); the counter update is conditional, so two racing answers cannot both pass.
- **Attestation is "none".** The browser's key is taken as given; no vendor trust chain is checked. That is enough for a second factor tied to a person who already signed in; it is not hardware-model assurance. A client that needs a named authenticator model would need attestation verification, which is not built.
- **Lost device.** An administrator's *Reset MFA* removes the person's passkeys with the authenticator and recovery codes, so the lost-device path does not leave a stolen device able to sign in. Ten passkeys per person at most.
- **Tests.** `TestStage4923PasskeyRegistrationAndAssertion` (a software authenticator: wrong challenge, other origin, other relying party, no user verification, tampered data, another key, wrong ceremony, counter replay all refused) and `TestStage4923CBORRefusesHostileInput`. Not tried: a real browser with a real key (needs a device and an HTTPS host).
- **Not done.** Passkeys do not replace the password, and the tiers still test "has an authenticator", not "used a passkey"; making a tier demand a passkey is a later choice.

## Decision: dual approval for a privilege change (49.3.6)

Decided at the recommended answer (ask 193, all recommended approved): a privilege change needs the recent second factor (the `privilege_change` tier above) and is recorded with evidence and a rollback; it does not need a second person. A second approver adds delay for a small team and the change is already reversible and audited. Revisit when a client asks for it; the maker-checker engine is the pattern to reuse.
