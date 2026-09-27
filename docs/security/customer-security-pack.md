---
doc_id: DOC-CUSTSECPACK01
title: Customer security pack
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [customers, prospective customers, security-owner]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: customer
last_verified: 2026-09-16
review_by: 2026-10-16
supersedes: none
superseded_by: none
---

# Customer security pack

**Stage 49.16.3** — established 2026-09-16. The one document meant to leave
this repository and go to a customer, prospect, or their security reviewer
during due diligence. Everything in it is true and current as of the review
date above; everything it omits (specific unresolved findings, exact patch
timing, exploit-enabling detail) is omitted on purpose, per
[vulnerability-disclosure-lifecycle.md](vulnerability-disclosure-lifecycle.md)'s
confidentiality section — a reviewer who needs that level of detail gets it
under NDA, not in a document meant for general distribution.

> **What this is not.** Not a certification, not an audit opinion, not a claim
> of ISO/SOC attestation (none exists — see §7). It is an honest architecture
> and control summary, written the same way every other document in
> `docs/security/` is: every claim points at something real in this
> repository, and every open gap is named as a gap, not hidden.

## 1. Architecture and data flow

Single-tenant-per-schema multi-tenant ERP: one Go binary (`erp-server`), one
PostgreSQL database, one **schema per tenant** (`tenant_<name>`), with a
shared `public.tenants` registry mapping tenants to their schema. A request
resolves its tenant first (subdomain or explicit header, depending on
deployment mode — see §2), then every subsequent query is scoped to that
tenant's schema by the application layer, not by a shared table with a
`tenant_id` column a query could forget to filter on. Full trust-boundary
detail (numbered, for anyone doing a deeper review) is in
[threat_model.md](threat_model.md) §3.

**Notable, and unusual for a vendor of this kind: the entire source tree is
publicly auditable.** This is an open-source project
(`github.com/Vishalnayak226/custom_erp`, public repository) — every control
described in this pack, including the ones that are incomplete, is visible in
the actual code, not asserted in a slide. That is a stronger transparency
position than most vendors offer, and it is also why this pack is careful not
to describe *specific unpatched weaknesses* — those would be a public roadmap
for an attacker in a way that a closed-source vendor's equivalent gap would
not be.

## 2. Deployment and shared-responsibility model

The only configuration with production evidence today is a **single
self-hosted box**: one droplet, systemd-managed application, PostgreSQL on
the same host over loopback, Caddy terminating TLS in front of it
([threat_model.md](threat_model.md) §1.1, configuration D1). An optional mode
(D2) adds per-tenant hostnames with automatic per-tenant TLS certificate
issuance.

| Responsibility | Who owns it |
|---|---|
| Application code, its own vulnerabilities and their fixes | This project (see §5, §6) |
| TLS certificate issuance and renewal | Caddy, automatically, on this project's deployment |
| Physical/network security of the host | Whoever hosts the deployment (today: a single DigitalOcean droplet — see the third-party inventory, §8) |
| Tenant configuration choices (roles granted, MFA enforcement, retention settings) | The tenant administrator |
| Data the tenant chooses to import, and its accuracy | The tenant |

## 3. Encryption, keys, backup and disaster recovery

**Encryption.** TLS in transit (Caddy). At rest: connector credentials
(Shopify/BigCommerce/Magento tokens) and nightly backups are AES-256-GCM/
AES-256 encrypted; passwords are bcrypt-hashed, never stored plain. Full key
inventory — what each key protects and how it rotates — is public in
[README.md](README.md)'s "Data classification, keys and secrets" table,
because a customer's security reviewer asking "what happens if a key leaks"
deserves a real answer, not "trust us."

