---
doc_id: DOC-HUMANOPS0001
title: Secure human operations, support, devices and anti-social-engineering controls
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-16
review_by: 2026-10-16
supersedes: none
superseded_by: none
---

# Secure human operations, support, devices and anti-social-engineering controls

**Stage 49.14** — established 2026-09-16. Companion to
[threat_model.md](threat_model.md) (the adversary personas this item designs
for: malicious cashier/employee/manager/tenant admin, hostile tenant, support
engineer, accidental operator), [risk_register.md](risk_register.md) and
**49.2** (identity lifecycle — 49.14.8 depends on 49.2.1, still open). This
document is scoped to *people, process and device posture*; it does not
duplicate **49.3** (authorization/tenant isolation, not yet built) or
**49.4** (session/browser hardening, not yet built) — those own the technical
controls a social-engineering or physical-access attack would ultimately have
to defeat.

> **Status of this document, read the same way as its siblings
> ([vulnerability-disclosure-lifecycle.md](vulnerability-disclosure-lifecycle.md),
> [customer-security-pack.md](customer-security-pack.md)).** This repository
> has exactly one real contributor (`.github/CODEOWNERS`,
> `@Vishalnayak226`). Every place 49.14's own wording implies an
> organization — "support operator," "role-specific practice," "personnel
> controls" — is written honestly for a single-owner project today. Several
> sub-items below are consequently **not built**, named as gaps rather than
> quietly assumed, because building process for people who do not yet exist
> would be exactly the parallel-prose problem **49.16.1**'s note already
> warned against.

---

## 49.14.1 — Plain-language security UX

**Built, and already the established convention across the codebase — not a
new pattern introduced here.** Every authentication/session/recovery/lookup
boundary in this tree already answers with one generic, non-enumerating
message rather than a specific one, so a caller cannot learn *why* something
failed only *that* it did:

- `internal/server/handlers_auth.go` — a disabled account, a sandbox-expired
  account, a wrong password and a dead session token all fail with the "same
  generic failure as a bad password," by explicit comment at each branch.
- `internal/server/middleware.go` — an expired token and a revoked/demoted
  live-state token both return the identical generic 401 (`stage29_8_test.go`
  pins this: "no leak about why").
- `internal/server/middleware_public_api.go` — every public-API auth failure
  (`publicAPIAuthFailureMessage`) is one shared constant, not a
  per-cause string.
- `internal/server/handlers_pim_import_template.go` — "no such token" and a
  malformed token deliberately share one message.
- `internal/server/handlers_pim_export.go` and the generic document API's
  404 handling — "which of those is true is not information [disclosed]," by
  comment.
- Password reset (49.2.4) already answers "if that account exists, a reset
  link was sent" uniformly regardless of whether the account exists.

**What this item does not claim**: a formal per-screen review of every
UI-facing string in `public/app.js` for tone/clarity (as opposed to
non-enumeration, which is what's verified above) has not been done.
`[needs UX audit: a full pass over public/app.js's security-relevant copy —
step-up prompts, approval banners, error toasts — for plain-language quality,
not just information disclosure]`. The non-enumeration property itself is the
part with real security consequence, and that part is true today, checked
against actual source rather than asserted.

---

## 49.14.2 — Privileged/admin UX

**Partially built**, via 49.2.4 rather than a dedicated UX pass:

- **Dual-control for a privileged target** — `engines/admin_password_reset.go`
  requires a second, different Super Admin to approve resetting *another*
  Super Admin's password (maker-checker refuses same-actor
  approval), with a mandatory reason once the target is privileged.
- **Reauthentication for a sensitive self-change** — `handleUpdateProfile`
  requires the caller's current password before an email change actually
  takes effect; `/me/mfa/reenroll` already requires the account password.
- **Reason is mandatory, not optional, for an irreversible action** —
  `RequestTenantDeprovision` (`engines/tenant_lifecycle.go`) refuses an empty
  reason outright: "a reason is required and is recorded in the tenant's
  evidence trail."

