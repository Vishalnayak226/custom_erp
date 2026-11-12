---
doc_id: SEC-SESSION-ADR-001
title: ADR: session topology
type: normative
status: approved
owner: security-owner
approvers: [security-owner, engineering-owner]
audience: [engineering, security]
applies_to: browser sessions, cookies and cross-origin requests
authority: proposed-design
confidentiality: internal
last_verified: 2026-11-11
review_by: 2027-02-10
supersedes: none
superseded_by: none
verification_scope: written from the code on 2026-11-07 (erp-5c); Option B approved 2026-11-11, step 1 built (162.15)
---

# ADR: session topology (Stage 49.4.1)

Status: **approved 2026-11-11 (Option B, item 162.15)**. Step 1 is built; see *Decision* at the end. "How it works today" and "Gaps" below describe the code on 2026-11-07, before step 1.

## How it works today

- **Token**: a signed (HMAC) bearer token kept in browser `localStorage` (`erp_token`). No cookies are set or read (pinned by `TestNoRouteAuthorizesFromCookie`).
- **Lifetime**: one fixed lifetime from sign-in, `security.session_token_ttl_hours` (default 24, range 1-720) or env `JWT_EXPIRY_HOURS`. There is no refresh and no rotation: the absolute lifetime is the only server-side limit.
- **Idle timeout**: client side only (`armIdleTimer` in `public/app.js`, per-user `idle_timeout_minutes`, shared across tabs through `erp_last_activity`). The server does not look at idleness.
- **Revocation**: the `cv` claim must equal `users.credential_version`. It is bumped on password change or reset, role-grant change, sign out everywhere (one user or all), and admin "end session". Every outstanding token of that user then fails. One token cannot be revoked alone.
- **Logout**: `logout()` clears browser storage. There is no server logout; a copied token stays valid until it expires or the version is bumped.
- **Concurrent sessions**: unlimited, no device list for ordinary users. Privileged sessions are recorded (`RecordPrivilegedSession`) and can be ended by an admin.
- **Reauth**: sensitive profile and MFA changes ask for the password again (`verifyOwnPassword`).
- **Cache headers**: every API response carries `Cache-Control: no-store` (49.4.5); two read-only lookups set their own private max-age (field formats 300 s, a PIM lookup 86400 s). Attachments are `private, no-store`.

## Gaps

1. The token lives in `localStorage`, so an XSS bug could read it. Mitigated by the strict script CSP (49.4.3) but not removed.
2. Idle timeout is not enforced server side: a stolen token ignores it until expiry.
3. No server logout / single-token revoke.
4. No refresh or rotation: a 24-hour token is a 24-hour bearer secret.
5. No concurrent-session cap or user-visible device list.

## Options

A. Keep as is, document (cheapest; gaps 1-5 stay).
B. Server-side session record (session id in the token, a sessions table, idle and absolute checks, single revoke, device list). Closes 2-5, keeps bearer tokens. Needs a migration and a per-request lookup (cache it).
C. HttpOnly SameSite cookie plus CSRF token. Closes 1 as well but changes every client and the extension/API-key model, and needs the CSRF design from 49.4.2.

## Recommendation

Option B in two steps: first the sessions table with idle and absolute expiry and a real logout (additive, default behaviour unchanged until a tenant turns the idle limit on); decide on C only if a customer requires HttpOnly tokens. This also resolves 47.8.3 (the token-storage question), which stays needs-decision until chosen.

## Decision (2026-11-11)

The owner approved Option B ("All approved", ask 162).

**Step 1, built (162.15):**

- The token's existing `jti` is the session id, so no new claim was added. Table `user_sessions` was added by migration `migrations_stage162_15_user_sessions.sql`. It holds the id, user, started, last seen, expiry, address, device, ended at and ended by.
- `apiMiddleware` records a session the first time it sees it (`UserSessionRefused` in `engines/auth_sessions.go`). That covers every way a token is issued, including tokens issued before the change. The middleware refuses a session that has ended with `GLOBAL-0009`. The check is cached per `authStateCacheTTL`, and last seen is written at most once a minute.
- **Real logout:** `POST /api/v1/auth/logout` ends that one session. If the token belongs to a privileged session, that session ends too. The ERP and the store app call it on Sign out. This closes gap 3.
- **Server idle limit:** `security.session_idle_minutes`, 0 (off) as shipped. Past it, the next request ends the session with `ended_by = idle`. This closes gap 2 for clients that set it.
- Rows are swept hourly once their token has been expired for a day. A missing row reads as a live session, so the sweep never touches a token that is still valid.

**Step 2, not built:** a device list for ordinary users and a cap on concurrent sessions (gap 5). Refresh and rotation (gap 4) also remain. Build these on request.

**Option C** is chosen only if a customer requires HttpOnly tokens (gap 1).

## Small fixes made with this ADR

None needed for cache headers (already no-store everywhere that matters). Related hardening shipped in the same batch: Vary: Origin and exact-origin CORS, an Origin check on writes (report mode), the script-safe file serving headers.
