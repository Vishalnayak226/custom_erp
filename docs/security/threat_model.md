---
doc_id: DOC-13177E8D20
title: Security charter and threat model
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

# Security charter and threat model

**Stage 49.0** — established 2026-09-06. Covers items 49.0.1 through 49.0.5 and 49.0.7;
the risk register (49.0.6) is [risk_register.md](risk_register.md).

> **Status of this document.** This is the first engineering draft; domain approval is pending. It is written from
> the code, the deployment files and the 2026-09-01 deep persona audit, not from a
> template — every claim below points at something in this repository. It has been
> drafted and technically verified by the build session that wrote it; the domain
> approvals 49.0's acceptance requires (security, privacy, infrastructure, product,
> finance/control, data, SRE, legal) have **not** been obtained, and an AI or a
> developer cannot self-certify them. Until those approvals exist, treat this as an
> accurate engineering description of the system's threat surface and an unapproved
> risk position.

## 0. What this document is for

Stage 49 spends forty-plus sessions adding controls. This file exists so those
controls are chosen against something. It answers four questions in order, and
every later 49.x item is expected to cite an answer here rather than reason from
scratch:

1. What are we protecting, in which configurations? (§1, §2)
2. Where does trust change hands? (§3)
3. Who is trying to break it, and how? (§4, §5)
4. When does this document stop being true? (§6)

Nothing here claims the system is secure. Several sections describe controls that
do not exist yet; those are recorded as risks, not written as prose that implies
they are done.

---

## 1. Scope and security objectives (49.0.1)

### 1.1 Supported deployment configurations

| # | Configuration | Description | Assurance status |
|---|---|---|---|
| D1 | **Single-box hosted** | One `erp-server` Go binary under systemd as the unprivileged `erp` user (`deploy/erp.service`), PostgreSQL on the same host over loopback, Caddy terminating TLS and reverse-proxying to `127.0.0.1:8080` (`deploy/Caddyfile`), config in `/etc/erp/erp.env` (mode 640, `root:erp`), nightly encrypted backup by cron (`deploy/backup.sh`). Multi-tenant: one PostgreSQL schema per tenant, registry in `public.tenants`. | The only configuration with production evidence. Everything in this document is written against it unless stated otherwise. |
| D2 | **Single-box, per-tenant hostnames** | D1 plus `TENANT_BASE_DOMAIN`, which turns on host→tenant binding and Caddy `on_demand_tls` certificate issue per tenant slug (`internal/server/tenant_host.go`, `GET /internal/tls-ask`). | Supported; adds one unauthenticated surface (§3.2) and one new confusion class (§5, M-07). |
| D3 | **Local development** | `go run ./cmd/server`, `ENV` unset, seeded bootstrap accounts active, generated local signing key. | Explicitly **not** a deployment. The 49.1.3 baseline validator prints the same findings here that would block production, so the gap is visible before deploy, not after. |
| D4 | **Sandbox tenant** | A tenant schema flagged as sandbox (Stage 38.7); external side effects are suppressed per tenant. | Supported inside D1/D2. Its isolation from real side effects is a control, and 49.7.6 owns proving it. |

Not supported, and stated so rather than left ambiguous: containerised/Kubernetes
topologies, a managed/remote PostgreSQL (possible, but §5 M-09 and baseline finding
SB-015 apply and no deployment has been evidenced), horizontal scale-out of the Go
process (the in-process rate limiter and the JWT key file are per-process — see
A-10), and any configuration where the Go port is reachable from the internet
without the reverse proxy in front.

### 1.2 Regions, integrations, devices

- **Region/law:** India-first. GST, DPDP and CERT-In obligations are owned by 47.16
  and 48.6; this document does not restate them and must not be read as legal advice.
- **Outbound integrations:** Shopify, BigCommerce, Magento (channel connectors,
  credentials encrypted at rest with `CHANNEL_CREDENTIAL_KEY`), Unicommerce, Pine
  Labs, CleverTap, Delhivery and Shiprocket (courier), SMTP (password reset,
  notifications), an ops alert webhook, and QZ Tray for local label/receipt printing.