**Not built**: before/after permission diff, blast-radius preview, an
explicit tenant/environment banner, and typed confirmation using a human
business name (rather than a UUID) before an irreversible action. These are
**49.3.6**'s ("Privilege-change safety") stated acceptance bar, not a second
copy of it — 49.3.6 stays the owner and stays open. What exists today
(dual control + mandatory reason + audit trail) is real containment, just not
the full UX described by 49.14.2's own wording.

---

## 49.14.3 — Floor/shared-device posture

**Not built — a genuine, honestly-stated gap, not a partial one.** Grepped
for any badge/PIN/fast-switch/shared-device login path across the Go source:
none exists. Every login, on every device including a POS terminal, is the
same full username+password(+MFA-where-required) flow as an office login;
there is no individual-fast-sign-in, no short role-appropriate lock timer
distinct from the ordinary session idle timeout, and no privacy-safe
lock-screen concept.

What already exists and *partially* mitigates the risk this item is really
about (a shared terminal left logged in): the ordinary session idle/absolute
lifetime and Stage 29.8's live-state re-check, which revokes an in-progress
session within its polling interval if the account is disabled/demoted —
general session hygiene, not a floor-specific control.

`[needs product decision: is a shared-device fast-switch flow in scope at
all for this ERP's POS module, given the lightweight-by-design constraint —
a badge/PIN layer is a real UX and security-surface addition, not a small
one, and should not be built speculatively ahead of a concrete cashier-facing
complaint]`.

---

## 49.14.4 — Support has no invisible backdoor

**Built, and independently verified by an executed scanner — the strongest
sub-item in this document.** `internal/securityscan/bypass.go` (49.1.4) scans
the entire source tree for magic identity comparisons, impersonation paths
and hidden support back doors; its own reviewed allowlist
(`bypass_test.go`) is **empty of any impersonation or back-door finding** —
the only allowlisted entries are the four shipped bootstrap credentials and
the code that exists specifically to refuse them (`security_baseline.go`'s
SB-021). There is no "log in as this user," no shared support account and no
magic tenant/role override anywhere in this codebase, checked by an
executed test rather than asserted from memory
(`TestNoUnreviewedBypassPattern`, re-run every `go test ./...`).

The one place a support-shaped action exists at all is **guided
remediation, not impersonation**: `engines/admin_password_reset.go` lets an
admin reset a user's credential (evidenced, single-admin for an ordinary
target, dual-control for a Super Admin target — 49.2.4) without ever
authenticating *as* that user. This is exactly the item's own stated
preference ("prefer guided diagnostics over impersonation") already built,
not a placeholder for a future one.

