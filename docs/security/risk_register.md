---
doc_id: DOC-883F429D51
title: Security risk register
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner]
applies_to: source documentation; scoped release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# Security risk register

**Stage 49.0.6** — opened 2026-09-06. Companion to [threat_model.md](threat_model.md).

> **Acceptance authority is not yet assigned.** 49.0.6 requires that a critical or
> high risk cannot be accepted by its own implementer, and that exceptions are
> time-bound and narrowly scoped. No such authority exists for this project yet, so
> **every row below is Open or Closed — none is Accepted**, and the Owner column
> names the role that must own it rather than pretending one has been appointed.
> Appointing them is part of 49.0's acceptance criteria and is not something a
> build session can do.

## How to read a row

- **Exposure** — which of the supported configurations in threat_model.md §1.1 it
  affects (D1 single-box, D2 per-tenant hostnames, D3 dev, D4 sandbox).
- **Detectability** — whether the system would tell anyone if this were exploited
  today. "None" is itself a finding, not a footnote.
- **Evidence** — what proves the current state, either way. A row with no evidence
  link is an opinion.
- **Review** — the date by which the row must be re-examined even if nothing has
  changed. A risk with no expiry becomes a permanent excuse.

Severity is the pair (likelihood, impact) resolved to one word, in the same
vocabulary the deep persona audit used: **Critical** ≈ its P0, **High** ≈ its P1.

---

## Open risks

### R-01 — Re-running `db/migration.sql` resets every rotated bootstrap password

| | |
|---|---|
| **Severity** | High (likelihood: low; impact: critical) |
| **Exposure** | D1, D2, D3 |
| **Detectability** | Partial — SB-021 reports it at the *next* boot, so the window is one restart, not zero. |
| **Found** | 2026-09-06, Stage 49.1.3 |

