---
doc_id: DOC-PRODUCT-STAGE75-EINVOICE-PLAN
title: E-invoice readiness - IRIS IRP-6 direct integration plan
type: reference
status: approved
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md Stage 75.2
authority: proposed-design
confidentiality: internal
last_verified: 2026-10-17
review_by: 2026-11-17
supersedes: none
superseded_by: none
---

# E-invoice readiness: IRIS IRP-6 direct integration plan

**As of 2026-10-17. Approved by the user on 2026-10-17.** This is Stage 75.2 in `docs/micro_checklist.md`, and part of the [Stage 75 plan](hosting-media-reports-gst-plan-2026-10-17.md). Source: `IRIS_IRP6_Direct_EInvoice_Integration_Dev_Ready_BRD.pdf` (18 pp.). This stage builds the IRN half of 74.4.1–74.4.5 and 74.4.7. The e-way bill (74.4.6) stays in Stage 74, behind its own gate.

## 1. Goal and gates

**Goal:** when a client needs e-invoicing, turning it on is configuration plus IRIS production approval, not a build.

| Gate | What it unlocks |
|---|---|
| None | 75.2.1–75.2.10: everything that runs against a fake provider |
| IRIS sandbox credentials + the current IRIS spec | 75.2.11 IRIS adapter, 75.2.12 sandbox test pack |
| CA confirms the client is in scope + IRIS production approval + Finance UAT | 75.2.13 pilot on one GSTIN |

**Out of scope here:** the e-way bill (IRIS still marks its EWB APIs as conditional), and a second IRP/GSP adapter (the interface allows one; none is built).

## 2. Choosing the IRN partner

**Default: IRIS IRP-6 direct.**
- IRIS states its core APIs are free after sandbox testing and production approval.
- Taxpayers can integrate directly, with no GSP.
- A sandbox is available.

NIC's direct API is limited to large taxpayers (the BRD cites a ₹100 crore condition), so it doesn't fit our clients.

**Evaluate before committing:** IRIS, one other IRP that offers direct API access, and one GSP (a paid, managed option kept as the fallback quote). Send all three the same questions (§7) and compare them on:

| Criterion | Why it matters to us |
|---|---|
| Direct API access at our clients' turnover | Decides whether we need a GSP at all |
| Price: core API, per document, annual minimum | "Free API" vs 12-month total cost |
| **One integrator account for many client GSTINs** | We are a SaaS issuing for clients, not only for ourselves |
| Static-IP rule and how to change a whitelisted IP | Ties into hosting (75.5.4) |
| Sandbox quality and an exportable current spec | Decides how fast 75.2.11 goes |
| EWB coverage and its status | Gates 74.4.6 |
| SLA, support, rate limits, outage notices | Production operations |

Because the provider sits behind an interface (75.2.5), choosing IRIS now doesn't lock us in.

## 3. What we reuse

| Need | Existing piece |
|---|---|
| Swappable provider | `CourierAdapter` pattern (`engines/courier.go:22`) |
| Durable queue, retries, no double run | `FOR UPDATE SKIP LOCKED` claiming, as in `engines/jobrunner.go:261`; idempotency keys |
| Per-client encrypted credentials | `channel_credentials` (`CHANNEL_CREDENTIAL_KEY`) |
| Seller GSTIN and address | Location masters (`engines/location_masters.go`) |
| Tax amounts, place of supply, HSN | `engines/gst.go`, `gst_place_of_supply.go`, `hsn_catalog.go` |
| Document numbers | Number series (Stage 60, `engines/number_series.go`) |
| Printing IRN + QR | Print formats (Stages 63/71) + `public/qrcode.min.js` (already vendored) |
| Dual approval for cancel and manual import | Maker-checker (`engines/approval.go`) |
| Alerts | The alert webhook (as `BACKUP_STALE`) |
| Reports | `ReportDefinition` |
| Settings | Settings registry (`engines/settings_definitions.go`) |
| RSA, AES, SHA-256, RS256 signature check | Go standard library only, no new module |

## 4. Where it runs

- **Production:** the ERP on the Bangalore (BLR1) droplet makes the IRP calls itself, from the droplet's own IPv4. Confirm that address with `curl -4 https://icanhazip.com` on the droplet, and whitelist it with IRIS. No separate gateway server.
- **Runbook rule:** never destroy or rebuild the droplet without changing the IRIS whitelist first.
- **If 56.11 moves production to Railway:** the IRP worker stays on a Bangalore droplet (same binary, worker-only mode), because Railway has no India region (75.5.4).
- **Dev and staging:** the fake provider by default. Sandbox credentials only, never production ones. A sandbox document can never be retried in production: the environment is part of every unique key.