- **Inbound integrations:** Shopify webhooks (HMAC, fail-closed when
  `SHOPIFY_WEBHOOK_SECRET` is unset), a PIM import hook (`X-Hook-Token`), a PIM
  catalog share link (query token), and the scoped public API `/api/public/v1/*`.
- **Devices:** desktop browsers (primary), POS terminals, RF/handheld devices.
  RF/mobile WMS is **Preview**, not certified — finding A-06.

### 1.3 Security objectives

Each objective is written so it can fail a test, not so it can pass a questionnaire.

| Property | Objective | Where it is currently proven |
|---|---|---|
| **Tenant isolation** | No request authenticated for tenant A can read, infer, mutate, queue, export or restore data belonging to tenant B, through any route, report, export, attachment, job or error message. | Partially. Schema-per-tenant plus token-carried tenant; the full two-hostile-tenant harness is 49.3.4/49.10.3 and does not exist yet. |
| **Authorization** | Every route evaluates role capability **and** entity/location/owner scope **and** field classification server-side, before the response is produced. | Partially. Route capability classification exists for all 455 session routes (47.1.1); scope and field policy are 47.1.x work in progress. A-01 was the proof it did not hold before. |
| **Business integrity** | Every high-value command (sale, return, allocation, journal, payroll, credential change) produces zero or one complete, reconciled outcome, regardless of retry, tab count, network loss or process kill. | Not yet. A-02 through A-05 are open P0s. |
| **Authenticity of evidence** | The audit trail can be shown to be complete and untampered for the period a claim covers. | Not yet. ~90% of existing audit rows carry no checksum (A-07). |
| **Confidentiality at rest** | A stolen database snapshot or backup yields no reusable credential and only the minimum data its threat model allows. | Partially. Passwords are bcrypt; connector credentials are AES-256-GCM — but under a key that may live only in one host's user config dir (baseline finding SB-022). |
| **Availability** | Hostile or accidental load cannot starve login, command completion or incident controls, and cannot grow memory or disk without bound. | Partially. Request timeouts and per-tenant concurrency caps exist; A-10 (unbounded rate-limiter keys) and A-11 (generic 503 under normal burst) are open. |
| **Recoverability** | A verified restore of an uncompromised point is achievable within the approved RTO, and money/stock/audit reconcile afterwards. | Partially. Nightly encrypted backup and a restore drill script exist (`deploy/restore_drill.sh`); post-restore reconciliation is 49.13.5. |
| **Privacy** | Personal data is collected, retained, exported and deleted according to a recorded purpose, with statutory conflicts surfaced rather than silently resolved. | Not yet. 49.6.1/49.6.8. |

---

## 2. Crown jewels (49.0.2)

Ranked by what an attacker gains, not by how the code is organised. "Protection
today" describes what actually exists in this tree — not what is planned.

