---
title: Epic before/after
type: record
status: archived
owner: qa-owner
authority: historical
confidentiality: internal
captured_at: 2026-11-10T17:50:19Z
---

# Epic before/after: baseline-2026-11-10 -> after-erp-5c-c-2026-11-10

Before: scratch commit 5b4932f, before the journey lab (unstripped binary) (2026-11-10T17:48:47Z, commit 5b4932f59a). After: erp-5c group c: journey lab, soak and epic ledger added (unstripped binary) (2026-11-10T17:50:19Z, commit 5b4932f59a).

| Metric | Unit | Before | After | Change | % | Note |
|---|---|---:|---:|---:|---:|---|
| api.soak_p95_ms_last_quarter | ms | - | - | - | - | not in the before entry |
| backup.seconds_to_verified_restore | seconds | - | 140.50 | - | - | no -journeys evidence given |
| backup.size_bytes | bytes | - | 843165 | - | - | no -journeys evidence given |
| browser.boot_bytes_and_cpu | bytes | - | - | - | - | needs a real browser; comes from the browser suite (47.9.4), not from this tool |
| budget.application_log_files_bytes | bytes | - | - | - | - | directory does not exist in this environment |
| budget.backup_artifacts_bytes | bytes | - | - | - | - | directory does not exist in this environment |
| budget.binary_stripped_bytes | bytes | 45506048 | 45502464 | -3584 | -0.0% |  |
| budget.cold_core_gzip_bytes | bytes | 184314 | 184314 | +0 | +0.0% |  |
| budget.embedded_kb_raw_bytes | bytes | 1705064 | 1705064 | +0 | +0.0% |  |
| budget.initial_js_gzip_bytes | bytes | 116451 | 116451 | +0 | +0.0% |  |
| budget.initial_js_plus_first_screen_gzip_bytes | bytes | 144735 | 144735 | +0 | +0.0% |  |
| budget.kb_largest_topic_raw_bytes | bytes | 103594 | 103594 | +0 | +0.0% |  |
| budget.kb_search_index_raw_bytes | bytes | 225267 | 225267 | +0 | +0.0% |  |
| budget.postgres_database_bytes | bytes | - | - | - | - | DATABASE_URL not supplied |
| db.audit_rows_per_1k_tx | rows per 1k writes | - | 2444.44 | - | - | no -journeys evidence given |
| db.heap_bytes_per_1k_tx | bytes per 1k writes | - | 1820444.44 | - | - | relation sizes grow in 8 KB pages, so a short run is coarse |
| db.index_bytes_per_1k_tx | bytes per 1k writes | - | 1166222.22 | - | - | relation sizes grow in 8 KB pages, so a short run is coarse |
| db.soak_projected_24h_growth | bytes | - | 759066277 | - | - | not in the before entry |
| db.wal_bytes_per_1k_tx | bytes per 1k writes | - | 6168241111.11 | - | - | WAL position is cluster-wide, so on a shared Postgres it also counts other databases' writes; trust it only from a private cluster |
| deps.go_modules | modules | 2 | 2 | +0 | +0.0% |  |
| jobs.outbox_rows_per_1k_tx | rows per 1k writes | - | 0 | - | - | no -journeys evidence given |
| memory.server_peak_rss_kb | KB | - | 48956 | - | - | not in the before entry |
| memory.server_rss_after_journeys_kb | KB | - | 47596 | - | - | no -journeys evidence given |
| memory.soak_rss_peak_kb | KB | - | 77396 | - | - | not in the before entry |
| memory.soak_rss_start_kb | KB | - | 39220 | - | - | not in the before entry |
| static.public_bytes | bytes | 7188141 | 7188141 | +0 | +0.0% |  |
| static.public_files | files | 168 | 168 | +0 | +0.0% |  |
| static.public_gzip_bytes | bytes | 4094521 | 4094521 | +0 | +0.0% |  |
| static.server_binary_bytes | bytes | 45506048 | 45502464 | -3584 | -0.0% |  |
| support.cmd_tools | tools | 21 | 23 | +2 | +9.5% |  |
| support.guide_and_kb_pages | pages | 71 | 71 | +0 | +0.0% |  |
| support.queue_categories | categories | - | - | - | - | the product has no support queue, so there is nothing to count |
| support.routes_with_permission | routes | 896 | 896 | +0 | +0.0% |  |
| support.runbooks | files | 10 | 11 | +1 | +10.0% |  |