## 5. Build items

**Buildable now (no credentials):**

- [ ] **75.2.1 Eligibility rules** (74.4.1, IRN half).
  - **Scope:** per legal entity and GSTIN, effective-dated: enabled flag, the turnover band (₹5 crore threshold; the ₹10 crore 30-day rule), document types INV/CRN/DBN, supply types B2B/SEZWP/SEZWOP/EXPWP/EXPWOP/DEXP, and the policy for inter-GSTIN transfers.
  - **Never sent to the IRP:** B2C POS bills, advances, store credit and same-GSTIN transfers. These record `NOT_REQUIRED` with the reason.
  - **Evidence:** every decision stores its rule version.
- [ ] **75.2.2 Canonical tax document.** Built from the *posted* Sales Invoice, Credit Note or Debit Note, and frozen at that moment, with a SHA-256 payload hash. Field mapping follows BRD §7: seller, buyer, document, transaction, items, values and exports. A retry never re-reads live item or customer data.
- [ ] **75.2.3 Validation before submission** (74.4.2). It checks:
  - **Number:** document number ≤ 16 characters, allowed charset, stored uppercase.
  - **Identity and location:** GSTIN format and checksum; PIN, state and place-of-supply consistency.
  - **Lines:** HSN, UQC, and the bill-level discount allocated to lines reproducibly.
  - **Totals:** totals = lines + tax ± round-off, in paise integers, not floats.
  - **Notes:** a note references its original.

  Errors name the field and the fix. The number series used for INV/CRN/DBN on an enabled GSTIN can't be saved if it would produce more than 16 characters.
- [ ] **75.2.4 Data model.** Additive migration `db/migrations_stage75_2_einvoice.sql`, per tenant schema, propagated as Stage 64 does:
  - `irp_profiles`: entity, GSTIN, environment, provider, credential reference, flags, whitelisted IP, test status, activation date.
  - `irp_documents`: unique on (environment, GSTIN, fiscal year, type, normalized number) and on (environment, GSTIN, IRN).
  - `irp_attempts`: redacted.
  - `irp_events`: append-only.

  The signed invoice and signed QR are stored byte-exact. They are small, so they go in the database, not object storage.
- [ ] **75.2.5 Provider interface + fake provider.**
  - **Interface:** `IRPProvider` with Authenticate, GenerateIRN, GetByIRN, GetByDocument, CancelIRN and Health.
  - **Fake provider:** plays back scripted outcomes: success, rejection, timeout after the IRP accepted, "already registered", 401, 429/5xx and IP blocked.
- [ ] **75.2.6 State machine + worker** (74.4.4). It is kept separate from the document's own status.
  - **States:** READY → QUEUED → SUBMITTING → ACKNOWLEDGED / REJECTED / UNKNOWN → RECONCILE; CANCEL_PENDING → CANCELLED / CANCEL_UNKNOWN; MANUAL_REVIEW, MANUAL_IMPORT, BLOCKED_CONFIG, NOT_REQUIRED.
  - **Claiming:** the worker claims rows with SKIP LOCKED and a lease; one submission per document identity.
  - **UNKNOWN:** a lookup by document comes before any retry. UNKNOWN is never shown as success or as failure.
  - **Tokens:** cached per (environment, GSTIN, credential version), refreshed under a per-GSTIN lock.
  - **Failures:** exponential backoff; exhausted retries go to the exception queue.
- [ ] **75.2.7 Locks and corrections** (74.4.5).
  - **Locks:** no tax-field edit after ACKNOWLEDGED.
  - **Cancellation:** only within 24 h, by the tax role, with maker-checker. After 24 h it goes to MANUAL_REVIEW and then a credit note.
  - **Numbers:** a cancelled number is never reused.
  - **30-day rule (turnover ≥ ₹10 crore):** warn at 7, 25 and 28 days, then block.
- [ ] **75.2.8 Print.**
  - **Acknowledged:** IRN, Ack No, Ack Date and the signed QR on the invoice, credit-note and debit-note formats. The QR is drawn from the IRP's own signed QR text with `qrcode.min.js`, never generated by us. A reprint uses the stored bytes.
  - **Before acknowledgement:** an "IRN pending" banner.
  - **B2C:** prints are unchanged.
