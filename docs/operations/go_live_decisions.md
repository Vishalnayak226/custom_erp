# Go-Live Decisions — Worksheet & Execution Guide

Every item below is a real checklist item (`docs/micro_checklist.md`) that is code-complete or scoped, but blocked purely on a decision or a real-world credential only you can provide. This doc has two jobs:

1. **Decision worksheet** — every option worth weighing, in one place, so you don't have to reconstruct context to decide.
2. **Execution guide** — once you've picked, the concrete steps to go get the credential/service and where it plugs into this codebase.

## How to use this

- Work section by section. Each has a **Your call:** line — fill it in (or just tell me in chat) and I'll do the wiring-in-code half of the "once decided" step (env vars, config, flipping the checklist item) while you do the portal/vendor half.
- Nothing here is urgent-blocking each other except where noted ("depends on"). Pick off whichever section is easiest for you first.
- Vendor portal UI changes over time — steps below are the current standard flow as of 2026; if a screen doesn't match, search "`<vendor> API sandbox setup`" and the shape will still be the same (create app → get client id/secret/token).
- Once you've made a call and done the portal-side step, tell me — I'll wire the credential in, run `scripts/verify_connector_live.ps1` (or the equivalent), flip the item to `[x]` in `micro_checklist.md`, and log it in `project_ledger.md`.

---

## Summary table

Status column current as of **2026-08-06**.

