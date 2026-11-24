---
doc_id: DOC-ENG-SETUP
title: Developer setup and verification
type: procedure
status: draft
owner: engineering-owner
approvers: [engineering-owner]
audience: [developers]
applies_to: isolated development checkout
authority: canonical
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Developer setup and verification

Use an isolated checkout and development database. Read the current
[current handover state](../ai_handover.md#current-repository-state) for shared-tree state;
this procedure contains stable steps without workstation credentials.

1. Install the Go version required by `go.mod`, PostgreSQL 16-compatible tooling, Git and
   PowerShell 7 for the repository's operational wrappers. Go dependencies are in `go.mod`/
   `go.sum`. The browser application uses native assets; its optional npm bundle is not
   required for normal development.
2. Obtain approved development connection/configuration through the local secret/config
   mechanism. Set `DATABASE_URL` for the intended database; do not rely on an implicit
   connection when operating data or evidence tools. Keep real external side effects off.
3. Follow the existing migration runner or CI ordering for the full additive migration set.
   Applying only the base schema does not produce a current database. Inspect the intended
   environment before running any operator command.
4. Build and verify the change proportionally. Build the application from `./cmd/server`;
   database tests must use an isolated fixture and the serialized package order below.
5. Start the development application through the existing manager or built server with
   approved environment values. Verify the displayed environment and simulated delivery
   posture before trying any workflow.

```powershell
go build ./...
go vet ./...
go test ./... -p 1
pwsh docs/update-docs.ps1 -Group Content -Check
go run ./cmd/doclint -strict
```

`-race` requires the supported C toolchain; CI supplies it on Linux. Documentation changes
have [focused safety checks](../test-docs-safety.ps1) that run in a temporary copy and do
not require business-data mutations. The brain also requires the ignored local graph:
refresh it explicitly with `graphify update .`; its `-Check` never refreshes it.

Record exact commands, results and unresolved failures. Do not interpret a documentation
check or a test-file link as proof that application UAT, release deployment or a provider
integration passed. The [project constitution](../../CONTRIBUTING.md) explains review and evidence.

## Notes for this tree

These notes moved here from the [handover](../ai_handover.md) on 2026-11-24 (item 264.2), so
the handover fits its 150-line budget.

### Build and verification

- Build the server with `go build ./cmd/server` (or `go build ./...` for package coverage).
- The browser uses `public/index.html`, classic `public/app.js`, and native modules
  `public/view-*.js`. Run `node --check public/app.js`; check modules with
  `Get-Content -Raw public/view-X.js | node --input-type=module --check`.
- `TEST_DATABASE_URL` overrides the test database connection. Use an isolated disposable
  PostgreSQL database for database-backed suites; do not point tests at a shared tenant or assume
  a service/port is live. Check listeners and database identity before starting or mutating one.
- A prior test caused unrecoverable deletion of development audit history; the production database
  was not affected. Read the current archive/test notes in the ledger before running audit archive
  tests or broad `engines` suites. Never run those suites concurrently against a shared database.
- For release budgets, `go run ./cmd/releasebudget -root . -binary <stripped-release-binary>`
  emits JSON with byte units; pass a read-only database URL for storage observations. The same
  thresholds now run as Go tests (`go test ./cmd/releasebudget/`), so a regression breaks the suite,
  not only CI. The two JS metrics are **different sets**: `initial_js_gzip_bytes` is the gated
  shell-only figure; `initial_js_plus_first_screen_gzip_bytes` adds the first lazy screen and is an
  unbudgeted observation. Do not merge them to make a number look better.
- Browser XSS checks: `node docs/qa/browser-xss-boundary.cjs <abs-evidence-dir>` and
  `node docs/qa/browser-xss-action-boundary.cjs <abs-evidence-dir>`.
- `GET /api/v1/ready` is readiness, distinct from `/api/v1/health` (liveness): 503 while migrations
  are pending or the process is draining, naming the pending migrations. Both public.
- Regenerate brain pages only through `pwsh docs/brain/update-brain.ps1` (or the docs wrapper).
  Review staged output first; never hand-edit generated pages or change access controls to force a
  write. The updater's temp-then-copy path exists for Windows-protected folders.

### Safety and release boundaries

- Keep tenant identity and schema resolution server-side. Do not add shared services, Redis or a
  broker without measured need and an approved architecture change.
- CSP: `script-src`'s inline exception **is retired** (2026-10-04) and enforced, after both phases
  measured zero violations across all 29 views. The report-only header now carries the next
  candidate, strict `style-src` (629 inline-style violations reported, not enforced) — retire it
  the same way: migrate, confirm zero report-only violations, then flip. HTML-sink classification
  (47.8.1) stays open; ~300 candidates are listed in
  `docs/security/browser-boundary-sink-classification.json`. See the
  [browser inventory](../security/browser-boundary-inventory.md).
- `OPS_ALERT_WEBHOOK_URL` and `OPS_ALERT_RESPONDER` are both unset. Local drills point the webhook
  at a loopback collector and are safe; do not send real external alerts or claim responder delivery
  until an authorized endpoint and named owner are configured. Setting those two variables is the
  whole remaining change — delivery, redaction, bounding, cooldown and responder naming are built.
- Vendor penetration testing, physical device acceptance, production changes and commercial/legal
  decisions remain separate gates. No production deploy or external message is implied by local
  code/tests.
- Preserve the existing no-commit instruction unless the user explicitly asks for a commit in the
  active task. Review `git status` and stage only individually reviewed files.