| Rank | Asset | Where it lives | If it falls | Protection today |
|---|---|---|---|---|
| 1 | **Session signing key** (`JWT_SECRET` / `JWT_SECRET_<n>`) | `/etc/erp/erp.env`, or a generated file under the OS user config dir | Forge a token for any user, role and tenant. Total compromise of every tenant at once. | Env var read at startup; rotation keyring (29.8); baseline findings SB-002/003/004; file mode 0600 checked by SB-020. |
| 2 | **Connector credential key** (`CHANNEL_CREDENTIAL_KEY`) | Same | Decrypt every stored Shopify/BigCommerce/Magento API token — i.e. control of the customer's storefronts, from an ERP backup. | AES-256-GCM; never returned by any HTTP handler (`getChannelCredential` is package-private); baseline findings SB-007/SB-022. |
| 3 | **Tenant registry and routing** (`public.tenants`, `schema_name`, `host_slug`) | PostgreSQL `public` schema | Point one tenant's hostname or token at another's schema — silent cross-tenant read/write. | Schema identifiers are validated before interpolation in new code (`validSchemaIdent`); the older engine fan-outs interpolate the registry value directly, which is why 49.3.3 exists. |
| 4 | **Identity and permissions** (`<tenant>.users`, `role_permissions`, `field_permissions`, `approval_rules`) | Each tenant schema | Grant yourself Super Admin; approve your own transactions; read payroll. | bcrypt password hashes; MFA for privileged roles; `IsSuperAdmin` as the single privilege predicate (40.3); auth-state cache re-checks active status within `AUTH_STATE_CACHE_SECONDS`. |
| 5 | **Payroll, HR and personal data** (`Payslip`, `EmployeeLoan`, `Grievance`, `ExpenseClaim`) | Each tenant schema | Direct privacy harm and DPDP exposure; the highest-consequence read in the product. | Route capability classification (47.1.1). A-01 proved a Cashier reached these before that existed. |
| 6 | **Money and stock authority** (prices, cost, margin, GL postings, period status, stock ownership) | Each tenant schema | Fraud that looks like ordinary trading: under-priced sales, replayed refunds, stock moved between owners. | Server-authoritative pricing and atomic checkout are in flight (47.2–47.5); A-02 to A-05 are the open proofs. |
| 7 | **Audit and security evidence** (`audit_logs`, `system_error_logs`) | Each tenant schema | Erase the record of everything above. Every other control's value depends on this one. | Checksum chain, verification endpoint. ~90% of rows predate the checksum (A-07). |
| 8 | **Backups** (`BACKUP_DIR`, nightly encrypted dump) | Host filesystem, `BACKUP_ENCRYPTION_KEY` | Offline copy of every tenant, with no rate limit, no audit and no MFA. | Encrypted at rest; freshness monitored hourly; restore drill script exists. |
| 9 | **Release artifact and migrations** (`erp-server` binary, `db/migration.sql`, 150 incremental migrations) | Build host → `/opt/erp` | Ship a back door to every deployment at once. | Migration checksums and a pending-migration warning at boot. Signing and provenance are 49.9.6/49.9.7 and do not exist. |
| 10 | **Bootstrap credentials** (`admin`, `cashier1`, `manager1`, `system` in `db/migration.sql`) | Repository, and every database created from it | Four known passwords, one of them (`system`) carrying the Super Admin role. | Baseline finding SB-021 refuses an `ENV=production` boot while any of them is active in any tenant schema. Removing them is 49.1.5. |

---

## 3. Trust boundaries and data flow (49.0.3)

### 3.1 The main path

```
 [browser / POS / RF device]                     untrusted: every value is attacker-controlled
        │  HTTPS
        ▼
 ┌──────────────────────┐   B1  TLS terminates. Caddy REPLACES X-Forwarded-For rather than
 │ Caddy reverse proxy  │       appending, which is what makes TRUST_PROXY=1 safe. On-demand
 │ :80/:443             │       TLS asks the app (GET /internal/tls-ask) before issuing a
 └──────────┬───────────┘       certificate for an unknown hostname.
            │  HTTP, loopback only (HOST=127.0.0.1)
            ▼
 ┌──────────────────────┐   B2  staticAssetCache → compressResponses → securityHeaders →
 │ Go HTTP server       │       tenantHostGate → ServeMux. Static assets and the SPA shell
 │ :8080                │       stop here and never reach apiMiddleware.
 └──────────┬───────────┘
            │
            ▼
 ┌──────────────────────┐   B3  AUTHENTICATION. Bearer token verified against the signing
 │ apiMiddleware        │       keyring; tenant, user, role and scope resolved and published
 │ (455 routes)         │       as Resolved-* headers. publicRoutes (12 entries) skip it.
 └──────────┬───────────┘
            │
            ▼
 ┌──────────────────────┐   B4  AUTHORIZATION. Route capability + access level (47.1.1).
 │ checkRouteCapability │       Field policy, entity/location/owner scope and workflow state
 │ + handler            │       are the parts 47.1.x and 49.3 are still closing.
 └──────────┬───────────┘
            │
            ▼
 ┌──────────────────────┐   B5  BUSINESS INVARIANTS. ValidateDocument is the shared choke
 │ engines/*            │       point every document mutation passes through. Money, stock,
 │ (transactions)       │       tax and approval decisions must be made here, never accepted
 └──────────┬───────────┘       from the client (A-02 is what happens when they are).
            │
            ▼
 ┌──────────────────────┐   B6  TENANT DATA. Schema-per-tenant; the schema name comes from
 │ PostgreSQL           │       public.tenants and is interpolated as a SQL identifier.
 │ tenant_<slug>.*      │       Application, migration and backup identities are not yet
 └──────────┬───────────┘       separated (49.7.1).
            │
            ├──────────────► B7  JOBS / OUTBOX. 28 background workers started at boot, each
            │                    fanning out across every tenant schema. They carry their own
            │                    tenant context, not a request's. 49.3.2 owns proving none of
            │                    them can act with an ambient identity.
            │
            └──────────────► B8  EGRESS. Channel connectors, couriers, SMTP, ops webhook, QZ
                                 print. Gated globally by ExternalSideEffectsEnabled() and
                                 per-tenant by the sandbox flag. Destinations are not yet
                                 allowlisted (49.8.7).
```

