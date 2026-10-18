---
doc_id: DOC-PRODUCT-STAGE75-HOSTING-PLAN
title: Stage 75 plan - off-box backups, e-invoice readiness, reports, media, hosting
type: reference
status: approved
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md Stage 75
authority: proposed-design
confidentiality: internal
last_verified: 2026-10-17
review_by: 2026-11-17
supersedes: none
superseded_by: none
---

# Stage 75 plan: off-box backups, e-invoice readiness, reports, media, hosting

**As of 2026-10-17. Approved by the user on 2026-10-17.** Status is tracked as **Stage 75** in `docs/micro_checklist.md`. The e-invoice phase has its own plan: [einvoice-iris-irp-plan-2026-10-17.md](einvoice-iris-irp-plan-2026-10-17.md).

Inputs:
- `Low_Cost_Enterprise_Suite_Hosting_Reports_Media_GST_Plan.pdf` (13 pp.);
- `IRIS_IRP6_Direct_EInvoice_Integration_Dev_Ready_BRD.pdf` (18 pp.);
- the user's report catalogs `ERP_Operational_Reports_Master_Catalog.xlsx` (152 reports) and `Enterprise_Extended_Modules_Report_Catalog.xlsx` (138 reports).

## 1. Decisions

| # | Decision | What it means for the build |
|---|---|---|
| D1 | **Cloudflare R2** holds the off-box copies | One private bucket for database dumps and product images. Free at our size, and a different company from DigitalOcean |
| D2 | **DigitalOcean daily droplet backups** (~₹150/month) | A whole-machine image off the droplet, at most 24 h old. A second layer, not the main one |
| D3 | **Media: build the swappable storage layer now**; the R2 backend only when needed | 75.4.1 now; 75.4.2–75.4.4 wait for their gate |
| D4 | **Reports: the two catalogs are the spec** | 290 reports, with their column lists. The report columns must match the catalog |
| D5 | **E-invoice: build ready now against IRIS IRP-6; go live per client only after the CA confirms** | Everything except the IRIS-specific adapter can be built and tested before any credentials exist |
| D6 | **Railway is on the Pro plan.** Production stays on the droplet until 56.11 | Stage 56 as written, plus 75.5. IRIS needs an Indian IP (see 75.5.4) |

## 2. Order of work

| Phase | What | Starts | Your part | Touches production? |
|---|---|---|---|---|
| 75.1 | Off-box backups | Now | R2 bucket + token, DO daily backups, copy of the backup key | Yes: the backup script |
| 75.2 | E-invoice readiness | Right after 75.1 | IRIS sandbox registration, questions to IRIS, CA scope | No, until the pilot |
| 75.3 | Reports to the catalog | After 75.3.1 (your 15-minute check) | 75.3.1 | Read-only reports, normal deploy |
| 75.4 | Media storage layer | 75.4.1 any time (small) | None | No behaviour change |
| 75.5 | Hosting additions to Stage 56 | With your Railway testing | Railway testing | No |

E-invoice comes before reports because IRIS registration and approval take calendar time on IRIS's side, and the code should be ready when the sandbox credentials arrive. Reports are the largest phase. They run in three waves and can continue after that.

## 3. Phase 75.1 — Off-box backups (P0)

**Why:** nightly encrypted dumps stay on the droplet only (`docs/operations/backup_restore.md:68`), and `/opt/erp/media_store` is in no backup at all. Losing the droplet today loses the data, its backups and every product photo.

Your actions:
- [ ] **75.1.1 Create the R2 bucket.** Make it private, with an API token scoped to that one bucket (Object Read & Write) and a lifecycle rule that deletes objects after 30 days. If the dashboard offers a bucket lock rule, turn it on for 14 days, so a compromised server can't delete the copies. Put the endpoint, bucket name, access key and secret in `/etc/erp/erp.env` as `OFFSITE_S3_ENDPOINT`, `OFFSITE_S3_BUCKET`, `OFFSITE_S3_ACCESS_KEY` and `OFFSITE_S3_SECRET_KEY`. Never paste them in chat.
- [ ] **75.1.2 Turn on daily backups** for the droplet in the DigitalOcean console.
- [ ] **75.1.3 Copy the production `BACKUP_ENCRYPTION_KEY`** from `/etc/erp/erp.env` into your password manager. An off-box dump can't be restored if its key dies with the droplet.

Build:
- [ ] **75.1.4 Upload each dump.** `deploy/backup.sh` uploads each `custom_erp_*.dump.enc` to the bucket using `curl --aws-sigv4`, which is built into curl 7.75+, so there is no new tool or Go module. Check the droplet's curl version first; `rclone` is the fallback only if it is too old.
  - **Recording:** the result goes in the existing `backup_runs` row, as additive columns `offsite_key` and `offsite_outcome` (`db/migrations_stage75_1_offsite_backup.sql`).
  - **Failure:** a failed upload keeps the local dump and posts `BACKUP_OFFSITE_FAILED`.