**What does not exist, and is deliberately not invented here**: a
ticket-bound, time-limited, customer-consented delegated support-access
*feature* (a formal "support opens a scoped session into this tenant with
the tenant's own consent and a visible indicator" flow). Item 49.14.4's own
wording treats that as the fallback for when guided diagnostics is not
enough — at today's single-owner scale, guided diagnostics covers every real
support action this project has needed. `[needs product decision: build a
formal delegated-access flow only once a real support workflow needs to see
inside a tenant's data, rather than ahead of that need]`.

---

## 49.14.5 — Governed break-glass

**Not built.** Two existing mechanisms are adjacent but neither is
break-glass as this item defines it:

- `ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=1` (`engines/environment.go`,
  documented in [README.md](README.md)'s key-inventory section and reused
  throughout Stage 47/49) is a system-wide kill switch for outbound side
  effects — global, not scoped to a named eligible identity, and not itself
  gated by dual custody.
- Ordinary Super Admin capability plus 49.2.4's dual-control-for-privileged-
  targets is strong routine containment, but it is not time-boxed, does not
  require an incident/reason link to *activate*, and produces no
  break-glass-specific alert.

No named eligible break-glass identity, no dual-custody activation, no
automatic page-on-use, and no forced post-use rotation/review exists.
`[needs decision: security-owner + product]` — whether a formal break-glass
mechanism is worth the added surface at this scale, given that 49.2.4's
dual-control-for-Super-Admin-targets plus the full audit trail already gives
an equivalent (if less automated) containment/evidence property for the one
real emergency scenario this project has needed so far (a locked-out Super
Admin). Building the full mechanism ahead of a second administrator existing
would be speculative.

---

## 49.14.6 — Secure device/browser baseline

**Partially documented, not formalized.**
[`docs/guides/QZ_PRINTING_SETUP.md`](../guides/QZ_PRINTING_SETUP.md) is the
one existing "declared POS/RF endpoint" document — QZ Tray install,
version, and the CSP exception Stage 40.8 found and fixed
(`connect-src` needed `ws://localhost:*`/`wss://localhost.qz.io:*` for any
QZ-based silent print to ever reach a real printer). **47.6.6** (device/
scanner/printer certification, the gloves/weak-Wi-Fi/interruption/app-restart
run) stays open and explicitly needs physical hardware not available from
this development environment — not duplicated here.

Not built: a formal cross-device baseline document naming supported browser
versions, lock/encryption/update posture expectations, kiosk-escape
prevention, or a decommission-wipe procedure. `[needs decision: write this
once 47.6.6's physical-hardware pass has real devices to describe, rather
than write a baseline for hardware nobody has tested against yet]`.

---

## 49.14.7 — Role-specific practice

**Not built — deliberately, at this project's current scale.** There is no
onboarding curriculum, phishing simulation or periodic security exercise
program; a single-contributor project has no audience for one yet. `[needs
decision: security-owner — a formal training program is worth building once
a second person joins the project, not ahead of that]`.

---

## 49.14.8 — Personnel controls

**Partially true by construction, not by a built control.**
`.github/CODEOWNERS` (49.9.2) names the one real contributor
(`@Vishalnayak226`) across every sensitive path (auth/tenant/finance/
inventory/migrations/deploy/security) — "least repository access" is
trivially satisfied when there is exactly one person with access at all, the
same honest framing 49.15/49.16 already use. Dual control for signing keys,
backups, the production database and security-policy changes is **not
built**: no signing key exists yet (tracked as 49.9.7's own open gap, not
duplicated here), and backups/production-DB access are today a single
operator's SSH session, not a two-person control.

**Joiner/mover/leaver evidence and periodic access recertification** — the
harder, identity-lifecycle half of this item — belongs to **49.2.1**
("Identity lifecycle"), which stays open; 49.14.8 does not attempt a second,
parallel version of it. `[needs decision: security-owner/business-owner —
dual control for backups/production DB/signing keys is worth building once
a second operator exists to be the second control]`.

---

## Acceptance, read honestly

Item 49.14's stated acceptance is: *"a legitimate floor user can work
quickly without shared credentials; support can help without universal
access; a phished/helpdesk-targeted/angry insider identity meets multiple
observable containment boundaries; emergency access is faster than
improvisation and never invisible."*

**Met today**: support cannot reach universal access — verified by an
executed scanner, not aspiration (49.14.4). Non-enumerating, plain-language
failure responses are the consistent default at every checked boundary
(49.14.1).

**Not met today, and named rather than hidden**: a floor user's login is not
fast or badge-based (49.14.3 — an open product question, not an oversight);
emergency access is exactly as fast as the ordinary dual-control admin-reset
path and no faster, because no dedicated break-glass mechanism exists yet
(49.14.5); privileged UX has the containment (dual control, mandatory
reason) but not the preview/diff/typed-confirmation layer (49.14.2); device
baseline, training and personnel dual-control remain open, each tied to a
named decision or to a prerequisite (47.6.6's hardware, 49.2.1's identity
lifecycle, 49.9.7's signing key, or simply a second person existing) rather
than to missing effort.