`db/migration.sql`'s users INSERT ends `ON CONFLICT (id) DO UPDATE SET password_hash
= EXCLUDED.password_hash`. An operator who re-runs the base schema file — a
reasonable thing to believe is idempotent and safe — silently restores four known
passwords, one of them (`system`) carrying the Super Admin role, over whatever the
deployment had rotated them to.

- **Treatment:** change the conflict clause so a rotated credential is never
  overwritten (`DO NOTHING`, or a guard on the current hash), as part of 49.1.5's
  secure-provisioning work. Until then, SB-021 is the compensating control and the
  runbook must say never to re-run the base schema against a live database.
- **Owner:** infrastructure/deployment, with the 49.1.5 implementer.
- **Evidence:** `db/migration.sql:147-152`; `TestSeededCredentialHashesCoverMigrationSQL`.
- **Review:** 2026-12-06, or at 49.1.5 closure.

### R-02 — Four bootstrap credentials ship in the repository

| | |
|---|---|
| **Severity** | Critical if any is live in production; otherwise High |
| **Exposure** | D1, D2, D3 |
| **Detectability** | Good — SB-021 refuses an `ENV=production` boot while any is active, across every tenant schema. |
| **Found** | 24.27 for `admin`; the other three on 2026-09-06 |

`admin`, `cashier1`, `manager1` and `system` are created with literal bcrypt hashes
anyone with the source can crack or simply recognise. Before 2026-09-06 only `admin`
was checked, in `tenant_default` only — so `system` (Super Admin) and two others
could be live in production with a published password and nothing would say so.

- **Treatment:** replace the seed with a cryptographically random one-time bootstrap
  credential that must be rotated at first login (49.1.5). The blocking startup check
  is in place now and is the interim control.
- **Owner:** security, with the 49.1.5 implementer.
- **Evidence:** `engines/security_baseline.go` (SB-021); `internal/securityscan/bypass_test.go` reviewed entry `seeded-credential`.
- **Review:** 2026-12-06, or at 49.1.5 closure.

### R-03 — Connector credential key can exist only on one host, outside every backup

| | |
|---|---|
| **Severity** | High (likelihood: medium; impact: high) |
| **Exposure** | D1, D2 with any sales-channel connector configured |
| **Detectability** | Good since 2026-09-06 — SB-022 blocks a production boot once `channel_credentials` holds rows and `CHANNEL_CREDENTIAL_KEY` is unset. |
| **Found** | 2026-09-06, Stage 49.1.3 |

With `CHANNEL_CREDENTIAL_KEY` unset, `engines/channel_credentials.go` generates an
AES-256 key into the OS user config directory. The database backup then contains the
ciphertext of every Shopify/BigCommerce/Magento API token but not the key: a restore
onto a replacement host silently cannot decrypt them, and losing the host loses the
credentials with no way to recover them. `deploy/erp.env.example` lists the variable
under "Optional", which is how a deployment reaches this state by following the
documentation correctly.

- **Treatment:** SB-022 (done). Move the variable into the required section of the
  env example when connectors are in use, and give it a place in the 49.6.5 key
  inventory with a rotation and re-encryption path.
  **Update 2026-09-11 (Stage 49.6.5):** the rotation and re-encryption path now
  exists — `engines/secret_keyring.go` generalizes the JWT signing-key keyring
  (Stage 29.8) to `CHANNEL_CREDENTIAL_KEY_<n>`, and
  `tenantctl reencrypt-channel-credentials` completes a rotation by re-sealing
  every stored row under the current key; `deploy/erp.env.example` documents
  both and moves the bare var into a clearly-labeled "required if you use a
  connector" section. SB-007/SB-022 and the new SB-023 (placeholder detection)
  all recognise the keyring form. This closes the TREATMENT; it does not by
  itself put a real managed key into any live deployment's `CHANNEL_CREDENTIAL_KEY`
  or back it up off-host, which is a deployment action outside this session's
  reach — the row therefore stays **Open**, not Closed, until an operator
  confirms that action.
- **Owner:** infrastructure, with security review.
- **Evidence:** `engines/security_baseline.go` (SB-007, SB-022, SB-023);
  `engines/secret_keyring.go`, `engines/secret_keyring_test.go`,
  `engines/channel_credentials_rotation_test.go`; `deploy/erp.env.example`;
  `docs/security/README.md` key inventory.
- **Review:** 2027-03-06, or at 49.6.5 closure.

### R-05 — Two unauthenticated-allowlist entries name routes that do not exist

| | |
|---|---|
| **Severity** | Low today; High if a route is later registered at either path |
| **Exposure** | D1, D2 |
| **Detectability** | Good — `TestPublicRouteAllowlistHasNoNewDeadEntries` holds the count at exactly these two. |
| **Found** | 2026-09-06, Stage 49.1.1 |

`internal/server/middleware.go`'s `publicRoutes` allowlists
`/api/v1/integration/courier/delhivery/tracking` and
`/api/v1/integration/courier/shiprocket/tracking`. No handler exists for either
anywhere in the tree. They cause no harm now — the request 404s before anything —
but they pre-authorise whatever is registered at those paths in future, which is
precisely how an internal route becomes public without anyone deciding it.

- **Treatment:** delete both entries, or register the Stage 35.5 routes they were
  written for. Deliberately not done in the session that found it, because
  `middleware.go` was under concurrent edit; it is a two-line change for whoever
  next owns that file.
- **Owner:** whoever next changes `internal/server/middleware.go`.
- **Evidence:** `internal/securityscan/surface_test.go` (`knownDeadPublicRouteEntries`).
- **Review:** 2026-12-06.

### R-06 — The shipped environment example contains a placeholder signing key

| | |
|---|---|
| **Severity** | Medium (likelihood: low; impact: critical) |
| **Exposure** | D1, D2 |
| **Detectability** | Good since 2026-09-06 — SB-004 refuses a production boot on this exact value. |
| **Found** | 2026-09-06, Stage 49.1.3 |

`deploy/erp.env.example` ships `JWT_SECRET=CHANGE_ME_openssl_rand_hex_48`. A copied
env file that reaches production unedited would give every reader of this repository
the ability to forge a token for any user in any tenant.

- **Treatment:** SB-004's placeholder detector now includes this exact shape (done).
  The residual risk is other placeholder shapes; the detector is a smell test, not a
  proof, and 49.6.6's leak scanning is the durable answer.
- **Owner:** security.
- **Evidence:** `engines/security_baseline.go` (`isLowEntropyPlaceholder`);
  `TestPlaceholderSigningKeysAreRejected`.
- **Review:** 2027-03-06.

### R-07 — Runtime, migration and backup identities are the same account

| | |
|---|---|
| **Severity** | High |
| **Exposure** | D1, D2 |
| **Detectability** | None. |
| **Found** | 2026-09-06, while modelling persona P6 |

The application runs as the `erp` Unix user with one `DATABASE_URL`; migrations and
backups use the same credential. Compromise of the running process therefore also
grants schema-modification and full-dump ability, which is the difference between a
data breach and a total one.

- **Treatment:** 49.7.1 — separate least-privilege PostgreSQL roles for runtime,
  migration and backup, and separate OS identities where the deployment allows.
- **Owner:** infrastructure/SRE.
- **Evidence:** `deploy/erp.service`, `deploy/erp.env.example`, `deploy/backup.sh`.
- **Review:** at 49.7.1 closure.

### R-08 — No governed break-glass or support-access evidence path

| | |
|---|---|
| **Severity** | High |
| **Exposure** | D1, D2 |
| **Detectability** | None. |
| **Found** | 2026-09-06, while modelling persona P6 |

There is no impersonation or hidden support path in the code — the 49.1.4 scan found
none, which is the good version of this — but that means host access is the only way
to help a customer, and host access leaves no tenant-visible trail. The absence of a
back door and the absence of a governed front door are the same gap.

- **Treatment:** 49.14.4 (consented, ticket-bound, time-limited delegated access) and
  49.14.5 (named break-glass identities with immediate alerting and forced review).
- **Owner:** product + security.
- **Evidence:** `internal/securityscan/bypass.go` `bypass-vocabulary` scan, clean.
- **Review:** at 49.14 closure.

### R-10 — Static asset server answered every HTTP method, not just GET/HEAD

| | |
|---|---|
| **Severity** | Low (no reflection/echo, no browser can send a real `TRACE`; still a real allowlist gap) |
| **Exposure** | D1, D2 |
| **Detectability** | None before this session; now caught by `TestOnlyReadMethodsRejectsEverythingButGetAndHead` and by re-running `cmd/edgecheck`. |
| **Found** | 2026-09-08, Stage 49.1.7 (outside-in verification), confirmed live against production |

`internal/server/routes.go` registered the static file tree at the bare
`http.Handle("/", fs)` pattern, which in Go's `net/http.ServeMux` syntax
matches every HTTP method. `http.FileServer`/`http.ServeFile` only
special-case `HEAD` and otherwise ignore `r.Method` entirely, so `TRACE`,
`PUT`, `DELETE`, `PATCH` and `POST` against any real static asset all
returned `200` with the file body — confirmed against production itself
(`TRACE /app.js`, `PUT /styles.css`), not just reasoned about. Every other
route in the codebase either carries an explicit method in its `ServeMux`
pattern or runs behind `apiMiddleware`, both of which already refuse a wrong
method; this was the one surface with neither.

- **Treatment:** `internal/server/static_fileserver.go` gained
  `onlyReadMethods`, wrapping the file server so anything but GET/HEAD gets a
  `405` with `Allow: GET, HEAD` before the file server runs. Done and tested
  in this tree; **not yet deployed to production**, so the exposure above is
  still live until the next deploy.
- **Owner:** whoever runs the next production deploy.
- **Evidence:** `internal/server/static_fileserver_test.go`
  (`TestOnlyReadMethodsRejectsEverythingButGetAndHead`);
  `docs/security/outside_in_verification_2026-09-08.md`.
- **Review:** close this row once the fix is deployed and `cmd/edgecheck`
  re-run against production shows the `TRACE /` check passing.

### R-09 — No artifact signing, provenance or reproducible build

| | |
|---|---|
| **Severity** | High, narrowed as of 2026-09-06 (see below) |
| **Exposure** | D1, D2 |
| **Detectability** | Partial since 2026-09-06 — a `release-artifact` CI job now produces a checksum/SBOM/provenance manifest for any tagged build, and `supplychain.VerifyArtifact` detects a tampered candidate binary against it. Still **None** for whatever is already running in production, which predates this manifest, and for the real deploy path, which does not call it yet. |

Nothing at deploy time verifies that `/opt/erp/erp-server` was built from a reviewed
commit. A compromised build host, contributor account or CI job could publish a
trusted-looking binary. Stage 49.9 built the mechanism a signature and a
deploy-time check would use — a hermetic CI build (`go mod verify`,
`-trimpath`, `CGO_ENABLED=0`, pinned toolchain and actions), a minimal SBOM +
checksums + provenance manifest (`cmd/releasemanifest`, package
`internal/supplychain`), and a verify mode that refuses a mismatched binary —
but two things remain genuinely open, not merely undocumented: (1) no signing
key exists, so nothing cryptographically ties the manifest to this specific
pipeline; provisioning one is an infrastructure decision this session cannot
make. (2) `deploy/deploy.ps1` does not consume the CI artifact at all — it
still builds locally on the operator's machine and ships over SSH, so the
manifest mechanism exists in CI but is not yet wired into the path that
actually reaches production. See `docs/security/secure-development-lifecycle.md`
§49.9.7 for the full accounting. Mitigated in practice — but not entirely by
design — by an unusually small dependency surface: two Go modules, both
indirect, both pinned, and now every GitHub Action pinned to a commit SHA too
(49.9.4/49.9.8).

- **Treatment:** 49.9.6/49.9.7 mechanism built; signing-key provisioning and
  wiring `deploy.ps1` to fetch/verify the CI artifact are the remaining work.
- **Owner:** whoever owns the release pipeline, for the signing key and the
  `deploy.ps1` integration.
- **Evidence:** `docs/security/dependency-ledger.json`;
  `internal/supplychain/manifest.go`; `cmd/releasemanifest`;
  `.github/workflows/ci.yml`'s `release-artifact` job; `deploy/deploy.ps1`
  (still the unmodified local-build path).
- **Review:** at full 49.9 closure (signing key + deploy-side verification).

### R-13 — No enforced code review or branch protection on `main`

| | |
|---|---|
| **Severity** | High |
| **Exposure** | D1, D2, D3 (affects every commit that reaches any deployment) |
| **Detectability** | None — GitHub does not log "a direct push bypassed review" as an event distinct from an ordinary push, when no branch protection rule exists to bypass. |
| **Found** | 2026-09-06, Stage 49.9.2 |

`main` has no branch protection rule today: no required pull request, no
required status check, no required review, and (per `git log`) exactly one
contributor identity has ever pushed to this repository. `.github/CODEOWNERS`
now names an owner for auth/tenant/finance/inventory/migrations/deploy/security
paths, but CODEOWNERS enforces nothing by itself — it only gates a merge once
"Require review from Code Owners" is turned on for `main`, which is a
repository setting an org owner applies, not a file this repository can ship
its way into being enforced.

- **Treatment:** an org owner enables, on `main`: required pull request +
  required Code Owner review, required status checks (at minimum
  `build-and-test` from `.github/workflows/ci.yml`), and (optionally) "include
  administrators." See `docs/security/secure-development-lifecycle.md` §49.9.2
  for the exact settings.
- **Owner:** org owner (github.com/Vishalnayak226).
- **Evidence:** `.github/CODEOWNERS`; absence of any branch-protection API
  response to check against from this tree (no GitHub API access from a build
  session).
- **Review:** at 49.9.2 closure, when branch protection is actually enabled.

### R-12 — Admin-driven MFA reset has no dual control for a privileged target

| | |
|---|---|
| **Severity** | Medium (likelihood: low — requires a Super Admin account already compromised or malicious; impact: high — clears a second factor with no second signer) |
| **Exposure** | D1, D2 |
| **Detectability** | Good — `USER_MANAGEMENT`/`MFA_RESET_BY_ADMIN` audit log entry, but only after the fact. |
| **Found** | 2026-09-09, Stage 49.2.4, while closing the equivalent gap for password reset |

`handleAdminResetUserMFA` (`internal/server/handlers_mfa_recovery.go`) lets any
single Super Admin clear MFA enrollment for *any* other account, including another
Super Admin's, with no second approver. 49.2.4 built exactly this "auditable
dual-control helpdesk recovery for privileged users" control for the equivalent
admin-driven password reset (`engines.RequestAdminPasswordReset`,
`PasswordResetRequest` doctype) but deliberately did not retrofit it onto the
pre-existing MFA-reset endpoint in the same session — `TestAdminResetUserMFA`
(`internal/server/mfa_recovery_test.go`) seeds both the acting admin and the target
with role `HR/Admin` by construction, so applying dual control here is a real
behavior change to already-tested, working functionality, not a drop-in addition,
and deserved its own reviewed session rather than riding along.

- **Treatment:** give `handleAdminResetUserMFA` the same privileged-target gate
  `RequestAdminPasswordReset` uses (`engines.IsSuperAdmin(targetRole)` routes through
  `SubmitForApproval`/`DecideApproval` instead of executing immediately, following
  the `PasswordResetRequest` doctype's exact shape), and update `TestAdminResetUserMFA`
  to reflect a privileged target needing a second, different Super Admin's approval.
  The MFA-clearing SQL currently inline in `handleAdminResetUserMFA` should be
  extracted into a small exported `engines` function first, so both the immediate
  and the dual-control path call one choke point rather than duplicating it.
- **Owner:** whoever next picks up a 49.2 sub-item.
- **Evidence:** `internal/server/handlers_mfa_recovery.go` (`handleAdminResetUserMFA`);
  `engines/admin_password_reset.go` (the sibling that already has the gate).
- **Review:** at the next 49.2 session, or 2026-12-09.

---

## Risks carried in from the 2026-09-01 deep persona audit

49.0's acceptance requires Stage 47's findings to live in *this* register rather than
a parallel backlog. They are not restated here — the audit
(`docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md`) is the evidence and
`docs/micro_checklist.md` Stage 47 is the treatment plan. This table is the index,
with the misuse case each one corresponds to in threat_model.md §5.

| ID | Severity | Risk | Misuse case | Treatment | Status |
|---|---|---|---|---|---|
| A-01 | Critical | Cashier can read HR, system and finance data | M-01 | 47.1 | In progress |
| A-02 | Critical | POS trusts the browser for sale and cost price | M-02 | 47.2 | In progress |
| A-03 | Critical | Failed checkout can post stock and deduct again on retry | M-03 | 47.3 | In progress |
| A-04 | Critical | Legacy POS return is replayable | M-04 | 47.4 | In progress |
| A-05 | Critical (3PL) | Stock ownership not enforced during allocation/picking | M-05 | 47.5 | In progress |
| A-06 | Critical (RF) | WMS mobile workflows not operable on a phone | — | 47.6 | Open |
| A-07 | Critical (regulated) | Audit log neither fully protected nor scale-ready | M-08 | 47.7 | Open |
| A-08 | Critical (internet-facing) | XSS blast radius includes 24-hour bearer tokens | M-06 | 47.8 + 49.4.3 | Open |
| A-09 | High | Report and log routes lack role-capability checks | M-01 | 47.1 | In progress |
| A-10 | High | Rate-limiter histories grow per key with no global pruning | M-13 | 47.11 / 49.18.2 | Open |
| A-11 | High | Per-tenant concurrency cap 503s during a normal burst | — | 47.11 / 49.13.2 | Open |
| A-12 | High | Search applies SQL limit/offset before text filtering | — | 47.x | Open |

---

## Closed

### R-10 — A password reset link could be logged in production

| | |
|---|---|
| **Severity** | High (likelihood: medium — any transient SMTP failure; impact: high — full account takeover) |
| **Exposure** | D1, D2 |
| **Closed** | 2026-09-11, Stage 49.6.2/49.6.6/49.6.7 |

`engines/password_reset.go`'s `sendPasswordResetEmail` printed the full reset
link — a working, unexpired credential, not a reference to one — to the
application log in four branches. Three are dev-only convenience (no email on
file, no SMTP configured, external side effects off); the fourth,
`smtp.SendMail` failing, is not dev-only — an ordinary transient SMTP outage
in a real production deployment would have written a live account-takeover
token into the production log/journal, readable by anyone with log access,
for every reset attempted during the outage.

- **Fix:** `maskedResetLink` redacts the link whenever `ENV=production`
  (`TestMaskedResetLinkRedactsOnlyInProduction`); outside production it still
  prints, since a developer with no mailer configured has no other way to see
  the link they just generated.
- **Evidence:** `engines/password_reset.go`; `engines/password_reset_redaction_test.go`.

### R-11 — Login accepted a non-constant-time plaintext fallback if `password_hash` were ever unhashed

| | |
|---|---|
| **Severity** | High (authentication bypass shape, currently unreachable) |
| **Closed** | 2026-09-08, Stage 49.2.2 |

`handleLogin` (`internal/server/handlers_auth.go`) checked `bcrypt.CompareHashAndPassword`
and, on failure, also compared `u.PasswordHash != req.Password` directly — a leftover
"fallback check for local seed configs." Every account this codebase actually seeds
(`db/migration.sql`, `engines/saas.go` provisioning, `engines/tenant_lifecycle.go`
bootstrap credentials) stores a real bcrypt hash, so the fallback had no legitimate
caller and was not exploitable as shipped. It was still the wrong shape for a
security control: if `password_hash` were ever a plaintext value — a bad migration,
a manual SQL fix, test debris written directly with `SET password_hash = '...'`
(`engines/tenant_lifecycle_test.go` does exactly this to simulate an out-of-band
rotation) — that comparison would authenticate anyone who typed the literal stored
string, via a non-constant-time `!=` besides.

- **Fix:** the fallback comparison is removed; a login now succeeds only through
  `bcrypt.CompareHashAndPassword`, which fails closed on any non-bcrypt value and is
  constant-time by construction.
- **Evidence:** `internal/server/stage49_2_2_session_revocation_test.go` and the rest
  of the 49.2.2 test suite exercise the login/change-password paths with the fallback
  gone; no test in the suite relied on the removed behavior.

### R-04 — `public/` subdirectories were enumerable without authentication

| | |
|---|---|
| **Severity** | Medium (reconnaissance) |
| **Closed** | 2026-09-06, Stage 49.1.2 |

`http.FileServer(http.Dir("./public"))` generated an HTML index for any directory
without an `index.html`. `public/components` and `public/profiles` both qualify, so
an unauthenticated `GET /components/` returned a complete linked listing of every
front-end module — a free map of the application's internal structure, handed over
before login.

- **Fix:** `internal/server/static_fileserver.go` wraps the file system so a
  directory with no `index.html` reports "does not exist"; `http.FileServer` turns
  that into the same 404 an unknown filename gets, so the two cases are
  indistinguishable. Assets are still served by exact path, and a directory that has
  an `index.html` still serves it. Applied at the Go process, not at the reverse
  proxy, because the process is also reachable directly over the deployment's SSH
  tunnel and a control only one path enforces is not a control.
- **Evidence:** `TestStaticFileServerNeverListsADirectory`;
  `docs/security/attack_surface.json` `static_roots[0].directory_listing = false`.
