---
doc_id: DOC-5D2361897D
title: docs/security
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

# docs/security

Stage 49's security program lives here.

| File | What it is | Maintained by |
|---|---|---|
| [threat_model.md](threat_model.md) | The security charter: supported configurations, objectives, crown jewels, trust boundaries, adversary personas, misuse cases, review triggers. Stage 49.0.1–49.0.5 and 49.0.7. | Hand-written. Re-reviewed on the triggers in its own §6. |
| [risk_register.md](risk_register.md) | Every open and closed security risk, with exposure, detectability, treatment, owner and review expiry. Stage 49.0.6. The 2026-09-01 deep persona audit's findings are indexed into it rather than tracked separately. | Hand-written. |
| [attack_surface.json](attack_surface.json) | The machine-readable inventory: every route and its authentication class, static roots, background jobs, CLI commands, migrations, environment flags, outbound call sites, dependencies. Stage 49.1.1, and the approved profile the 49.1.6 drift check compares against. | **Generated — never edit by hand.** Regenerate with `go run ./cmd/surfacescan`. |
| [secure-development-lifecycle.md](secure-development-lifecycle.md) | Stage 49.9's SDLC/dependency/release-artifact supply-chain policy: what's built and tested vs. what's a documented GitHub-settings recommendation `[needs decision: org owner]`. | Hand-written. |
| [dependency-inventory.md](dependency-inventory.md) | How the dependency ledger works, what's enforced automatically, and the review checklist for a new dependency (49.9.4). | Hand-written. |
| [dependency-ledger.json](dependency-ledger.json) | Every direct/transitive Go module, CI action, CI tool and OS/runner image, each with owner/purpose/license/provenance/version/checksum. | **Hand-maintained, cross-checked by tests** — see `internal/supplychain`'s test file. |
| This file | Index and regeneration instructions. | Hand-written. |

## Release artifacts (49.9.6–49.9.9)

`cmd/releasemanifest` builds and verifies the SBOM/checksum/provenance record
for one build — see secure-development-lifecycle.md §49.9.6–49.9.7 for what
it produces and `.github/workflows/ci.yml`'s `release-artifact` job for
where it runs. Nothing here is committed to the repository (a manifest
describes one specific build, not the source tree), so there is no drift
check to run by hand the way there is for attack_surface.json.

```sh
# after building a release binary, from the repo root:
go run ./cmd/releasemanifest -commit "$(git rev-parse HEAD)" \
    -artifact erp-server=path/to/erp-server -out release_manifest.json

# before trusting a copy of that binary:
go run ./cmd/releasemanifest -verify -manifest release_manifest.json \
    -artifact erp-server=/path/to/candidate/erp-server
```

The no-bypass inventory (49.1.4) is deliberately *not* a document. It lives as a
reviewed allowlist in `internal/securityscan/bypass_test.go`, next to the scanner
that produces it, because a list of accepted exceptions that is not executed stops
being true within a release.

## Regenerating the inventory

```sh
go run ./cmd/surfacescan            # rewrite docs/security/attack_surface.json
go run ./cmd/surfacescan -check     # exit 1 if it is stale; write nothing
go run ./cmd/surfacescan -bypass    # print the 49.1.4 no-bypass scan
```