### 3.2 Every surface that answers before authentication

There are exactly five classes, and the 49.1.1 inventory
([attack_surface.json](attack_surface.json)) is generated from source so this list
cannot silently grow:

1. **`GET /internal/tls-ask`** — the only route registered with no middleware at all.
   Called by Caddy mid-TLS-handshake, when no request and therefore no token can
   exist. Answers only whether a hostname maps to a live tenant slug. Enforced by
   `TestNoUnauthenticatedRouteOutsideTheReviewedSet`.
2. **The 12 `publicRoutes` entries** — login, version, health, forgot/reset password,
   two courier tracking callbacks, three public help endpoints, the PIM import hook
   and the PIM catalog share link. Five of these are "public only up to a signature,
   header token or share token the handler itself verifies". Two of them
   (`/api/v1/integration/courier/*/tracking`) are **dead entries** — no route is
   registered at those paths (risk R-05).
3. **The scoped public API** — `/api/public/v1/*`, authenticated by an integration
   credential with a mandatory per-route scope. `publicAPIMiddleware` panics at
   registration for an unscoped route, so this surface cannot grow an unclassified
   member.
4. **Static assets** — `public/` served from disk. Since 49.1.2 a directory with no
   `index.html` returns 404 instead of a generated file listing.
5. **The SPA shell** — one `index.html` per product URL prefix and `/help/*`. Serves
   no tenant data; the client then authenticates.

### 3.3 Where secrets cross a boundary

| Secret | Enters at | Ever leaves? |
|---|---|---|
| Password | POST `/api/v1/login`, over TLS | Never. bcrypt hash only, never returned by any handler. |
| Session token | Response to login | To the browser, held in `localStorage` (A-08: no server-side revocation, 24h default lifetime). |
| Connector credential | Admin UI → `encryptChannelCredential` | Never. No handler returns a decrypted credential. |
| Webhook secret | `/etc/erp/erp.env` | Never. Used only for HMAC comparison. |
| Signing key | `/etc/erp/erp.env` or generated file | Never, and it must never appear in a log line — which is why the 49.1.3 validator's findings name variables and never values. |

---

## 4. Adversaries and abuse personas (49.0.4)

Ordered by how likely they are to matter for a real deployment of this product,
not by how exciting they are.

