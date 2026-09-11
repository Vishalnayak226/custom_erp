# Outside-in verification — 2026-09-08 (Stage 49.1.7)

Run from the developer's own machine over its ordinary internet connection
against `https://app.wholeops.in` and the box's public IP
(`139.59.17.16`) — a genuine outside-in vantage point, not the SSH tunnel and
not `127.0.0.1` (docs/ai_handover.md's 2026-08-14 entry names exactly why
that distinction matters: checking `127.0.0.1` proves nothing about the
firewall). Tool: `cmd/edgecheck` (new this session), which stays in the repo
so this check is repeatable on every release rather than a one-off manual
pass — the point 49.1.6/49.17 (baseline drift, release gates) exist for.

```
go run ./cmd/edgecheck -base https://app.wholeops.in -direct-host 139.59.17.16
```

## Result: 27 PASS, 1 SKIP, 1 FAIL

Full machine-readable output: see the command above (not committed as a
static artifact — the report is a point-in-time HTTP/TCP probe result, and
committing one would go stale the moment the deployment changes; re-run it
to get current evidence).

| Check | Result | Detail |
|---|---|---|
| HTTP → HTTPS redirect | PASS | `308` to `https://` |
| `/api/v1/health`, `/api/v1/version` | PASS | `200`, as declared |
| `/internal/tls-ask`, `/internal/`, `/internal/anything-at-all` | PASS | `404` — Caddy's `@internal path /internal/*` block holds |
| `/api/v1/debug/panic` | PASS | `404` — `ENV=production` still gates it (24.16) |
| `TRACE /` | **FAIL** | `200` — see finding below |
| `PUT /api/v1/login`, `DELETE /api/v1/health`, `GET /api/v1/login` | PASS | `404` — Go's ServeMux refuses a method-mismatched request on an explicitly-method-scoped pattern without falling through to the static catch-all |
| 5 encoded/traversal path variants against `/internal/tls-ask` and `/etc/passwd` | PASS | `404` on every variant |
| Security headers (HSTS, CSP, X-Content-Type-Options, Referrer-Policy) | PASS | all present |
| `Server`/`X-Powered-By` | PASS | absent |
| Unknown-tenant hostname | SKIP | `TENANT_BASE_DOMAIN` is not set in production (Stage 44 is built but not deployed — see docs/ai_handover.md's 2026-08-14 entry); re-run with `-tenant-base-domain wholeops.in` once it is |
| Direct origin, port 8080 | PASS | connection timed out — the app is genuinely reachable only through Caddy, not directly from the internet |
| Ports 5432 (Postgres), 6379, 9200, 3000, 9090 | PASS | all timed out — no extraneous public service |
| Ports 22, 80, 443 | PASS | all open, matching the DigitalOcean firewall's declared 3 inbound rules |

## Finding: the static file server answers every HTTP method, not just GET/HEAD

`internal/server/routes.go` registered the whole static asset tree at the
bare `http.Handle("/", fs)` pattern. In Go's `net/http.ServeMux` syntax a
pattern with no method verb matches **every** method, and `http.FileServer`
(and the `http.ServeFile` calls used for the SPA shell) only special-case
`HEAD` internally — they never otherwise look at `r.Method`. Confirmed live
against production before the fix, not just reasoned about:

```
TRACE /app.js  -> 200 (full file body)
PUT   /styles.css -> 200
TRACE /   -> 200 (SPA shell HTML)
```

Every other route in this codebase either declares an explicit method in its
`ServeMux` pattern or runs behind `apiMiddleware`, both of which already
refuse a wrong method — this was the one gap, because the static file server
has neither. It is not a request-smuggling or XST (cross-site tracing)
finding — the response is the ordinary file body, not a header/cookie echo,
and no browser API can send a real `TRACE` — but it is a genuine violation of
49.1.7's own acceptance line ("alternate methods ... fail safely") and of
49.4.4/49.18.5's "allowlisted method" requirement, and it is exactly the kind
of low-cost hygiene gap an external scanner or a PCI-DSS-style ASV scan
flags on sight.

**Fixed in this tree**, not yet deployed: `internal/server/static_fileserver.go`
gained `onlyReadMethods`, a small wrapper that refuses anything but GET/HEAD
with a `405` and an `Allow: GET, HEAD` header before the file server ever
runs; `routes.go` now registers `http.Handle("/", onlyReadMethods(fs))`.
Verified on a local scratch instance (port 8098): `TRACE`/`PUT`/`DELETE`/
`PATCH`/`POST` against `/app.js` and `/` all now `405`; `GET`/`HEAD` still
`200`. Regression test: `TestOnlyReadMethodsRejectsEverythingButGetAndHead`
(`internal/server/static_fileserver_test.go`). Recorded as **R-10** in
`risk_register.md` — open until deployed, per this repo's convention of not
marking a production-facing finding closed on a code change alone that
hasn't shipped.

## What this closes and what stays open

49.1.7 — outside-in verification from an untrusted network — is built and
run: the check found a real, previously-unknown gap, which is the point of
having it, and the tool is reusable for every future release (49.17.2's
release-candidate gate can call it once a domain and direct-host are
parameterized in CI, which this session did not wire up — that is a 49.17
concern, not a 49.1 one). 49.1's own acceptance line
("a generated surface diff accompanies every release ... unsupported
surfaces are technically unavailable") now also covers the method axis, not
only the route axis.

Left genuinely open: the unknown-tenant-host check needs `TENANT_BASE_DOMAIN`
live in production to mean anything (currently skipped, not failed); this
tool should be re-run against production **after** the R-10 fix is deployed,
to turn today's FAIL into a recorded PASS rather than an assumption.
