---
doc_id: DOC-PROD-136
title: System design lessons for the ERP
type: reference
status: draft
owner: engineering-owner
approvers: [product-owner]
audience: [maintainers, ai-sessions]
applies_to: engines, internal/server, deploy, public
authority: source
confidentiality: internal
last_verified: 2026-11-27
review_by: 2027-02-26
supersedes: none
superseded_by: none
---

# System design lessons for the ERP

Ask 290 (2026-11-26). The owner shared three learning sites and asked what is useful. This
note keeps the useful lessons. Each lesson is checked against what the ERP already does.
The notes are our own words. The sites are not open source, so none of their text, art or
code is copied here.

## The three sources

| Site | What it is | Useful to us |
|---|---|---|
| [designed-to-scale.vercel.app](https://designed-to-scale.vercel.app) | Interactive system design course by Dilum Sanjaya: about 45 lessons of about 10 steps each. Topics: scaling, caching, storage, transactions, queues, reliability, security, deployments, and three worked designs (URL shortener, news feed, chat). | **High.** Most lessons map directly onto ERP risks. The lessons are below. |
| [protocol-explorer-alpha.vercel.app](https://protocol-explorer-alpha.vercel.app) | Same author. About 40 networking protocols, each shown packet by packet: DNS, TCP, UDP, TLS, HTTP/3 (QUIC), DHCP, ICMP and traceroute, email, SSH, WebSocket, Wi-Fi and WPA3, captive portals, proxies, VPNs, NTP, DNSSEC, RADIUS, SMB, Modbus and more. | **Low.** Good background for store network faults (see below). Nothing to build. |
| [bytesizedalgorithms.com](https://bytesizedalgorithms.com) | Paid algorithms course (86 lessons, $5). Its terms forbid reuse. Only the home page is free. | **Low.** Shows a good teaching format, described below. |

Both free sites are client-side apps. Their lesson text sits in the script bundle, which is how
it was read.

## Lessons the ERP already follows

These are recorded so a later session does not rebuild them.

| Lesson | Where the ERP does it |
|---|---|
| A retried request must not charge or post twice: use an idempotency key | `Idempotency-Key` in `internal/server/command_keys.go` and `engines/public_api_runtime.go` |
| Save a record and its event in one transaction (transactional outbox) | `engines/outbox.go` |
| Hold stock with an expiry instead of holding a lock while someone pays | `inventory_reservation.expires_at` (`engines/inventory.go`, `engines/fulfillment.go`); a worker frees expired piece holds (`StartPieceHoldWorker`) |
| Take row locks in a fixed order so two sales cannot deadlock | Lines sorted by SKU before locking (item 47.3.4, `engines/inventory.go`); `engines/pim_bulk.go` |
| Failed jobs retry, then go to a dead-letter list for a person | `engines/jobrunner.go` |
| A 429 says when to come back | `Retry-After` from `RateLimiter.RetryAfter` in `internal/server/middleware.go` |
| Limit each tenant, not only each user | `tenantConcurrencyLimiter` in `internal/server/middleware.go`, with a heavy class for reports |
| A tenant can never read another tenant's rows | One schema per tenant through `search_path`. This is stronger than row-level security, so no RLS is needed. |
| Long reports must not freeze the app | `statement_timeout` in `db/query_cancel.go` and `engines/query_explain.go` |
| Refuse a write based on stale data | Stock adjustment snapshot check (`INVENT-1312`, HTTP 409) |
| Two people editing the same record: the second gets a conflict, not a silent overwrite | `expected_version` (item 24.10, `internal/server/handlers_core_doc_engine.go`). Since item 290.3 the browser sends it on every edit of a record it read, from one place in `app.js`; an API caller that leaves it out keeps last-write-wins. |
| Database changes are additive so old and new code can run side by side (expand and contract) | Constitution Article 5 |
| Deploying is separate from switching a feature on | Feature flags in `engines/saas.go` and `engines/approval.go` |
| Put the rule in the database too, as a constraint | `CHECK (qty >= 0)` and the balance checks in `db/migrations/` |
| Certificates renew themselves | Caddy (ACME) in `deploy/` |

## Gaps found

| Gap | Why it matters | Item |
|---|---|---|
| **Backups are a nightly `pg_dump` at 02:00 only.** There is no write-ahead-log archive. | A bad write, such as an update with no `WHERE`, is copied everywhere at once. Without the log archive the only way back is last night's dump, so up to a day of sales, receipts and postings is lost. With an archive the database can be restored to the minute before the mistake. | 290.2 (relates to 50.15). Built 2026-11-27: `deploy/pitr.sh`, drilled locally; on in production once the owner runs `install_pitr.sh` |
| **Most edit screens do not send `expected_version`.** | Two users editing the same item, vendor or PO: the later save silently wins. The server already detects it. The screens just don't ask. | 290.3. Built 2026-11-27: one shared save path sends it |

## Lessons to apply in future work

A checklist for new features. Each line is a rule worth checking in review.

- **Money, stock or ledger command:** give it an idempotency key. Never call a payment
  provider or wait on a person while a transaction or row lock is open. Treat a timeout as
  "unknown", not "failed". Ask again or retry with the same key. Never undo blindly.
- **Anything that waits on a person or an outside service:** hold, then confirm. Write a hold
  with an expiry, do the slow part outside any transaction, then confirm. A cleanup job releases
  expired holds.
- **Multi-row locks:** lock in a fixed order (by id or SKU).
- **Business rules that must never break:** add a database constraint as well as the code check
  (unique, foreign key, `CHECK`).
- **New background job:** make it safe to run twice. Count attempts and send poison jobs to the
  dead-letter list. Alert when that list grows.
- **Events that must follow a save:** write them through the outbox in the same transaction.
- **Order matters only per key:** process one item's or one account's events in order. Different
  keys can run in parallel.
- **New cache:** delete on write instead of overwriting. Always set a TTL. Add jitter so many keys
  don't expire at once. For a hot key, let one request refill it while the others wait
  ("single-flight"). Cache "not found" briefly too. The ERP's existing in-memory caches were not
  checked against this.
- **New index:** check it is selective. Use a partial index for a small slice, such as
  `WHERE status = 'pending'`. Use an expression index for `LOWER(email)`. Check with `EXPLAIN`.
- **New endpoint that updates a record:** allow-list the fields it may change. Never bind the
  whole request body (mass assignment). Answer 404, not 403, for a record the caller may not see.
- **A service acting for a user:** use the user's permissions, not its own (confused deputy).
- **Alerts:** alert on what users feel, such as slow or failing checkouts, not on CPU. Put
  deploys on the timeline, because "what changed?" is the first question in an incident.
- **Tests:** wait for a condition, never a fixed time. Give each test its own data. A flaky test
  trains people to ignore red.
- **IDs:** gaps in numbering are harmless. Sequential numbers in a URL show volume and can be
  walked, so the authorization check on every request is what protects them. Statutory invoice
  numbers stay sequential by law.
- **Sizing:** plan for the peak (a festival sale), not the average. Daily users times actions
  per user, divided by 86,400 seconds, then multiplied by the peak factor, gives requests per
  second.

## Network background for store faults

From Protocol Explorer. Useful when a store says "the POS can't reach the server":

- **Captive portal:** hotel or mall Wi-Fi intercepts the first request and shows a sign-in page.
  The till looks connected, but every API call returns the portal page.
- **DNS:** the till must turn the server's name into an address first. A bad or slow resolver
  looks like the whole app being down.
- **NTP:** clocks drift. A till with a wrong clock stamps wrong times and can fail TLS
  certificate checks. See also the timestamp notes for item 132.19.
- **Reverse proxy:** Caddy ends TLS and forwards plain HTTP to the app. A Caddy fault looks like
  an app fault.
- **WebSocket:** an HTTP request asks to upgrade, then the connection stays open. PrintBridge
  uses this to reach the local printer agent.

## The teaching format

Both courses show each idea as a short run of steps. Each step has:

- a short title;
- two or three sentences;
- one callout: an insight, a tip or a warning;
- a list of what changed;
- a few numbers, such as hit rate or queue depth;
- an animated diagram of the same state.

Each lesson opens with a symptom ("users say the shop feels slow"), works through cases
("Case: the cache stampede") and ends with a recap. This would suit "How it works" pages in
Help for the core flows, such as PO to receipt to bill. It could be built in vanilla JS and
SVG, with no framework. Item 290.4 built it: *How buying works* and *How a sale works* under Getting Started.