| # | Decision | Checklist IDs | Depends on | Type | Status |
|---|---|---|---|---|---|
| 1 | Escalation contacts | 20.1 | — | Internal, no portal | ⛔ Open — parked by user 2026-08-06 |
| 2 | Ops alert webhook | 20.2 / 26.2.2 | — | Portal (Slack/Teams) | ⛔ Open — parked by user 2026-08-06 |
| 3 | Connector sandbox credentials | 20.3 / 26.2.1 | — | Portal (Shopify/BigCommerce/Magento) | ⛔ Open — parked by user 2026-08-06 |
| 4 | Production hosting | 20.4 / 26.1.1 | — | Portal (VPS/cloud provider) | ✅ **Decided 2026-08-06** — droplet formalised, `ENV=production` set. Domain/TLS still open |
| 5 | Edge WAF / rate-limiting | 26.1.3 | #4 | Portal (Cloudflare/cloud WAF) | 🟡 **Partial** — proxy hardening done; WAF needs a domain |
| 6 | Tenant backup scope | 26.1.6 | — | Internal scope decision | ✅ Decided + built 2026-08-05 |
| 7 | External security/perf reviewer | 20.5 | — | Portal/vendor engagement | ⛔ Open — scope doc drafted (see #13) |
| 8 | GSP sandbox (e-invoice/e-way bill) | 20.30/20.31, 26.2.3/26.2.4, 26.6.9 | — | Portal (NIC or GSP) | ⛔ Open — parked by user 2026-08-06 |
| 9 | Payment-terminal sandbox | 26.2.5 | — | Portal (Pine Labs) | ⛔ Open — parked by user 2026-08-06 |
| 10 | Supplier portal auth model | 26.4.10 | — | Internal product decision | ✅ Decided + built 2026-08-05 |
| 11 | AI content-assist scope | 26.4.11 | — | Internal governance decision | ✅ **Decided + built 2026-08-06** — local/offline only |
| 12 | P2 bundles go/no-go | 26.5.11, 26.7.8, 26.8.7, 26.9.9, 26.10.6 | — | Internal go-ahead per bundle | ✅ 4 of 5 built 2026-07-27; 26.10.6 measured 2026-08-06 → not justified |
| 13 | External pen-test engagement | 26.11.1 | #4 (ideally) | Portal/vendor engagement | 🟡 **Scope doc drafted** — [`pentest_scope.md`](pentest_scope.md); needs a vendor |
| 14 | DR/restore drill in real prod | 26.11.3 | #4 | Internal, once #4 exists | 🟡 **Unblocked** by #4 — not yet run |
| 15 | Business UAT cycle | 26.11.5 | — | Internal, needs real users | 🟡 **Run sheet drafted** — [`uat_run_sheet.md`](uat_run_sheet.md); needs real users |
| 16 | Hypercare window | 26.11.6 | #15 | Internal decision | 🟡 **Plan drafted** — [`hypercare_plan.md`](hypercare_plan.md); needs your sign-off + #1/#2 |

---

## 1. Escalation contacts (20.1)

`docs/operations/incident_runbook.md` §2 has a real table with `_TBD_` placeholders — Primary on-call, Secondary/escalation, Business owner (name/contact/hours). No portal involved; this is just naming real people.

**Your call:** give me name + phone/email/Slack handle + hours for each of the 3 rows (or say "just me for all three" if that's genuinely the current staffing).

**Once decided:** I fill in `incident_runbook.md` §2 directly and flip 20.1 to `[x]`.

---

## 2. Ops alert webhook — `OPS_ALERT_WEBHOOK_URL` (20.2 / 26.2.2)

Three real triggers already wired (`incident_runbook.md` §3): panic recovery, failed backup, sustained error rate. They all fire through one env var — whichever chat tool you pick just needs to accept a generic incoming-webhook POST.

| Option | Pros | Cons |
|---|---|---|
| **Slack incoming webhook** (recommended) | Simplest setup, free, everyone already has Slack in most small teams | Needs a Slack workspace |
| Microsoft Teams incoming webhook | Same simplicity if your team lives in Teams | Classic webhook connectors are being phased out in favor of Workflows in some tenants — extra step |
| Discord webhook | Free, trivial | Unusual choice for ops alerting, no real advantage here |

**Your call:** Slack, Teams, or other?

**Once decided (Slack, recommended):**
1. Go to `api.slack.com/apps` → **Create New App** → **From scratch** → name it (e.g. "ERP Ops Alerts") → pick your workspace.
2. Left sidebar → **Incoming Webhooks** → toggle **Activate Incoming Webhooks** on.
3. Scroll down → **Add New Webhook to Workspace** → pick the channel alerts should land in → **Allow**.
4. Copy the generated webhook URL (`https://hooks.slack.com/services/...`).
5. Tell me the value (or set it yourself) — it goes into the environment as `OPS_ALERT_WEBHOOK_URL` for both the Go server process and `manage.ps1`. Restart the server for it to pick it up.
6. I'll trigger a test panic/error path to confirm a message actually lands in the channel, then flip 20.2/26.2.2 to `[x]`.

**Once decided (Teams alternative):** Teams channel → `···` on the channel → **Connectors** (or **Workflows** → "Post to a channel when a webhook request is received" on newer tenants) → configure → copy the URL → same env var.

---

## 3. Non-production connector credentials — Shopify/BigCommerce/Magento (20.3 / 26.2.1)

Code-complete; only real sandbox credentials are missing. Once you have them, run `scripts/verify_connector_live.ps1`.

**Your call:** which platform(s) do you actually need live-verified? (You don't need all three if only one is relevant to your actual go-to-market.)

**Once decided:**

**Shopify** (free, no store purchase needed):
1. Create a Shopify Partner account at `partners.shopify.com` (free).
2. **Stores** → **Add store** → **Development store** (free, sandbox-only, never charges).
3. Inside that dev store's admin → **Settings** → **Apps and sales channels** → **Develop apps** → **Create an app**.
4. Configure Admin API scopes (products, orders, inventory — whatever this connector reads/writes; check `engines/` connector file for the exact scopes it calls).
5. **Install app** → reveal the **Admin API access token** (shown once — copy immediately).

**BigCommerce** (free trial store):
1. Sign up for a free trial store at `bigcommerce.com`.
2. Store admin → **Settings** → **API** → **Store API accounts** → **Create API account** (v2/v3 token type).
3. Copy the Client ID / Client Secret / Access Token shown once.

**Magento (Adobe Commerce / Open Source)**:
1. Easiest sandbox path: a free Adobe Commerce trial, or a local Magento Open Source install if you already have one for testing.
2. Admin → **System** → **Extensions** → **Integrations** → **Add New Integration**.
3. Fill name, set the resource scopes needed → **Save** → **Activate** → approve the permissions prompt.
4. Copy Consumer Key/Secret + Access Token/Secret shown after activation.

Hand me whichever credentials you get and I'll drop them into the connector config and run the verify script.

---

## 4. Production hosting decision (20.4 / 26.1.1)

> **DECIDED 2026-08-06 — the droplet is production.** The provider question was
> already settled in practice: a DigitalOcean droplet has been serving the real
> build for some time, with `systemd` supervision, the Go binary bound to
> `127.0.0.1`, Caddy installed, `ufw` active, and nightly encrypted backups.
> Formalised on 2026-08-06:
>
> - **`ENV=production` is now set** in `/etc/erp/erp.env`. It had never been set.
>   That gate does three things, and all three were off: the seed-admin
>   credential hard-stop (`engines/auth.go`), the non-UTF8 database hard-stop
>   (`db/db.go`), and removal of the `/api/v1/debug/panic` route
>   (`internal/server/routes.go`). Both hard-stop preconditions were verified
>   *before* flipping it — the database is UTF8 and the seed admin password was
>   already rotated — because either would have refused to boot. `debug/panic`
>   now returns 404.
> - **Caddy was serving the stock Debian welcome page.** The repo's own Caddyfile
>   had never been installed, and the `reverse_proxy` line was commented out, so
>   Caddy was doing nothing while the app was reached solely through an SSH
>   tunnel. Replaced with `deploy/Caddyfile.holding`, which returns a blank 404.
> - **Still open: domain and TLS.** Deliberately *not* worked around. Pointing
>   Caddy at the app over plain HTTP would publish the whole ERP — passwords,
>   tokens, tenant data — in cleartext, which is strictly worse than the current
>   tunnel-only posture. `deploy/enable_tls.sh <domain> <email>` does the whole
>   switch in one command (validates DNS points at the box before touching
>   anything, so a premature run can't burn Let's Encrypt failure quota;
>   validates the generated config; backs up and auto-rolls-back on a failed
>   reload). Supply a domain and this closes.
>
> The rest of this section is kept as the reasoning behind the choice, and stays
> accurate for a future re-evaluation.

Today "dev/test/live" (`environments.json`) are three databases and three ports on one Windows dev machine, not real hosting. This app is one Go binary (`erp-server.exe`) + PostgreSQL — **no Docker** (standing policy — Stage 14: "Docker built on request, then reverted on request"), no Kubernetes in practice today, no bundler/build step on the frontend. The hosting choice should match that shape: lightweight infra, not a container platform, unless you deliberately want to revisit the no-Docker policy.

### 4a. Provider

| Option | Fit | Notes |
|---|---|---|
| **Small Linux VPS** (DigitalOcean Droplet, Hetzner Cloud, or AWS Lightsail) — **recommended** | Matches current single-binary-plus-Postgres shape exactly | You (or I) install Go runtime/binary + Postgres + a reverse proxy directly on the box, supervised by `systemd`. Cheapest, simplest, most transparent. Hetzner is cheapest; DigitalOcean has the most beginner-friendly docs; Lightsail is easiest if you already use AWS elsewhere. |
| Managed PaaS (Render, Railway, Fly.io) | Fast to stand up, handles TLS/scaling for you | Under the hood these containerize your app for you — you don't author a Dockerfile, but it's not a bare-metal deploy either. Worth flagging since Docker was explicitly reverted here before; only pick this if that policy was about not *maintaining container infra yourselves*, not containers-at-all. |
| Full AWS (EC2 + RDS, or ECS/Kubernetes) | Matches `docs/architecture/architecture_evaluation.md`'s aspirational diagram | That diagram is a future-scale sketch, not what's built — meaningfully more ops overhead (VPC, IAM, RDS backups, ECS task defs) than a pre-revenue single-tenant-scale ERP needs right now. Don't reach for this until real multi-region/HA load justifies it. |

**Given this is India-focused (GST/e-invoice/Pine Labs)**, also weight latency to India: DigitalOcean Bangalore region, AWS `ap-south-1` (Mumbai) on Lightsail, or Hetzner doesn't have an India region (higher latency from Europe/US, fine for admin use, worse for a customer-facing storefront).

**My recommendation:** a single DigitalOcean Droplet or AWS Lightsail instance in Bangalore/Mumbai, sized around 2 vCPU / 4GB to start (Postgres + Go binary both fit comfortably), scaled up only when actually needed.

### 4a.1 Docker deep dive — is it required, and is bare-VPS scaling real?

Raised and answered in full 2026-07-26; condensed here for the record.

**Docker pros (general):** environment parity dev↔prod, portable across any cloud (no VM/AMI lock-in), the entire autoscaling/rolling-deploy/rollback-by-tag ecosystem (k8s/ECS/Cloud Run) is built around it, dependency isolation for messy runtime stacks.

**Docker cons (general + specific to this app):** a new operational surface (Dockerfiles, image registry, patch cadence), small CPU/memory overhead, debugging one layer removed (exec-into-container vs. SSH+`ps`). **Specific to this app: Docker's headline benefit — dependency isolation — barely applies**, since Go already compiles to one static binary with no runtime dependency tree to isolate. What it would actually buy here is cross-cloud portability and orchestrator access, neither of which is a real pain point today. It was also already tried once (Stage 14: built on request, reverted on request) — the standing policy comes from that lived decision, not an untested guess; the ledger doesn't record *why* it was reverted, worth recalling if that reason still applies.

**Is Docker required on cloud? No — depends which product you pick:**

| Hosting category | Examples | Docker required? |
|---|---|---|
| Bare IaaS VM | DigitalOcean Droplet, Hetzner, EC2, Azure VM, GCP Compute Engine, Lightsail | No — a Linux box, run the binary under `systemd` |
| Buildpack PaaS | Render Web Service, Railway, Heroku | Not authored by you, but containerized invisibly under the hood |
| Container-native PaaS | Cloud Run, AWS App Runner, Azure Container Apps, Fly.io | Yes — a container image is the deployment unit |
| Managed orchestration | Kubernetes (EKS/GKE/AKS), ECS | Yes — mandatory, that's the point of the platform |

**If you did containerize:** cost is a Dockerfile (small for a static Go binary) + a registry (GHCR is free) + an extra CI build/push step; container-native pricing (per-vCPU-second) can cost more than a fixed VPS at steady load but less at low/spiky load (scale-to-zero). Benefit is autoscaling + zero-downtime rolling deploys + instant rollback-by-tag. Not worth it yet at this app's current scale.

**If you stay no-Docker, scaling is still real, just manual/scripted instead of autoscaled:**
1. **Vertical first**: resize the VPS (most providers do this in under a minute of downtime) — one well-tuned box goes a long way for an ERP's actual transaction volume.
2. **Horizontal, once outgrown**: run the same binary on a 2nd VPS (still plain `systemd`), add a load balancer (cloud LB or Caddy/Nginx/HAProxy round-robin). Auth is JWT (stateless, no sticky sessions needed) and all real state lives in one shared Postgres, so this works cleanly — **with one concrete exception already confirmed in code**: the per-tenant concurrency limiter and the global rate limiter (`internal/server/middleware.go`'s `tenantConcurrencyLimiter`/`globalLimiter`) are both plain in-process `sync.Mutex`+map state, not backed by Redis/Postgres. Run 2 instances and a "cap of 15 concurrent requests per tenant" silently becomes 30 (15 per instance) — this hits identically whether you scale via bare VMs or containers, so it needs a shared backing store before adding a 2nd instance of either kind.
3. **Postgres**: vertical (bigger box, tuned `shared_buffers`) first, then a managed Postgres add-on (DigitalOcean Managed DB, RDS) once real load justifies it — same don't-build-ahead-of-a-measured-bottleneck logic as the 26.10.6 decision.
4. **Managing N boxes without Docker**: **Ansible** — agentless/SSH-based, pushes the same binary+config to every box, no containers involved, and is a natural extension of the existing `promote.ps1`/`manage.ps1 -Env` pattern rather than a new tool philosophy.

**Conclusion:** stay no-Docker for the initial go-live (§4a's recommendation already assumes this); revisit only if you specifically want autoscaling-on-demand or land on a PaaS that mandates it.

**Final call (2026-07-27):** no Docker for now, plain VPS + `systemd` + Caddy. Reasoning given explicitly: a 20-year survival bet favors boring/durable tech over faster-churning orchestration tooling; Docker is cheap to bolt on later (a Dockerfile for a static Go binary is trivial) but costly to carry now for a benefit (autoscaling) not yet needed; and it's the least to learn for a non-expert operator. The desktop-disk-space concern that prompted the question doesn't actually apply in the cloud (Docker Engine runs natively on a Linux VPS, no WSL2-style VM-in-a-VM overhead) — noted for completeness, but wasn't the deciding factor either way.

**Rough growth path** (reversible, small step at a time — none of these require a rewrite):

| Stage | Trigger | What changes | Rough cost/mo |
|---|---|---|---|
| 0 (start here) | 0-1 client | 1 Droplet (Bangalore/Mumbai), 1-2 vCPU/2-4GB, Postgres + binary same box, `systemd` + Caddy + Cloudflare free tier | ~$12-24 |
| 1 | One box maxed on CPU/RAM | Resize the same droplet bigger — no architecture change | ~$40-80 |
| 2 | Postgres itself is the bottleneck | Split Postgres onto its own box, or a managed Postgres add-on | ~$100-150 |
| 3 | One app box maxed even after resizing | 2nd app VPS + load balancer; **must** fix the in-process concurrency/rate limiter (above) to a shared store first — required at this point regardless of Docker | ~$150-250 |
| 4 (maybe never) | Running 5+ app instances, manual deploy/scaling genuinely hurts | This is the point Docker/an orchestrator starts earning its keep — not before | reconsider with real usage data |

**"Until how many clients is containerization not required?"** No clean number exists — the real trigger is a *measured* bottleneck (CPU/RAM/DB saturation, degrading latency), watched via the Stage 26.1.2 System Status dashboard and the Stage 26.1.5 Tenant Usage dashboard (both already built) — not a client-count guess, same "don't build ahead of a measured bottleneck" principle as the 26.10.6 decision. As a rough planning anchor only: this app's usage pattern (an ERP — bursty within business hours, not sustained/viral load) plus the existing hard ceiling already in code (`db/db.go`'s shared `dbMaxOpenConns = 50` connection pool, one pool for every tenant) suggests a single well-resized box plausibly carries **on the order of a few hundred tenants** before a 2nd app instance is worth it — but treat that as a planning anchor to revisit against real dashboard numbers, not a hard line. Note the connection-pool cap is itself a more immediate ceiling than tenant count and may need raising (or fronting with PgBouncer) well before either the app tier or Docker becomes the bottleneck.

**Prepared-but-dormant (2026-07-27):** rather than build container support live whenever stage 4 is actually reached, the code is now kept ready in advance so switching is a same-day flip, not a project:
- `Dockerfile` (repo root) — multi-stage build, distroless runtime image, fully commented as dormant/not-the-deployment-path.
- `.dockerignore`, `docker-compose.yml` (local smoke-test only — `docker compose up --build` + apply the schema the same way CI does, then hit `localhost:8080`; not a deployment mechanism).
- `.github/workflows/ci.yml`'s new `docker-build-check` job builds the image on every push (never pushes/deploys it) — this is what keeps it from silently rotting until the day it's actually needed; a dormant Dockerfile nobody ever builds is worse than no Dockerfile, since it'd fail exactly when you finally need it.
- **This does reintroduce a Docker-related file into the repo**, which is worth flagging plainly since the standing policy traces back to Stage 14 building Docker support and then reverting it at explicit request. This time it's inert (never runs in CI/dev/prod, doesn't change how the app boots or is tested day to day) and was requested with that history in view — treat it as a deliberate, informed exception to the standing policy, not a silent walk-back of it.
- **Turning it on later**, when the dashboards show a real trigger: point `promote.ps1`/`manage.ps1 -Env` (or its then-current equivalent) at building/pushing this image to a registry (GHCR is the free, zero-new-vendor option) instead of copying a raw binary, and swap the target host's `systemd` unit for a `docker run`/compose invocation — the image itself needs no changes to make that switch.

### 4b. Domain

Any registrar works (Namecheap, GoDaddy, Cloudflare Registrar — Cloudflare sells at wholesale cost with no markup, worth a look if you're already leaning toward Cloudflare for WAF in §5).

### 4c. TLS

**Caddy** as the reverse proxy in front of the Go binary gets you automatic Let's Encrypt TLS with essentially zero config (a single `Caddyfile` reverse-proxying to `localhost:8080`). This is an infra binary sitting next to your app, not a new Go module/JS dependency, so it doesn't trip the "no new third-party dependency" rule — but flagging it since it is a new piece of ops tooling. Alternative: Nginx + Certbot (more config, more battle-tested/familiar if you already know Nginx).

### 4d. Secrets store

Given the lightweight-first principle, don't reach for Vault/AWS Secrets Manager for a first production deploy. A root-only-readable environment file (`/etc/erp/erp.env`, `chmod 600`) loaded via a `systemd` `EnvironmentFile=` directive is the direct equivalent of what `manage.ps1`/`.env` already do locally, just on the production box. Revisit a real secrets manager only once you have multiple people/services needing scoped access to different secrets.

**Your call:** provider + region, domain registrar (if not already owned), TLS approach, secrets approach. You can mix-and-match my recommendations or push back on any of them.

**Once decided, step-by-step (assuming the recommended VPS + Caddy + env-file path):**
1. Spin up the VPS (Ubuntu 22.04/24.04 LTS is the safe default) in your chosen region.
2. Point your domain's DNS `A` record at the VPS's public IP.
3. Install Postgres on the box (or use the provider's managed Postgres add-on if you'd rather not administer it yourself — DigitalOcean and Lightsail both offer one).
4. Copy `erp-server.exe`'s Linux-built equivalent (`GOOS=linux GOARCH=amd64 go build -o erp-server ./cmd/server`) to the box, plus `public/` and migration files.
5. Create `/etc/erp/erp.env` with `chmod 600`, containing `DATABASE_URL`, `OPS_ALERT_WEBHOOK_URL`, `ENV=production`, and any connector/GSP/Pine Labs credentials from the other sections here.
6. Write a `systemd` unit (`/etc/systemd/system/erp.service`) that runs the binary with `EnvironmentFile=/etc/erp/erp.env`, `Restart=on-failure` — this is `manage.ps1`'s process-supervision job, done the Linux-native way.
7. Install Caddy, point its `Caddyfile` at your domain → `reverse_proxy localhost:8080`, `systemctl enable --now caddy`. TLS is automatic from here.
8. Run the existing migrations against the box's Postgres, smoke-test `https://yourdomain/api/v1/...` from outside.
9. Tell me once the box is reachable — I'll help wire `promote.ps1`/`manage.ps1 -Env` (or a new equivalent) to target it, and we close out 26.1.1.

---

## 5. Edge WAF / rate-limiting (26.1.3) — depends on §4

> **Partially closed 2026-08-06.** §4 is settled (the droplet), so the parts that
> don't need a domain are done:
>
> - `deploy/Caddyfile` now carries the real production proxy config — strips
>   `Server`/`X-Powered-By`, caps request bodies at 12MB before they reach Go,
>   health-checks the upstream, sets proxy timeouts above the app's own so long
>   report exports aren't cut off, and rolls access logs.
> - `trusted_proxies static private_ranges` makes Caddy's `X-Forwarded-For`
>   authoritative. This is load-bearing: `TRUST_PROXY=1` is set on the box, and
>   without it a client could forge the header and defeat per-IP rate limiting
>   entirely.
> - Two missing security headers — `Referrer-Policy` and `Permissions-Policy` —
>   were added in the **app** (`internal/server/middleware.go`, alongside the
>   existing HSTS/CSP/X-Frame-Options/nosniff), not on the proxy, so they hold on
>   the SSH-tunnel path too. One owner per header, no drift.
>
> **Deliberately not done: rate limiting at the Caddy layer.** Caddy has no
> built-in rate limiter; the community module needs an `xcaddy` rebuild and a
> custom binary to keep patched — a new build toolchain on the box for something
> the app already does (Stage 13.14: 5/min/IP on the auth category). Rejected as
> a poor trade under the lightweight-first principle.
>
> **Still open, and genuinely blocked on a domain:** the actual edge WAF.
> Cloudflare needs DNS to point at it, so there is nothing to configure until §4's
> domain exists.

App-level rate limiting (Stage 13.14) already covers you in the meantime, so this isn't urgent — it's defense-in-depth on top.

**If you went with a VPS (§4):** put **Cloudflare** (free tier) in front as your DNS — proxied orange-cloud mode gives you WAF rules, DDoS mitigation, and TLS termination at their edge for free, and pairs naturally with buying your domain through Cloudflare Registrar too.
**If you went with AWS:** AWS WAF attached to whatever's in front (ALB/CloudFront) is the native fit.

**Your call:** confirm once §4 is settled — this one is basically determined by that choice, not an independent decision.

**Once decided (Cloudflare path):** sign up at `cloudflare.com` → **Add a site** → point your domain's nameservers at Cloudflare → enable proxying (orange cloud) on your `A` record → **Security** tab → turn on the managed WAF ruleset + a rate-limiting rule on login/API paths.

---

## 6. Tenant-scoped backup scope (26.1.6)

Extends Stage 17.3's whole-DB backup engine to filter by tenant schema. Purely a scope call, no vendor involved.

| Option | Pros | Cons |
|---|---|---|
| **On-demand export only** (recommended to start) | Small addition — one new "export this tenant" action reusing the existing `pg_dump`-based engine filtered to one schema; no new scheduled job/storage growth | No standing per-tenant RPO guarantee — restore point is whenever someone last clicked export |
| Full scheduled per-tenant cadence (e.g. nightly) | Real per-tenant RPO/SLA you can promise a customer | Storage cost scales with tenant count; needs retention/rotation policy per tenant, more ops surface |

**My recommendation:** ship on-demand export now (it's a small, additive extension of existing code); revisit scheduled cadence once you have enough paying multi-tenant customers that a per-tenant SLA is something you're actually selling — consistent with how 26.10.6 (data mart) already defers to "don't build ahead of a measured bottleneck."

**Your call:** on-demand only, full cadence, or a specific cadence (daily/weekly) you already know you need.

**Once decided:** no portal step — this is pure build work once you tell me the scope; I'll implement and flip 26.1.6.

---

## 7. External security/performance reviewer (20.5)

Broader than the pen-test in §13 — this can be a single external technical review (architecture + performance + a basic security pass) rather than a formal penetration test engagement.

| Option | Pros | Cons |
|---|---|---|
| Independent freelance consultant (Upwork/Toptal, look for security+backend generalist) | Cheaper, faster to book, flexible scope | Quality varies, needs you to vet credentials |
| Boutique security/dev consultancy | More structured deliverable, accountable engagement | Higher cost, longer lead time |
| Combine with §13's pen-test vendor | One engagement covers both, avoids duplicate onboarding | Only works if that vendor also does general architecture/perf review, not just pentesting |

**My recommendation:** given the scope overlaps §13, ask your chosen pen-test vendor (§13) if they also offer an architecture/performance review add-on before booking a second, separate engagement.

**Your call:** who, and whether to fold this into §13 or run separately.

**Once decided:** this is a scheduling/contracting task on your end (SOW, NDA, access grant to a staging environment) — tell me once it's booked and I'll prep a scoped read-only reviewer account and a staging snapshot for them.

---

## 8. GSP sandbox — e-invoice/IRN + e-way bill (20.30/20.31, 26.2.3/26.2.4, 26.6.9)

`engines/gst.go`'s GST calc engine is ready to call a real GSP (GST Suvidha Provider) — this is purely the missing credential. A GSP is a government-authorized middleman API for IRN (e-invoice) generation and e-way bill generation.

| Option | Pros | Cons |
|---|---|---|
| **NIC's own free sandbox** (`einv-apisandbox.nic.in` for e-invoice, similarly for e-way bill) — recommended to start | Free, direct from the government body, no vendor contract needed just to test | Lower-level API, less polished docs/support than a commercial GSP; production e-invoice for real turnover still typically goes through a registered GSP or your own NIC production API registration |
| Commercial GSP (ClearTax, Cygnet, MasterGST, Vayana) | Production-grade support, uptime SLA, often bundled with GST filing tools you may already want | Paid, onboarding takes longer, another vendor relationship |

**My recommendation:** register for NIC's free sandbox first to close out the *testing* items (20.30/20.31/26.2.3/26.2.4/26.6.9) cheaply; decide on a paid GSP only once you're actually going live with real invoice volume.

**Your call:** NIC sandbox now (recommended), or go straight to a specific commercial GSP if you already have a relationship/preference.

**Once decided (NIC sandbox path):**
1. You need a real GSTIN registered on the e-invoice/e-way bill system (even a test/dummy-registered one works for sandbox — check current NIC onboarding requirements, this has changed over the years).
2. Register for sandbox access at the NIC e-invoice API sandbox portal; they issue a sandbox `client_id`/`client_secret` and a test GSTIN to use.
3. Similarly register for the e-way bill sandbox (separate registration from e-invoice, same NIC ecosystem).
4. Hand me the sandbox base URL + credentials — I'll wire them into `engines/gst.go`'s GSP client config and run a real IRN generation + e-way bill generation end-to-end against the sandbox.

**Once decided (commercial GSP path):** sign up on the GSP's developer/partner portal → they issue sandbox API credentials (client id/secret, often a separate "test GSTIN") → same handoff to me as above.

---

## 9. Payment-terminal sandbox — Pine Labs (26.2.5)

Stage 25.7 already enforces terminal-mapping checks; only live settlement testing is missing.

**Your call:** confirm Pine Labs (already named in the checklist) vs. a different terminal provider you actually use/plan to use.

**Once decided (Pine Labs):**
1. Go to Pine Labs' developer portal (their integration/API partner program) and register as a developer/merchant partner.
2. Request **UAT/sandbox credentials** for their payment API (Plutus Smart / Pine Labs Online, depending on which integration path fits your terminal type).
3. They issue a merchant ID + secret key for a test terminal, plus test card/UPI credentials to simulate a settlement.
4. Hand me the sandbox credentials — I'll wire them into the existing terminal-mapping config and run a real test settlement end-to-end, then flip 26.2.5.

---

## 10. Supplier portal auth model (26.4.10)

Needed before building the supplier submission/QC-approval workflow at all.

| Option | Pros | Cons |
|---|---|---|
| **Limited-role login** (new "Supplier" role in the existing user/role system) — recommended | Reuses the existing role-permission engine and login flow entirely — zero new auth code, matches this repo's reuse-over-duplicate rule | Suppliers become real per-tenant users (count against any per-tenant user cap/licensing you set up), and a supplier who supplies to 3 of your tenants needs 3 separate logins |
| Dedicated separate supplier portal (its own auth/session surface) | A supplier gets one identity across everything they supply to; cleaner separation of "your staff" vs. "external party" | A second auth system to build and secure — meaningfully more work, and a second attack surface to pen-test |

**My recommendation:** limited-role login, given the "no parallel third way of doing the same thing" principle already governing this codebase — build the dedicated portal later only if you actually have suppliers servicing many tenants and the duplicate-login friction becomes a real complaint.

**Your call:** limited-role login (recommended) or dedicated portal.

**Once decided:** no portal step — pure build work once you confirm; I'll implement the chosen model and flip 26.4.10.

---

## 11. AI content-assist scope (26.4.11)

> **DECIDED AND BUILT 2026-08-06 — local/offline only.** You chose the
> no-third-party-API option, and it answers most of the questions below by
> construction rather than by policy:
>
> - **Q1 (provider / what leaves the server):** no provider, and nothing. The
>   generator (`engines/pim_content_assist.go`) composes a draft from the
>   product's own Item fields and resolved family attribute values using
>   deterministic templates. No network call, no API key.
> - **Q2 (human-in-the-loop):** guaranteed *structurally*, not by convention.
>   `GenerateContentSuggestion` returns a suggestion and has no code path that
>   writes `ProductContent`. Whoever accepts it saves it as an ordinary Draft,
>   which still passes the existing Stage 15.1/26.4.5 approval gate. There is a
>   test asserting no `ProductContent` row is created, so a future refactor that
>   "helpfully" persists the draft fails the build.
> - **Q3 (audit trail):** every generation writes a `ContentAssistLog` row
>   recording the generated text, the source fields it rested on, the user, and
>   the generator id (`local-template-v1` — names the generator, not a model, so
>   rows stay unambiguous if a real model is ever introduced).
> - **Q4 (prompt injection):** no model, so no prompt to inject — but the related
>   risk is real, because supplier-submitted attribute values (§10) can reach
>   published copy. `sanitizeAssistInput` strips angle brackets and control
>   characters from every value before composition, with a test asserting no
>   angle bracket can survive.
> - **Q5 (cost controls):** not applicable, nothing metered.
>
> **The honest limitation:** deterministic templates restate known attributes.
> They cannot write persuasive marketing copy — but they also cannot hallucinate
> a product feature, which is exactly why this shape is defensible without a
> review model behind it. Treat the output as a starting point that saves
> retyping, not as finished copy. If genuinely generative copy is wanted later,
> that is a new decision on Q1, and the audit/human-in-the-loop machinery built
> here already covers it.
>
> The original decision checklist is kept below for that future re-evaluation.

Explicitly excluded until governance/audit/prompt-safety scope is defined (per the source PDF's own §6.1 note) — this section is the checklist of things to actually decide, not a menu of vendors.

Decisions needed before this can be scoped for building:
1. **Which model/provider** does the assist call — and does that mean PIM content (potentially including supplier-submitted text, per §10) leaves this server to a third-party API? If so, which data fields are in-bounds to send.
2. **Human-in-the-loop guarantee** — my recommendation: AI only ever *suggests* a draft; it must still pass through the existing `ProductContent` approval gate (Stage 15.1/26.4.5) before publishing, never auto-publish. This reuses the existing approval engine rather than inventing a new safety mechanism.
3. **Audit trail** — every AI-generated suggestion should be logged as such (who/what prompted it, what model, what was accepted vs. edited) so a reviewer can tell AI-authored content from human-authored content after the fact.
4. **Prompt-injection defense** — since supplier-submitted content (§10) could itself be adversarial input to a future "summarize this supplier's submission" feature, this needs at least a stated scope limit (e.g., "assist only operates on internally-authored draft fields, never directly on unreviewed supplier input") until a real defense is built.
5. **Cost/rate controls** — a per-tenant cap on AI calls if usage-based billing from the provider is a factor.

**Your call:** answer 1-4 above (5 only matters if you're picking a paid API). This is the one item genuinely too open to have a single "recommended" default — it's a product/governance call.

**Once decided:** no portal step by itself (unless your model choice needs an API key from a provider, in which case that's a quick signup); the real work is scoping + building against your answers above.

---

## 12. P2 bundles — go/no-go per bundle (26.5.11, 26.7.8, 26.8.7, 26.9.9, 26.10.6)

> **Corrected 2026-08-06 — this section was stale and had been driving wrong decisions.**
> It described all five bundles as un-started. **Four of the five were greenlit and
> built on 2026-07-27** and have been `[x]` in `micro_checklist.md` since. The table
> below is now the real state. Read `micro_checklist.md` as the authority; this
> section is a decision worksheet, and a worksheet that lags the tracker is worse
> than none — it invites re-deciding settled questions.

| Bundle | What's in it | Status |
|---|---|---|
| **26.5.11** WMS enterprise tier 2 | Slotting/re-slotting optimizer, labor standards/productivity dashboard, RF/voice/mobile picking, 3PL multi-owner billing, robotics/conveyor/scale API integration | ✅ **Built 2026-07-27** (scoped into 26.5.12+) |
| **26.7.8** CRM/loyalty analytics | Customer householding/merge, CLV/cohort/churn analytics, two-way CleverTap segment sync | ✅ **Built 2026-07-27** — `engines/crm_analytics.go` (`MergeCustomers`, `GetCustomerLifetimeValue`, `GetCohortRetention`, CleverTap segment sync), scoped into 26.7.9-26.7.11 |
| **26.8.7** HR/people process | Full KRA/KPI appraisal cycles, training, grievance handling | ✅ **Built 2026-07-27** |
| **26.9.9** Manufacturing scheduling | Finite/infinite capacity scheduling, subcontracting/outside-processing | ✅ **Built 2026-07-27** |
| **26.10.6** BI data mart / read replica | Dedicated data mart or read replica for heavy BI query load | ⛔ **Open — deliberately not built.** See below. |

### 26.10.6 — measured 2026-08-06, and the answer is "not yet"

This is the one bundle still open, and it is open on evidence rather than on
inertia. Its greenlight condition was *"only justified once real report-query
load is measured against the live Postgres instance and shows an actual
bottleneck."* That condition is now **checkable**, because 26.10.7 built the
measurement mechanism (a `ReportRunLog` row per report run, aggregated by the
`report-performance` BI report).

Measured against the live droplet on 2026-08-06: the production database holds
**no transactional data at all** — the largest tenant table is `doctype_fields`
(722 rows, metadata), `documents` has 210 rows, and `gl_postings`, `POSCart` and
`SalesInvoice` are empty. Four user accounts.

There is therefore no query load, no bottleneck, and nothing a read replica
would relieve. Building one now would add a replication topology, a second
connection path and a staleness window to a system whose reports currently
return instantly — cost with no benefit, and precisely the speculative
over-building this bundle's own rationale warns against.

**Revisit when any of these becomes true** (check via Reports → Report Performance):

- any report's p95 duration exceeds ~5 seconds, or
- total report time exceeds ~10% of database busy time, or
- report queries begin measurably slowing transactional work (watch
  `db_pool.wait_duration_ms` on `/api/v1/health`).

Until then the honest status is "measured, not justified" — not "not yet
considered".

**For the four built bundles:** no action. **For 26.10.6:** no action needed
unless you want to override the measurement and build it anyway.

---

## 13. External penetration test engagement (26.11.1) — ideally after §4

A pen-test is most valuable against a real target, so doing this after production hosting (§4) exists is more representative than testing the current dev-only setup — but a first pass against a staging clone before go-live is also a legitimate order if you want the finding fixed before real traffic exists.

| Option | Pros | Cons |
|---|---|---|
| Boutique/firm engagement | Structured report, formal deliverable, good for compliance/customer trust | Higher cost, longer lead time (scoping call → SOW → scheduling) |
| Pentest-as-a-service platform (e.g. Cobalt, HackerOne, Bugcrowd point-in-time engagement) | Faster to start, scoped/fixed-price options exist, vetted testers | Marketplace model — quality/tester assignment less bespoke than a firm relationship |
| Independent certified freelancer (OSCP/CREST, via a professional network) | Cheapest, most flexible scope | Single point of failure on quality/availability; more vetting burden on you |

**My recommendation:** for a first pass on a pre-revenue/early-stage app, a fixed-scope engagement through a pentest-as-a-service platform is the best cost/rigor tradeoff — you get vetted testers and a real report without a full boutique-firm sales cycle.

**Your call:** which tier, and timing relative to §4 (after real hosting exists, or against a staging clone now).

**Once decided:**
1. Scope the engagement to the actual attack surface: the public API, the auth/MFA flow, the multi-tenant isolation boundary (this is the one most worth emphasizing to any tester, given the tenant-schema architecture), and the admin/HR-Admin-gated screens.
2. Provision them a scoped test tenant + a couple of test user accounts (not real customer data) — I can set this up on request.
3. Get the engagement dates in writing (a pentest against production without a signed authorization window is something you do not want to have happen by accident).
4. When the report lands, hand it to me — I'll triage findings against the existing error-catalog/security patterns already in place and fix what's real.

---

## 14. DR/restore drill in a real production-like environment (26.11.3) — depends on §4

The mechanism itself is already proven (Stage 17.3's drill, logged in `docs/operations/restore_drill_log.md`) — this just needs a real prod-like box to drill against instead of the dev machine.

**Once §4 exists, step-by-step:**
1. Schedule a maintenance window (this shouldn't touch live traffic, but say so to whoever's on-call).
2. Take a real backup off the production box using the existing backup engine.
3. Restore it onto a scratch target — either a second small VPS spun up just for this drill, or a second Postgres instance/path on the same box if budget is tight (less realistic but still validates the mechanics).
4. Verify data integrity (row counts, spot-check a few known records) and time the restore (this is your real RTO number).
5. Log the result in `restore_drill_log.md` the same way the existing dev-environment drill is logged.
6. Tear down the scratch target if you spun up a separate one.

No decision needed here beyond "§4 is done" — tell me once hosting exists and I'll walk this with you live.

---

## 15. Business UAT cycle (26.11.5)

Needs real business users — `docs/guides/UAT_CHECKLIST.md` already exists as the script to run through.

**Your call:** who are the 3-5 real pilot users (which store/role mix — ideally at least one cashier, one store manager, one back-office/finance user, matching the roles this app actually has), and what window (1 week / 2 weeks) they'll run the checklist in.

**Once decided:** no portal step — I can prep a clean pilot tenant/seed data for them, walk them through `UAT_CHECKLIST.md` if useful, and track a defect/closure log as they go.

---

## 16. Hypercare window (26.11.6) — depends on §15

Definition needed: on-call owner, duration, rollback trigger, for the first real pilot going live.

**Your call, fill in:**
- On-call owner: _______ (can reuse §1's primary on-call if it's the same person)
- Duration: _______ (a common default is 2 weeks post-launch, tapering to normal support after)
- Rollback trigger: _______ (e.g., "any P0 defect blocking checkout/payment for more than 1 hour triggers rollback to the previous stable build")

**Once decided:** I'll fold this into `incident_runbook.md` as a time-boxed addendum for the pilot launch window.

---

## What happens as you decide

Tell me your call on any section above, in any order — I don't need all 16 at once. For each one I'll:
- Do the code/config side (env var wiring, credential plumbing, a scoping pass, a doc fill-in) once the portal/vendor side is done on your end.
- Flip the matching `micro_checklist.md` item(s) to `[x]` with the real decision recorded inline (matching how e.g. 20.13's offline-queue decision was logged).
- Add a line to `project_ledger.md` once a batch of these closes out, same as every other Stage.
