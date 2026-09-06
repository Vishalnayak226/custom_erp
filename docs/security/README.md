# docs/security

Stage 49's security program lives here. Four files, three of them written by hand and
one generated.

| File | What it is | Maintained by |
|---|---|---|
| [threat_model.md](threat_model.md) | The security charter: supported configurations, objectives, crown jewels, trust boundaries, adversary personas, misuse cases, review triggers. Stage 49.0.1–49.0.5 and 49.0.7. | Hand-written. Re-reviewed on the triggers in its own §6. |
| [risk_register.md](risk_register.md) | Every open and closed security risk, with exposure, detectability, treatment, owner and review expiry. Stage 49.0.6. The 2026-09-01 deep persona audit's findings are indexed into it rather than tracked separately. | Hand-written. |
| [attack_surface.json](attack_surface.json) | The machine-readable inventory: every route and its authentication class, static roots, background jobs, CLI commands, migrations, environment flags, outbound call sites, dependencies. Stage 49.1.1, and the approved profile the 49.1.6 drift check compares against. | **Generated — never edit by hand.** |
| This file | Index and regeneration instructions. | Hand-written. |

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
  the gap visible.