| ID | Persona | Starting position | What they want | Strongest current boundary |
|---|---|---|---|---|
| P1 | **Malicious or pressured cashier** | A valid Cashier session on a shared shop floor terminal | Under-price a sale for a friend, replay a refund, read payroll, see cost/margin | Route capability check (47.1.1). Business-invariant enforcement is the gap (A-02/A-04). |
| P2 | **Credential stuffer** | A password list, no access | Any working login, preferably privileged | Per-account and per-IP throttling; MFA on privileged roles. Enumeration-safe responses are 49.2.5. |
| P3 | **Compromised browser (XSS)** | Script execution in a logged-in user's page | The bearer token, then everything that role can reach | Weak today: `localStorage` tokens, CSP permits inline (A-08). 47.8/49.4.3 own this. |
| P4 | **Hostile tenant admin** | Full Super Admin of their own tenant | Reach another tenant's data through a shared route, job, report or export | Schema-per-tenant and token-carried tenant. Untested against a deliberate adversary until 49.10.3. |
| P5 | **Compromised connector or provider** | Control of an integration endpoint or its credentials | Replay webhooks, inject payloads, use the ERP as an outbound proxy | Shopify HMAC (fail-closed); per-tenant sandbox suppression. SSRF/destination allowlisting is open (49.8.4/49.8.7). |
| P6 | **Support/operator with host access** | SSH to the droplet, `erp` user or root | Anything, invisibly | Weak. No governed break-glass, no support-access evidence trail, no separation between the runtime, migration and backup identities. 49.14.4/49.14.5/49.7.1. |
| P7 | **Developer or CI account compromise** | Push access, or control of a build | Ship a back door to every deployment | Weak. No artifact signing, no provenance, no reproducible build (49.9.6/49.9.7). |
| P8 | **DBA / cloud administrator** | Direct PostgreSQL or hypervisor access | Read or alter data beneath the application, including the audit trail | Weak by design today: the audit chain is the only evidence, and 90% of it is unchecksummed (A-07). |
| P9 | **Ransomware operator** | Host compromise, likely via P6 or P7 | Encrypt production and every reachable backup | Backups are encrypted and monitored for freshness; whether they are reachable from the compromised host is exactly what 49.13.6 must drill. |
| P10 | **Accidental operator** | Legitimate access, wrong command | Data loss with no malice | Migration checksums; graceful shutdown; `ON CONFLICT DO UPDATE` in `db/migration.sql` re-seeds bootstrap passwords if it is re-run — a real footgun (R-01). |
| P11 | **Anonymous internet scanner** | Nothing | Any unauthenticated surface | Strong. Five surface classes, enumerated and tested (§3.2). |

**Collusion cases that must not be forgotten:** P1 + P6 (a cashier who knows the
support engineer), P7 + P9 (a poisoned release that carries the ransomware), and
P4 + P5 (a tenant admin who controls the connector their own tenant uses).

---

## 5. Misuse cases (49.0.5)

Every entry names an attacker goal, the path, and what would have to be true to stop
it. Entries M-01 to M-08 are **not hypothetical** — they are the 2026-09-01 deep
persona audit's proven findings, restated in this form so the same register holds
both proven and modelled abuse.

