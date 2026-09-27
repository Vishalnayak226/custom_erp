---
doc_id: DOC-THIRDPARTY01
title: Third-party risk register
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

# Third-party risk register

**Stage 49.16.6** — established 2026-09-16. Reuses
[threat_model.md](threat_model.md) §1.2's integration list as the source of
truth for *what* is integrated — this register adds the operational
dimensions §1.2 doesn't cover: data/access/criticality, contract/security
evidence, and the exit/credential-revocation/data-deletion plan for each.

**Reassess a row on material provider change or incident** — a provider
changing its own security posture, a breach disclosure, or this project
changing which capabilities it uses from that provider are all triggers,
not just the calendar `review_by` date above.

## How to read a row

- **Criticality** — what breaks if this provider is unavailable or
  compromised: **Critical** (the platform itself, or customer funds/data,
  directly at risk), **High** (a feature degrades or customer data is
  exposed but the platform keeps running), **Low** (cosmetic/non-data
  impact).
- **Data seen** — the most sensitive category of data this provider ever
  receives from this system, not the average case.
- **Evidence** — what this project has actually checked (a config file, a
  test, a code review), not a vendor's own marketing claim.

## Infrastructure

| Provider | Role | Criticality | Data seen | Contract/security evidence | Exit/revocation/deletion plan |
|---|---|---|---|---|---|
| DigitalOcean | Hosting (the single production droplet, `deploy/README.md`) | **Critical** | Everything — full database, application logs, backups at rest on the box | Standard DO terms; no dedicated enterprise security review performed | Data: encrypted backups already flow to the box owner independently of DO; destroying/recreating the droplet is the "exit" — `deploy/deploy.ps1` can redeploy to a fresh host from source + backup. Credential: root SSH key rotation is a manual operator action, not yet scripted — **tracked gap**. |
| Cloudflare | DNS for `wholeops.in` (zone exists per Stage 26.1.3b; edge proxy/WAF not yet activated — DNS-only today) | **Low today, High once WAF activation lands** | DNS queries only while proxy is off (grey-cloud) | Standard Cloudflare account terms | DNS is portable to any registrar/DNS host; no data lock-in. Revoking Cloudflare API access (if ever granted for automation) is a dashboard action, not yet needed since nothing here calls Cloudflare's API. |
| GitHub | Source hosting, CI/CD (`.github/workflows/ci.yml`), Actions runners | **Critical** (compromise = supply-chain compromise, per [threat_model.md](threat_model.md) persona P7) | Full source, CI secrets configuration (none of substance configured yet — see [secure-development-lifecycle.md](secure-development-lifecycle.md) §49.9.3) | Public repository; branch protection/CODEOWNERS enforcement is `[needs decision: org owner]`, tracked in that same document | Git history is fully portable (clone elsewhere); no proprietary lock-in. Revoking a compromised PAT/deploy key is a GitHub settings action. |
| Go module proxy (`proxy.golang.org`), `govulncheck` database | Build-time dependency resolution and vulnerability scanning | **Low** (build-time only, never reachable from the running production binary) | Nothing — public module names/versions only | `go.sum` pins every module's content hash; `internal/supplychain` tests enforce the ledger ([dependency-inventory.md](dependency-inventory.md)) | No exit needed — vendoring is a fallback already compatible with Go's toolchain if the proxy became untrusted. |

## Payments

| Provider | Role | Criticality | Data seen | Contract/security evidence | Exit/revocation/deletion plan |
|---|---|---|---|---|---|
| Pine Labs | POS payment terminal integration | **Critical** (money) | Transaction amount/reference, terminal identifiers — **never full PAN/CVV/track data**, enforced by [verification-standards-matrix.md](verification-standards-matrix.md)'s data-protection row and confirmed by grep (49.6.2) | Terminal-mapping checks enforced in code (Stage 25.7); live settlement sandbox testing is a named open item (`26.2.5`, needs sandbox credentials) | Credential rotation/revocation is provider-side (terminal re-pairing); no PAN data is ever stored here to delete. |

## Messaging

| Provider | Role | Criticality | Data seen | Contract/security evidence | Exit/revocation/deletion plan |
|---|---|---|---|---|---|
| Operator-supplied SMTP (`SMTP_HOST` etc., `deploy/erp.env.example`) | Password reset, notifications | **High** (a compromised SMTP relay could intercept password-reset links) | Recipient email address + notification content (password-reset links are redacted from logs in production — `engines/telemetry_redaction.go`, Stage 49.6.7) | Operator brings their own provider; this project enforces no specific one, so evidence is whatever the deploying operator's SMTP provider offers | Changing SMTP provider is an env-var change; no data retained here beyond normal mail-send logs. |
| CleverTap | Customer engagement/marketing messaging (per [threat_model.md](threat_model.md) §1.2) | **Medium** | Customer contact info for engagement campaigns, scoped to what the integration sends | Credential storage shares the same `CHANNEL_CREDENTIAL_KEY` encryption as channel connectors | Disconnecting the integration is a per-tenant credential removal; no separate data-deletion request has been exercised against CleverTap yet — **tracked as an open verification**. |

## Channel connectors (e-commerce)

| Provider | Role | Criticality | Data seen | Contract/security evidence | Exit/revocation/deletion plan |
|---|---|---|---|---|---|
| Shopify | Order/inventory sync; inbound webhooks | **High** | Order, customer and inventory data for the connected store | HMAC-verified inbound webhooks, fail-closed when `SHOPIFY_WEBHOOK_SECRET` is unset ([threat_model.md](threat_model.md) §1.2); credentials AES-256-GCM encrypted | Per-tenant credential deletion via `channel_credentials` row removal; Shopify-side app uninstall is the customer's own action. |
| BigCommerce, Magento, Unicommerce | Same connector pattern as Shopify | **High** | Same category as Shopify | Same encryption; UI-driven credential save has zero UI callers outside Pine Labs/Unicommerce per the existing `channel-connectors.md` KB gap note — **tracked**, not hidden | Same per-tenant credential removal pattern. |

## Courier

| Provider | Role | Criticality | Data seen | Contract/security evidence | Exit/revocation/deletion plan |
|---|---|---|---|---|---|
| Delhivery, Shiprocket | AWB generation, pickup/cancel, NDR resolution, tracking | **Medium** | Customer shipping address, order reference | Webhook verification/manifest/RTO handling exists per the `courier-integrations.md` KB handbook (Stage 39.14); the real Stage 35.5 courier engine has zero UI callers today per that same handbook's documented gap | Per-tenant credential removal; no persistent PII beyond what's needed for an active shipment. |

## Support and assessment providers

| Provider | Role | Criticality | Data seen | Contract/security evidence | Exit/revocation/deletion plan |
|---|---|---|---|---|---|
| *(none yet)* | Customer support tooling, external pentest/audit firm | n/a | n/a | **No formal support tooling or assessment provider engaged yet** — the external pentest referenced throughout this project's Stage 47/49 work (`20.5`/`26.11.1`) is a named open item, not yet contracted. Recorded honestly as an empty row rather than omitted, since 49.16.6 explicitly asks for support/assessment providers as a category. | n/a until one is engaged — when it is, this row gets real content, not a placeholder. |

---

Every row above traces back to something checked in this session (a code
citation, a config file, or an explicit existing open-item number) — no row
was filled from an assumption about what a typical ERP "probably" integrates
with.
