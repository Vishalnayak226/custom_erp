---
doc_id: OPS-CAP-001
title: Storage forecast
type: reference
status: draft
owner: operations-owner
approvers: [operations-owner]
audience: [engineering, operations]
applies_to: source after release 0.18.0; re-measure on every release that changes what a transaction writes
authority: canonical
confidentiality: internal
last_verified: 2026-10-28
review_by: 2026-12-28
supersedes: none
superseded_by: none
verification_scope: per-transaction sizes measured on one synthetic tenant on a development database; volumes, growth and backup sizes projected from them
---

# Storage forecast

How many bytes each kind of transaction adds, how fast a client's database grows at three sizes, what is swept and what is kept, and how much temporary space migrations, indexes and backups need (Stage 47.18.2). Every figure is marked **measured** or **projected**. The daily-state stores (logs, tokens, jobs) have their own read-only forecast: `go run ./cmd/stateforecast` (Stage 49.18.7).

## How it was measured

`cmd/synthtenant` builds a deterministic synthetic tenant through the real engines - masters through the CSV import core, stock through the stock ledger, sales through the POS quote and checkout, returns through the two-person return chain, postings through the general ledger (Stage 47.9.1). With `-measure` it notes the transaction-id window of every business transaction, then sums every row the tenant's schema holds by the transaction that wrote it (`xmin`).