**Backup and DR**, honestly, gaps included: nightly encrypted `pg_dump`,
14-day on-box retention (`docs/operations/backup_restore.md`), a documented
and scriptable monthly restore-drill procedure. **Two known gaps, not
hidden:** there is no off-box backup copy today (losing the host loses the
backups with it — tracked, not silently accepted), and automated
failure-alerting on a missed nightly backup exists in the code but needs one
operator-set environment variable to actually fire (tracked as an existing
open item). A customer whose contract requires an RPO/RTO commitment beyond
what a 14-day on-box daily backup provides should raise that explicitly
before relying on this deployment as-is.

## 4. Access control and separation of duties

Role-based access (built-in Super Admin/Store Manager/Cashier roles plus
custom table-driven roles), field-level permission enforcement, and a
maker-checker approval engine for controlled actions (discounts over
threshold, refunds, master-data changes) — an initiator cannot approve their
own controlled action by default. Every authenticated route is classified and
tested against exactly this ([verification-standards-matrix.md](verification-standards-matrix.md)
ASVS V4/V8 rows). A closed, fully product-wide "one authorization contract"
spanning every object/field/action/tenant/scope combination is in progress,
not yet complete — named honestly in the same matrix rather than implied
finished.

## 5. Logging, incident response and vulnerability process

**Audit trail.** Every business-critical action is recorded with independently
signed evidence (per-row HMAC) plus periodic checkpoints that detect
*deletion*, not just tampering of what remains — a chain-hash design was
deliberately rejected because it does not survive concurrent writes at this
system's actual throughput. See [README.md](README.md)'s test table for the
specific regression tests this claim rests on.

**Incident response.** A documented containment/eradication/recovery process
exists; see [vulnerability-disclosure-lifecycle.md](vulnerability-disclosure-lifecycle.md)
§49.15.6 for the honest status of live-drilling it (documented, not yet
timed end-to-end).

**Vulnerability handling.** Public reporting channel, safe-harbor terms and
severity-based remediation SLOs are all in
[SECURITY.md](../../SECURITY.md) and its companion lifecycle document —
this is the same document a reporter or a customer's security team would be
pointed at.

## 6. Subprocessors, residency and retention

Every outbound third party this system talks to on a customer's behalf —
channel connectors, couriers, payment terminal, hosting — is inventoried in
[third-party-risk-register.md](third-party-risk-register.md), including which
ones see customer data, at what criticality, and what the exit/deletion plan
is if a subprocessor relationship ends. Data residency and statutory
retention are the explicit subject of Stage 47.16/48.6, tracked as an open,
counsel-reviewed program, not asserted here.

## 7. Independent assessment

**None has been performed yet.** No ISO/SOC/industry attestation exists, and
no third-party penetration test has been engaged (tracked as an open item,
`20.5`/`26.11.1` in this project's own backlog). This section will be updated
the day either changes — a customer relying on this pack today is relying on
this project's own internal testing (unit/integration/adversarial test
suites already in the codebase) and the public auditability described in §1,
not on an external attestation.

## 8. Third-party inventory

See [third-party-risk-register.md](third-party-risk-register.md) for the full
list (hosting, DNS/TLS, payment, messaging, courier/channel connectors,
code/CI dependencies) with criticality and evidence status for each.

## 9. Supported versions and residual/configuration risk

**Supported versions:** see [SECURITY.md](../../SECURITY.md) — there is no
tagged release line yet; the only supported version is the commit currently
deployed to production. **Residual/configuration risks a customer should be
aware of before go-live**, named rather than buried: MFA is available but not
mandatory-by-default for every privileged role yet (tracked, `49.2.3`); CSRF/
strict-CSP hardening is in progress, not complete (`47.8`/`49.4`); no
off-box backup copy exists today (§3); no independent assessment has been
performed yet (§7). None of these are hidden findings — every one of them is
a named, tracked item in this project's own public backlog.

---

**Requesting more detail?** A customer under NDA can be given deeper evidence
(specific test names, `risk_register.md` rows, `attack_surface.json`) than
what's appropriate for general distribution in this pack — ask through the
channel in [SECURITY.md](../../SECURITY.md).