On this project's Windows dev machine, Controlled Folder Access blocks a freshly
built binary from writing under `Documents\` and reports it as "the system cannot
find the file specified" (see the note in `CLAUDE.md`). Write to `%TEMP%` and copy
in, the same way `docs/brain/update-brain.ps1` does:

```powershell
go run ./cmd/surfacescan -out $env:TEMP\attack_surface.json
Copy-Item $env:TEMP\attack_surface.json .\docs\security\attack_surface.json -Force
```

## What fails the build, and why

These run as ordinary `go test ./...` cases — no CI service, no extra tooling, and
no code in the production binary. The scanner package
(`internal/securityscan`) is imported only by its own tests and by `cmd/surfacescan`.

| Test | Fails when |
|---|---|
| `TestAttackSurfaceManifestIsCurrent` | A route, job, environment flag, outbound call site or dependency changed and the inventory was not regenerated. The failure prints what moved, not a byte diff. |
| `TestEveryRouteRegistrationIsRecognised` | `routes.go` grows a registration shape the scanner cannot parse — which would otherwise make a route silently invisible to the inventory. |
| `TestNoUnauthenticatedRouteOutsideTheReviewedSet` | A route is registered with no middleware and is not one of the reviewed exceptions. |
| `TestPublicRouteAllowlistHasNoNewDeadEntries` | An entry on `middleware.go`'s unauthenticated allowlist names a path no route serves. |
| `TestNoUnreviewedBypassPattern` | A committed credential, security-disabling flag, privileged shortcut or back-door phrase appears that is not in the reviewed inventory with a written reason. |
| `TestEverySecurityCriticalEnvVarIsChecked` | A security-critical environment variable exists with no check in the startup baseline validator. |
| `TestSeededCredentialHashesCoverMigrationSQL` | `db/migration.sql` seeds a password hash the startup validator does not know to refuse. |
| `TestStaticFileServerNeverListsADirectory` | The static file server would generate a directory index again. |

## The startup validator

`engines/security_baseline.go` runs on every boot, from `Run()` in
`internal/server/routes.go`. It logs each finding as `[SECURITY] SB-nnn …` with the
exact operator fix, and with `ENV=production` it refuses to start on a blocking one.
Outside production the identical list is logged and nothing is blocked, so a
developer sees the same findings they will meet at deploy time.

Findings never contain the value they are complaining about — only the variable name
and what is wrong with it. `TestBaselineFindingsNeverEchoConfiguredValues` enforces
that, because these lines land in the journal and in whatever collects it.

## Tenant lifecycle (49.1.5)

Provisioning, suspension, offboarding and purge are operator commands, not routes:
`cmd/tenantctl` (engine: `engines/tenant_lifecycle.go`, storage:
`db/migrations_stage49_1_5_tenant_lifecycle.sql`). Platform-level authority over
tenants deliberately has no HTTP surface — see the file header for why — so the
inventory above gains one CLI command rather than a route.

The operator procedure, with every command and what it refuses, is
[`../guides/ADMIN_GUIDE.md` §C.6](../guides/ADMIN_GUIDE.md). What matters here:

- The one-time admin password issued at provisioning **expires** (72h by default,
  `TENANT_BOOTSTRAP_TTL_HOURS`) and login refuses it afterwards until an operator
  reissues one. Rotation is detected by comparing the account's stored bcrypt hash
  with the one issued — bcrypt salts every hash, so equality is proof it was never
  changed, and no flag has to be remembered anywhere.
- Suspension and deprovisioning cut off **sessions already in flight**, not just new
  logins, through the tenant gate in `engines.ResolveLiveUserState`.
- A purge refuses — and records the refusal — without a prior deprovisioning, under a
  legal hold, inside the retention window, or with no backup reference; and it proves
  the removal afterwards (`VerifyTenantResidue`: no registry row, schema, hostname,
  shared-table row or cached session).
- `public.tenant_lifecycle_events` is the append-only evidence trail. It is
  deliberately **not** foreign-keyed to `public.tenants`, because it has to outlive
  the tenant it describes.
- `tenantctl db-privilege` reports whether the app connects as a database superuser.
  Fixing that is deployment state and belongs to 49.7.4; reporting it is what keeps
  the gap visible. `deploy/postgres_harden.sql` (49.7.1/49.7.4) is that fix for the
  migration and backup identities; the runtime identity's fix is documented as open
  in `deploy/README.md` Part A2.5 and risk_register.md R-07 - two existing HTTP
  tenant/sandbox-provisioning routes need schema-creation rights through the same
  connection ordinary requests use.

## Data classification, keys and secrets (49.6)

**Classification registry (49.6.1).** `engines/data_classification.go` extends
`engines/sensitive_fields.go` (47.1.3) rather than duplicating it: one row per
sensitive-field CATEGORY (not per field), adding a tier
(public/internal/confidential/restricted) and, where it applies, a privacy tag
(personal/sensitive_financial/authentication/audit/legal) plus purpose,
source, consumers, masking, export, retention trigger, legal-hold eligibility
and deletion behavior. `TestSensitiveFieldCategoriesAreAllClassified` fails
the build if a new sensitive-field category is ever added without a matching
row here. Several `Deletion`/`RetentionTrigger` answers are marked
`[needs decision: ...]` — the exact statutory retention window is 47.16's call,
not something a build session invents.

**Key inventory (49.6.5).** Every at-rest encryption key in this deployment,
what it protects, and how it rotates:

| Key | Env var(s) | Protects | Rotation | Blast radius if lost/leaked |
|---|---|---|---|---|
| Session signing key | `JWT_SECRET` / `JWT_SECRET_<n>` | Bearer session tokens | Zero-downtime keyring (Stage 29.8) — add `_<n>`, wait one token TTL, delete the old one | Forge a session for any user/role/tenant |
| Connector credential key | `CHANNEL_CREDENTIAL_KEY` / `CHANNEL_CREDENTIAL_KEY_<n>` | Shopify/BigCommerce/Magento tokens (`channel_credentials.encrypted_payload`) | Zero-downtime keyring (Stage 49.6.5, `engines/secret_keyring.go`) — add `_<n>`, run `tenantctl reencrypt-channel-credentials`, delete the old one | Decrypt every stored connector credential (risk register R-03) |
| Backup encryption key | `BACKUP_ENCRYPTION_KEY` | Nightly `pg_dump` (`deploy/backup.sh`) | Manual — re-encrypt existing backups or accept old backups stay under the old key until they roll off retention. Not read by the Go binary, so it has no `SB-*` baseline check; `deploy/backup.sh` and `docs/operations/backup_restore.md` are the only enforcement today. | Decrypt every historical backup — every tier of data in the system at once |

Both application-managed keys (JWT, channel credential) now share one
rotation-capable AES-256-GCM keyring implementation
(`engines/secret_keyring.go`, generalized from the JWT-only Stage 29.8
pattern): `NAME_<n>` env vars, highest number signs new data, every
configured key (plus the legacy bare `NAME`) still decrypts what it wrote —
so a ciphertext written before rotation existed keeps working with no batch
migration required first. `ReencryptChannelCredentials` /
`tenantctl reencrypt-channel-credentials` is the operator step that actually
completes a rotation by re-sealing every stored row under the current key.

`[needs decision: dual-control key recovery]` — nothing here requires a
second person to approve a key's recovery/rotation/destruction; keys are
environment-level operator actions, not an in-app workflow, so "dual control"
today is whatever the deployment's own change-management process enforces
outside this codebase. A compromise drill is the same: the rotation mechanics
above are unit-tested (`engines/secret_keyring_test.go`,
`engines/channel_credentials_rotation_test.go`), but a live drill against a
real deployment is an operational exercise, not something this session can
certify.

**Secret lifecycle (49.6.6) and safe telemetry (49.6.7).**
`engines/telemetry_redaction.go`'s `RedactForTelemetry`/`MaskIdentifier` are a
redaction CHOKE POINT, not a security-event pipeline — no structured
security-event system exists yet (that is Stage 49.11); this is what such a
pipeline must call on day one so the redaction rule cannot drift per call
site. It masks any key shaped like a bearer/password/secret/MFA/session/
cookie/credential outright, plus any field `engines/sensitive_fields.go`
classifies for the given doctype, walking nested maps/slices. Found and fixed
while auditing for exactly this: `engines/password_reset.go`'s SMTP-failure
branch logged the full password reset link — a working, unexpired credential
— and could do so in production on an ordinary transient send failure, not
only in dev. `maskedResetLink` now redacts it whenever `ENV=production`.

**Privacy rights (49.6.8).** `engines/privacy_rights.go` +
`db/migrations_stage49_6_privacy_rights.sql` (`data_subject_requests` table)
implement the request lifecycle — open, maker-checker decide (decider must
differ from requester), execute, evidence — for one concrete data flow:
Customer access/export/erasure/anonymization. Erasure anonymizes rather than
hard-deletes (Sales/Invoice reference `customer_id` and must survive for
statutory financial retention) and refuses under legal hold
(`SetSubjectLegalHold`, an ad-hoc JSONB field on the subject document, the
same no-new-column convention `Item.cost_price` already uses) — the refusal
is recorded, not silent, mirroring `PurgeTenant`'s (49.1.5) shape exactly.
Any other subject doctype, or `correction`/`consent_withdraw`, is recorded
but explicitly NOT auto-executed yet: `[needs decision: which Customer
marketing/communication flag consent-withdrawal should clear — none exists
today]`. The full 49.6.8 acceptance bar (search/index, jobs, files, logs,
audit, backups) remains open beyond Customer.