- **Heap bytes** are the row versions a transaction wrote (`pg_column_size`). Measured.
- **Factor** is the schema's whole on-disk size (tables, indexes, TOAST, free space) over its live row bytes. Measured, once per run.
- **Total** = heap x factor: the on-disk cost including indexes. Projected from the two measured numbers.
- A row updated later counts against the transaction that last wrote it (a customer's loyalty balance moves into the sale that earned it), and a dead version waiting for vacuum is not counted.

Reproduce on a scratch database (the tool refuses production and any database not named as a scratch one):

```
DATABASE_URL=postgres://.../erp_synth go run ./cmd/synthtenant -shape 10k -bills 1500 -seed 1 -measure
```

The shapes: `small` (60 bills), `10k`, `100k` and `1m` (bills; masters and stock scale with them). The run below is the `10k` shape's masters and stock - 2,000 items, 10 stores, 3 warehouses with 6,000 bins (85% empty), 200 suppliers, 5,000 customers - with 1,500 bills instead of 10,000. Run of 2026-10-28, seed 1, PostgreSQL 16.3 on the development machine; 33 minutes in all.

## Bytes per transaction (measured)

| Kind | Count | Rows written | Heap p50 | Heap p95 | Total p50 | Total p95 |
|---|---:|---:|---:|---:|---:|---:|
| sale | 1,500 | 17.8 | 5,188 B | 6,388 B | 11.2 KiB | 13.8 KiB |
| return | 80 | 15.9 | 5,366 B | 5,722 B | 11.6 KiB | 12.4 KiB |
| receipt | 13 | 2,884 | 1,110,516 B | 1,131,744 B | 2.3 MiB | 2.4 MiB |
| putaway | 900 | 8.0 | 2,490 B | 2,490 B | 5.4 KiB | 5.4 KiB |
| audit | 5,000 | 1 | 313 B | 313 B | 694 B | 694 B |
| month_end_posting | 480 | 2 | 416 B | 416 B | 922 B | 922 B |
| master:Item | 2,000 | 1 | 407 B | 407 B | 902 B | 902 B |
| master:Customer | 5,000 | 0.9 | 417 B | 419 B | 924 B | 929 B |
| master:Bin | 6,000 | 1 | 244 B | 244 B | 541 B | 541 B |
| master:Vendor | 200 | 1 | 296 B | 296 B | 656 B | 656 B |

- Factor **2.22** (66.3 MB on disk over the schema's live row bytes; 31.4 MB of it is indexes). A nearly empty tenant's factor is far higher - 7.14 on the `small` shape - because its fixed overhead dominates; an empty client schema is 5.3 MB.
- A **sale** is the bill, its stock-ledger lines, seven GL rows, the availability update and the outbox event. Mean heap 5,912 B (large bills pull it above the median).
- A **receipt** here is a 2,000-line opening receipt: **555 B heap, about 1.2 KiB on disk, per line**.
- Stock-ledger entries are the largest single record type: 29,249 of them hold 13.6 MB of the 24.2 MB `documents` heap.
- POS sales write no `audit_logs` row; the audit rows above are explicit events (5,000 price changes on the hot SKU, one per putaway and owner assignment).

## Growth by client size (projected)

From the measured means (sale 13.1 KB, return 12.1 KB, receipt line 1.2 KB, posting 0.9 KB on disk) and these assumed volumes: returns 6% of bills, 50 received lines per store per day, a month-end burst of postings.

| Profile | Stores | Bills / day | Per day | Per month | Per year | 8 years |
|---|---:|---:|---:|---:|---:|---:|
| Small | 2 | 100 | 1.5 MB | 45 MB | 0.55 GB | 4.4 GB |
| Medium (`10k` shape) | 10 | 1,000 | 14.5 MB | 434 MB | 5.3 GB | 42 GB |
| Large (`100k` shape) | 40 | 5,000 | 72 MB | 2.2 GB | 26 GB | 209 GB |

Nothing transactional is swept today, so these are also the database sizes after that long, plus the masters.

## Hot data and retention

Hot: the last 30 days of bills (`sales.return_window_days`, default 30 - a return reads its original bill), open documents, the current and previous financial year (GST returns, period close). Older rows are read by reports, audits and disputes only.

Swept on the leader server after their setting (the defaults the code ships; `0` on a setting is a hold):

| Setting | Default | What it bounds |
|---|---|---|
| `platform.report_export_retention_days` | 7 days | report export files |
| `platform.public_api_idempotency_retention_hours` | 24 hours | public API idempotency keys |
| `platform.public_api_log_retention_days` | 30 days | public API traffic log |
| `platform.job_runner_retention_days` | 30 days | finished background jobs |
| `platform.integration_log_retention_days` | 90 days | sent outbox events, integration and hook logs |
| `platform.error_log_retention_days` | 180 days | system error log |

Audit rows: hot for 365 days, then archived to an encrypted, compressed, signed file per checkpoint window (`audit_retention_policy`, Stage 47.7.6); deletion is off until a client decides it.

Kept for ever: every transaction (`documents`, including stock-ledger entries), `gl_postings`, and audit rows until archived. `inventory_availability` is bounded by SKUs x locations, not by time.

## Statutory retention

**[needs qualified review]** In India: GST records for 72 months from the due date of the year's annual return (CGST Act s.36); books of account for 8 years (Companies Act 2013 s.128(5)); income-tax books 6 years from the end of the assessment year (rule 6F). The planning figure is **8 financial years restorable**. For the Large profile that is about 209 GB if all of it stays online.

## Archive and restore

- Exists: the audit archive (above); a whole-database nightly dump; a per-client schema export; a client purge only after its retention window and with a backup reference (Stage 44).
- Not built: an archive tier for closed financial years of transactions. Until one exists every year stays in the live tables, and the 8-year figure above is the live database size.

## Backups (measured, then projected)

A custom-format `pg_dump` of the measured schema was **3.27 MB for 66.3 MB on disk (4.9%) and took 4 seconds** - indexes are not dumped and JSON compresses well. Synthetic names repeat, so real data will compress less; plan on 10%.

- Nightly backups kept 14 days on the server: about 14 x 10% = 1.4 x the database size of disk, plus one dump's size while it is encrypted.
- The off-site copy is locked for 35 days: 3.5 x the database size in the bucket.
- Large profile after one year (26 GB): about 2.6 GB a night, 37 GB on the server, 91 GB in the bucket.

## Peak temporary space (projected)

- **Additive migrations** (`ADD COLUMN IF NOT EXISTS`, nullable, no volatile default - the repo's rule) change metadata only: no extra space.
- **A new index** on a client's table needs the index's final size free plus sort spill up to the indexed data: plan for free space equal to that table's size. Migrations run client by client, so the peak is the largest single client's table (on the measured tenant `documents` is 47 MB, 23 MB of it indexes), not the sum.
- **A table rewrite** (`ALTER COLUMN TYPE`, `VACUUM FULL`) needs the whole table and its indexes again; the additive rule avoids it.
- **A restore** needs the full database size with every index rebuilt, plus its WAL.
- Not measured yet: WAL volume per transaction and checkpoint/archive space.