- [ ] **75.2.9 Screens** (list first, Stage 69).
  - **Tax Integration Settings:** per entity and GSTIN, credential status masked, "Test connection" (Health + Authenticate), auto-submit **off** by default.
  - **IRN Exception Workbench:** tabs Needs Action / Queued / Unknown / Rejected / Acknowledged / Cancelled; filters for GSTIN, date, source and age; actions Validate, Submit/Retry (only when eligible), Check status, Cancel (reason + approval), Manual import (dual verification).
  - **Badge:** an IRN status badge on invoice and note screens.
- [ ] **75.2.10 Reconciliation, alerts and reports** (74.4.7, IRN half).
  - **Daily job:** expected vs generated per GSTIN, plus UNKNOWN and QUEUED ageing.
  - **Alerts:** `IRP_UNKNOWN_AGING` (> 2 min), `IRP_QUEUE_AGING` (> 5 min), `IRP_AUTH_FAILING` (5 in a row), `IRP_IP_BLOCKED`, `IRP_30DAY_RISK`, `IRP_CANCEL_WINDOW` (< 2 h left).
  - **Catalog reports:** "E-Invoice / IRN Status Register", "E-Invoice Error / Retry Report" and "IRN Portal Manual Upload Report" (= R-TX-09).

**After sandbox access:**

- [ ] **75.2.11 IRIS adapter.** The encryption envelope, auth and the five core calls, implemented **exactly** as IRIS's current spec says (BRD SB-06), not guessed. Signed responses are verified with IRIS's published key. Contract tests are recorded from the sandbox.
- [ ] **75.2.12 Sandbox test pack.** The BRD's 45 cases (AUTH, ID, INV, VAL, QR, RET, CAN, OPS; EWB excluded) run in `go test` against the fake provider, then against the IRIS sandbox. This produces the IRIS test summary file and Finance sign-off on the prints.

**After the CA and IRIS approval:**

- [ ] **75.2.13 Pilot.**
  - **Onboarding:** IRIS production steps PR-01–PR-10, then a canary with one low-risk B2B invoice.
  - **Outage path:** the manual-portal SOP in `incident_runbook.md`.
  - **Docs:** ADMIN_GUIDE (setup), USER_GUIDE (workbench), USER_SOP (outage).

## 6. Your actions (these take calendar time, so start early)

1. **Register in the IRIS sandbox**: one Taxpayer user (SB-01), and a separate API Integrator user with a different organisational email (SB-02). Add an IRIS dummy test GSTIN (SB-04).
2. **Send the questions in §7** to IRIS (support@irisirp.com), and the same list to one other IRP and one GSP.
3. **Ask the CA for the scope matrix** (§8).
4. **Credentials go into the ERP's settings screen** (75.2.9) or the server env file, never chat or email.

## 7. Questions for IRIS (and the comparison partners)

- Current sandbox and production base URLs, API versions, required headers, the encryption and key-rotation method, and how to verify the signed QR.
- **We are an ERP provider.** Can one API Integrator account serve many client taxpayers' GSTINs, each authorising us? What consent or KYC is needed per client?
- Can we use the sandbox with dummy GSTINs only, before any of our clients' GSTINs is e-invoice enabled?
- Production approval documents, typical approval time, how the Indian static IP is verified, and how to change a whitelisted IP.
- Are generate, cancel and lookup free at our volume? What are the rate limits? Any support or storage charges?
- E-way bill: generate with IRN, generate later by IRN, cancel, vehicle update, validity extension. Which are live, and which are charged?
- SLA, maintenance notices, history retrieval, and their recommended outage process.

## 8. Decisions for the CA / Finance (BRD §17)

| ID | Decision |
|---|---|
| D01 | Which legal entities, GSTINs and document types to activate |
| D02 | Turnover (AATO) eligibility per PAN |
| D03 | Inter-GSTIN transfer and job-work invoice treatment |
| D04 | Who owns seller, ship-from, dispatch, bill-to and place of supply |
| D05 | Credit-note and debit-note numbering and references |
| D06 | When the IRN is generated: at posting, or before the first print |
| D14 | Manual portal fallback, and which roles may use it |
| D15 | Pilot GSTIN and the first live invoice |

## 9. Done when

- All 45 BRD cases pass against the fake provider, including RET-03 (two workers, one submission) and RET-04 (restart mid-submit).
- A test proves no secret or token reaches a log line, and one proves a user of one tenant or GSTIN can't read or act on another's documents.
- The sandbox run passes, and the IRIS test summary is produced (after 75.2.11).
- The ADMIN_GUIDE, USER_GUIDE and USER_SOP sections exist and have been followed step by step.