- [ ] **75.1.5 Copy new images nightly** to the bucket under `media/`. Files are named by checksum and never rewritten, so this is upload-only: a local manifest of names already sent, and an upload of whatever is missing. It is recorded as a `backup_runs` row with scope `media`.
- [ ] **75.1.6 Freshness alert.** `CheckBackupFreshness` (`engines/alerting.go`) also alerts when the newest *off-box* success is older than 26 h. Add a test beside `TestBackupFreshness`.
- [ ] **75.1.7 Restore drill from the bucket only.** Download the dump and images from R2, decrypt with the key copy from 75.1.3, restore on a scratch database, and open a product with its image. Log it in `docs/operations/restore_drill_log.md`.
- [ ] **75.1.8 Docs:** `backup_restore.md` (replace the line-68 "no off-box copy"), ADMIN_GUIDE §D, `incident_runbook.md`, and `go_live_decisions.md` (D1, D2).

**Done when:** 75.1.7 passes, and a forced upload failure raises an alert.

## 4. Phase 75.2 — E-invoice readiness (IRIS IRP-6 direct)

Full plan: [einvoice-iris-irp-plan-2026-10-17.md](einvoice-iris-irp-plan-2026-10-17.md). In short:
- **Partner:** IRIS IRP-6 direct is the default (its core APIs are free after approval). It is evaluated against one other IRP and one GSP using the same question list. The provider adapter keeps that choice reversible.
- **Before credentials:** the eligibility rules, canonical tax document, validation, data model, state machine, worker, fake provider, print, screens, reconciliation, alerts and reports.
- **After sandbox access:** the IRIS adapter (encryption, auth and endpoints exactly as IRIS's current spec says) and the sandbox test pack.
- **Where it runs:** IRIS accepts calls only from 1–4 whitelisted **Indian** static IPs, so calls go out from the Bangalore droplet.
- **Stage 74.4:** this builds the IRN half of 74.4.1–74.4.5 and 74.4.7. The e-way bill (74.4.6) stays in Stage 74, behind its own gate.

## 5. Phase 75.3 — Reports to the catalog

**Where we start.** The engine is built: about 100 registered reports, a catalog, run, drill-down, async CSV export, a sync row cap, a run log, scheduling, a column chooser, saved column profiles and filter presets. First-pass mapping of the 290 catalog reports (75.3.2 produces the exact matrix):

| Catalog sheet | Reports | Exist | Planned in Stage 73/74/75.2 | Wait for a Stage 74 module | To build |
|---|---|---|---|---|---|
| Sales & POS | 22 | ~2 | 2 | 3 (customer advance) | ~15 |
| Inventory | 21 | ~6 | 1 | – | ~14 |
| Inventory Loss & Audit | 12 | – | 4 (stock adjustment) | 8 (wastage, damage) | – |
| Procurement & GRN | 31 | ~2 | ~4 | 2 (receipt note) | ~23 |
| Stock Transfer | 9 | – | 1 | – | ~8 |
| External Movement | 17 | – | – | 17 (challan, job work, MI/MR) | – |
| GST & Compliance | 9 | ~1 | 4 (3 IRN in 75.2, EWB) | – | ~4 |
| E-commerce & Integration | 12 | ~3 | – | – | ~9 |
| Master Data & Audit | 11 | ~1 | – | – | ~10 |
| MIS & Management | 10 | ~1 | – | – | ~9 |
| OMS | 13 | ~6 | – | – | ~7 |
| PIM | 16 | ~3 | – | – | ~13 |
| Accounting | 26 | ~13 | – | – | ~13 |
| Expense | 11 | – | – | – | ~11 |
| CRM | 13 | ~5 | – | – | ~8 |
| HRMS | 16 | ~1 | – | – | ~15 |
| Manufacturing | 16 | ~5 | – | – | ~11 |
| WMS | 27 | ~6 | – | – | ~21 |
| **Total** | **290** | **~55** | **~16** | **~30** | **~190** |

About a third of the catalog is document registers and their item-wise detail ("PO Register" + "PO Itemwise Detail", "Transfer Out Register" + "Itemwise Detail", …). Writing those one by one would repeat the same code dozens of times, so one config-driven register builder comes first (75.3.4).

- [ ] **75.3.1 Why does it read as "missing"?** *Your 15-minute check on production:*
  1. Is the `reports` module enabled for the client tenant? Every report route is behind `moduleGate("reports", …)`.
  2. Does the client have transactions yet?
  3. Can you reach a result by following the USER_GUIDE "Reports" steps?

  If the module is off, that part is a setting, not a build.
- [ ] **75.3.2 Coverage matrix + column spec.**
  - `docs/product/report-catalog-coverage.md`: every one of the 290 rows marked as Exists (with registry id), Partial, Planned (with item id), Waits for module, or To build.
  - `docs/specs/reports/catalog_columns.tsv`: report name, column header and meaning only, **no sample data**. It becomes test data.
- [ ] **75.3.3 Store scope on reports.** Stage 74's re-check found reports have no store scope. Add it at the shared choke point (`RunReportContext` / export), so every existing and new report inherits it. Then test that a one-store role can't see or export another store's rows, through either run or export. This comes before new reports, because every new report inherits it.
- [ ] **75.3.4 Generic register builder** on `ReportDefinition`. One config = doctype, header/line column map, lookups (store, vendor, customer, item names), standard filters (date range, store/location, status, party) and totals. Each config registers as a normal report, so catalog, export, profiles, run log and scope all apply. It adds an **image column** type (thumbnail through `/pim/media/{id}/thumbnail`) for the six "with Image" reports. It also adds a **column-fidelity test**: each catalog-backed report's columns equal the catalog's list, in name and order.
- [ ] **75.3.5 Wave A — retail core:** Sales & POS, Inventory, Procurement & GRN, Stock Transfer, GST & Compliance (the rows not waiting on a module). Control totals that tie sales, stock valuation, AP/AR ageing and GST to the GL ship with this wave (= 73.2.19, pulled forward).
- [ ] **75.3.6 Wave B:** MIS & Management, Master Data & Audit, E-commerce & Integration (the ERP vs Unicommerce reconciliations use the existing connector), and the Accounting gaps. "Report Download Audit" comes from `ReportExportJob` plus `ReportRunLog`, which gains a `params` field.
- [ ] **75.3.7 Wave C:** the OMS, PIM, CRM, Expense, HRMS, Manufacturing and WMS gaps.
- **Rows waiting on a module** (customer advance, wastage/damage, receipt note, delivery challan, job work, MI/MR) ship with those Stage 74 modules under 74.5.1, using the same builder. They are not built here.
- **XLSX:** not built. CSV opens in Excel. Revisit when a named report needs a real workbook.

**Done when, per wave:** every report in the wave is registered, its columns match the spec, it has a USER_GUIDE entry, and it has passed a live check on a scratch port with seeded data.

## 6. Phase 75.4 — Media storage layer

- [ ] **75.4.1 `MediaStore` interface (now).** Put, Get and Stat, with a local-disk implementation that behaves exactly as today. Route every read and write site through it:
  - `pim_media.go:138, 295-301`;
  - `pim_dam.go:55, 122, 138, 287`;
  - `handlers_pim_dam.go:44`;
  - `handlers_procurement_pim2.go:430, 456`.

  The existing tests must pass unchanged.
- **Gate for 75.4.2–75.4.4:** 56.11 picks Railway, **or** droplet disk use passes 70%, **or** a channel connector needs public image URLs.
- [ ] **75.4.2 R2 backend, standard library only.** SigV4 signing written by hand over `net/http` and tested against AWS's published signing vectors; no AWS SDK. New object keys are tenant-prefixed (`<tenant_schema>/<checksum><ext>`). Today identical files are shared across clients, which is harmless only while nothing deletes files.
- [ ] **75.4.3 Copy and switch.** An idempotent, checksum-verified copy of `media_store/` into the bucket, then `MEDIA_STORE=s3` per instance. Test: upload, thumbnail, transform, redeploy, a cross-tenant denial, and images still loading after a database restore.
- [ ] **75.4.4 Public CDN URLs for approved catalog images.** `[needs design decision: only when a channel needs them]`.

## 7. Phase 75.5 — Hosting additions to Stage 56

- [ ] **75.5.1 Record the Railway Pro billing facts in 56.7:** seat fee, included usage, overage rules and region.
- [ ] **75.5.2 Staging guard in 56.1:**
  - the instance has its own Postgres and fresh secrets;
  - no live channel, payment or IRP credentials are ever entered on it;
  - `DATABASE_URL` never names the droplet.
- [ ] **75.5.3 Measure for 7 days instead of 5** (56.7), so the sample includes a weekend.
- [ ] **75.5.4 Add to 56.11's inputs: IRIS needs 1–4 whitelisted Indian static IPs, and Railway has no India region.** Moving production to Railway would still need a Bangalore droplet to send IRP calls (the same binary in a worker-only mode). That is one more reason to keep production on the droplet unless Railway clearly wins on cost and latency.

## 8. Not adopted from the PDFs

| Proposal | Why not |
|---|---|
| A new report engine and new report APIs | It exists. Rebuilding would break ~100 working reports |
| Presigned browser upload (`upload-intent` → PUT → `finalize`) | It adds CORS and an orphan sweeper for small product photos that the API already checksums |
| Quarantine states and malware scanning for media | Only images are stored today. Revisit when non-image attachments arrive |
| Two storage providers now | No private documents live in object storage yet |
| A separate IRP gateway server | The ERP already runs on the Bangalore droplet. A second server only if production moves (75.5.4) |
| XLSX on day one | CSV covers it |

## 9. Guardrails (every phase)

- Client data is never affected, and no password that is already set is changed.
- Migrations are additive only (`db/migrations_stage75_<n>_<name>.sql`). Binary and migrations deploy together.
- No new Go modules or JS libraries. Crypto and SigV4 use the standard library, and the backup script uses curl's built-in signing.
- Secrets live in env or encrypted credentials only: never in `public/`, logs, exports, chat or git.
- Nothing is pushed or deployed until you say so.
- Each item is done only after build/vet/test, a live check on a scratch port, and the guide and tracker docs are updated.