| ID | Persona | Goal | Path | Preventive control | Detective control | Status |
|---|---|---|---|---|---|---|
| M-01 | P1 | Read payroll and finance data | Call the HTTP route directly; the sidebar never hid anything server-side | Route capability + scope + field policy | Sensitive-read security event (49.11.1) | **A-01**, closing under 47.1 |
| M-02 | P1 | Sell at an arbitrary price | POST checkout with a chosen `sale_price`/`cost_price`; `discount_pct = 0` dodges approval | Server-resolved price/tax/cost | Manual-price and threshold-split detection (49.5.4) | **A-02**, closing under 47.2 |
| M-03 | P1 | Deduct stock twice | Fail checkout after the inventory commit, then retry | One transaction and one idempotency key per command | Stock↔ledger reconciliation exception | **A-03**, closing under 47.3 |
| M-04 | P1 | Refund the same line repeatedly | Legacy return path: no per-return idempotency key, evidence insert failure ignored | Authoritative return aggregate, locked cumulative quantity | Duplicate-refund detection | **A-04**, closing under 47.4 |
| M-05 | P4 | Consume another owner's stock | Allocation and picking do not filter by owner | Owner as a mandatory inventory dimension | Owner-level stock↔GL reconciliation | **A-05**, closing under 47.5 |
| M-06 | P3 | Steal a session | XSS → read `localStorage` → 24 hours of that role's access | Nonce/hash CSP, no inline handlers, revocable short-lived sessions | Impossible-scope / unusual-access detection | **A-08**, 47.8 + 49.4.3 |
| M-07 | P4 | Use tenant A's session on tenant B's hostname | Present a valid token at another tenant's host | `tenantHostGate` rejects the mismatch (401) when `TENANT_BASE_DOMAIN` is set | Tenant-routing-failure event | Controlled in D2; **untested against a deliberate adversary** |
| M-08 | P8 | Erase the record | Direct `UPDATE`/`DELETE` on `audit_logs`, or exploit the unserialised checksum chain | Serialised chain or signed events + checkpoints | Scheduled chain verification with alerting | **A-07**, 47.7 |
| M-09 | P5 | Use the ERP as a network probe | Point an outbound webhook at `169.254.169.254` or a private address | Destination approval, DNS/IP re-resolution, private-range denial | Egress denial event | Open — 49.8.4 |
| M-10 | P6 | Act as a customer's user, invisibly | Host access, or an unlogged impersonation path | Tenant-consented, ticket-bound, time-limited delegated access | Prominent customer indicator + full command evidence | Open — 49.14.4. No impersonation path exists in code today (verified by the 49.1.4 scan), which is the good version of this. |
| M-11 | P7 | Ship a back door | Compromise a dependency, a contributor account or one CI job | Pinned dependencies, signed artifacts, deployment verifies provenance | Release manifest and hash comparison | Open — 49.9. Mitigated in practice by an unusually small dependency surface: two Go modules, both indirect. |
| M-12 | P10 | Reset every bootstrap password by accident | Re-run `db/migration.sql`; its users INSERT ends in `ON CONFLICT DO UPDATE SET password_hash = EXCLUDED.password_hash` | A bootstrap seed that cannot overwrite a rotated credential | SB-021 at the next boot | Open — R-01 |
| M-13 | P2 | Lock a known victim out | Cheap repeated failed logins against one username | Lockout that costs the attacker more than the victim | Spray/stuffing detection | Open — 49.2.5 explicitly requires this not be possible |
| M-14 | P11 | Map the application before logging in | Fetch a directory index of `public/` | Directory listing suppressed (49.1.2) | Surface drift check (49.1.6) | **Closed 2026-09-06** |

---

## 6. Review triggers (49.0.7)

This document is re-reviewed — not merely re-read — when any of the following
happens. The 49.1.6 surface drift check fires automatically on several of them,
which is the point: the trigger should not depend on someone remembering.

**Automatic (a failing test or a blocked deploy):**

- A new route, or a route whose authentication class changes
  (`TestAttackSurfaceManifestIsCurrent`).
- A route registered with no middleware (`TestNoUnauthenticatedRouteOutsideTheReviewedSet`).
- A new entry on the unauthenticated `publicRoutes` allowlist that no route serves
  (`TestPublicRouteAllowlistHasNoNewDeadEntries`).
- A new bypass-pattern hit — committed credential, security-disabling flag,
  privileged shortcut, back-door vocabulary (`TestNoUnreviewedBypassPattern`).
- A new security-critical environment variable with no baseline check
  (`TestEverySecurityCriticalEnvVarIsChecked`).
- A new bootstrap credential in `db/migration.sql`
  (`TestSeededCredentialHashesCoverMigrationSQL`).

**Human (no test can detect these):**

- Any security incident, or a near miss.
- A new sensitive field, file flow, connector, outbound destination or dependency.
- A new supported deployment configuration, or a change to an existing one
  (a remote database, a second application instance, a CDN or WAF in front).
- A material architecture change to authentication, tenancy, jobs or the audit trail.
- A legal, regulatory or contractual change affecting a supported market.
- **Quarterly, even when nothing changed.** A threat model that is only revisited
  when code changes goes stale exactly when the attacker's world changes and ours
  does not.

---

## 7. What this document does not cover

Named explicitly, so absence is not mistaken for coverage:

- **Physical security** of the host, the store, or the POS hardware.
- **The customer's own tenant configuration** — a tenant that grants Super Admin to
  every user is outside any control this product can enforce. Shared-responsibility
  boundaries are 49.16.3.
- **Legal conclusions.** Breach-notification obligations, DPDP applicability and
  contractual claims are 47.16/48.6/49.16 and require qualified counsel.
- **The external penetration test.** 20.5/26.11.1 own it, and 49.10.7 says it runs
  only after internal stop-ship closure — a pentest against a system with four open
  P0s buys a report describing the four P0s.
