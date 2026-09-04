# Stage 47.0.2 — Sale/Checkout/Return Endpoint & Mutation Map

**Item:** [`docs/micro_checklist.md`](../micro_checklist.md) Stage 47, item **47.0.2** ("Inventory all active sale/checkout/return endpoints and every table/event they mutate. Name one authoritative command per event and explicitly deprecate parallel legacy paths in the design before implementation.")

**Status:** Research/design only. **No code was changed to produce this document.** Removing/deprecating any path listed below is explicitly out of scope here — that is **47.4.1**'s job, in a later session, after 47.2/47.3 land the replacement.

**Feeds:** 47.2 (server-authoritative quote/price), 47.3 (exactly-once atomic checkout), 47.4 (one atomic return/refund model). A developer picking up any of those three items should be able to start from this file instead of re-deriving the call graph.

**Source evidence:** [`ERP_DEEP_PERSONA_AUDIT_2026-09-01.md`](ERP_DEEP_PERSONA_AUDIT_2026-09-01.md) §3, findings A-02 (POS trusts the browser for price/cost), A-03 (checkout not atomic, retry can double-deduct), A-04 (legacy return is replayable and separates stock from finance). All three are re-confirmed below by reading the current code, with exact file:line references.

---

## 1. Endpoint inventory

Grepped `internal/server/routes.go` for every route matching `pos|checkout|sale|return|refund|fulfillment` (case-insensitive), then resolved each handler to its defining file.

| # | Method | Path | Handler | Handler file |
|---|--------|------|---------|---------------|
| 1 | POST | `/api/v1/checkout` | `handleCheckout` | `internal/server/handlers_pim_pos_finance.go:339` |
| 2 | POST | `/api/v1/approval/decide` | `handleDecideApproval` | `internal/server/handlers_pim_pos_finance.go:927` (calls `engines.FinalizePOSCheckout` at line 978 when a discount-gated `POSCart` is Approved — see §2.1) |
| 3 | POST | `/api/v1/pos/session/open` | `handlePOSSessionOpen` | `internal/server/handlers_pim_pos_finance.go:665` |
| 4 | POST | `/api/v1/pos/session/close` | `handlePOSSessionClose` | `internal/server/handlers_pim_pos_finance.go:695` |
| 5 | GET | `/api/v1/pos/session/current` | `handlePOSSessionCurrent` | `internal/server/handlers_pim_pos_finance.go` (read-only) |
| 6 | POST | `/api/v1/pos/offline-heartbeat` | `handlePOSOfflineHeartbeat` | `internal/server/handlers_pim_pos_finance.go:737` |
| 7 | POST | `/api/v1/pos/offers/preview` | `handlePOSOffersPreview` | `internal/server/handlers_pos_offers.go:18` (read-only preview; checkout re-evaluates independently — see routes.go:506-508 comment) |
| 8 | POST | **`/api/v1/fulfillment/return`** | `handleFulfillmentReturn` | `internal/server/handlers_operations.go:408` — **the legacy/POS in-store return path (A-04)** |
| 9 | POST | `/api/v1/fulfillment/task/transition` | `handleFulfillmentTaskTransition` | `internal/server/handlers_operations.go:373` — WMS pick/pack/dispatch task status, not a money/stock-of-record mutation itself; listed for completeness only, out of scope below |
| 10 | POST | **`/api/v1/returns`** | `handleCreateReturnRequest` | `internal/server/handlers_returns.go:14` — **the newer OMS `ReturnRequest` aggregate entry point (A-04's "one authoritative return aggregate" candidate)** |
| 11 | POST | `/api/v1/returns/{id}/approve` | `handleApproveReturnRequest` | `internal/server/handlers_returns.go:58` |
| 12 | POST | `/api/v1/returns/{id}/reject` | `handleRejectReturnRequest` | `internal/server/handlers_returns.go:78` |
| 13 | POST | `/api/v1/returns/{id}/receive` | `handleReceiveReturnRequest` | `internal/server/handlers_returns.go:102` |
| 14 | POST | `/api/v1/returns/{id}/qc` | `handleApplyReturnQC` | `internal/server/handlers_returns.go:125` |
| 15 | POST | `/api/v1/returns/{id}/reverse-pickup` | `handleScheduleReturnReversePickup` | `internal/server/handlers_returns.go:164` |
| 16 | POST | `/api/v1/refunds/{id}/approve` | `handleApproveRefundRequest` | `internal/server/handlers_returns.go:202` |
| 17 | POST | `/api/v1/refunds/{id}/reject` | `handleRejectRefundRequest` | `internal/server/handlers_returns.go:222` |
| 18 | POST | `/api/v1/refunds/{id}/process` | `handleProcessRefundRequest` | `internal/server/handlers_returns.go:249` |
| 19 | GET | `/api/v1/reports/sales-register` | `handleSalesRegisterReport` | read-only report, not a mutator; listed for completeness |
| 20 | POST | `/api/v1/finance/sales-invoice/{id}/post` / `/settle` | `handlePostSalesInvoice` / `handleSettleSalesInvoice` | B2B/invoice-based sale path, separate from POS checkout; not traced further here (out of A-02/A-03/A-04's scope, which is specifically the POS/cart path) |

**Frontend confirmation** (`public/app.js`): grepping for `fulfillment/return` and `/api/v1/returns` shows the POS "Process a Return" screen (`renderPOSReturnPanel`, `app.js:4866`) calls **only** `POST /api/v1/fulfillment/return` (`app.js:4984`). There is **no frontend caller anywhere in `app.js`** for `/api/v1/returns` or any of the `ReturnRequest`/`RefundRequest` routes (#10-18) — confirmed by an exact-string grep for `ReturnRequest` and `/api/v1/returns` across `app.js` returning zero matches. The only callers of `engines.CreateReturnRequest` in the whole repo are `handlers_returns.go` and test files (`engines/engines_test.go`, `engines/stage35_8_9_test.go`). This is the gap **Stage 47.4's acceptance text itself names**: "closes A-04 and Stage 35.9's stated missing management UI."

---

## 2. Per-handler mutation trace

### 2.1 `handleCheckout` (`POST /api/v1/checkout`) + `handleDecideApproval`'s discount-approval branch (`POST /api/v1/approval/decide`)

Both are thin request handlers around one shared engine function, **`engines.FinalizePOSCheckout`** (`engines/pos_checkout.go:17`) — this part of the codebase already follows the "one authoritative command" pattern the audit wants generalized. `handleCheckout` calls it directly when no discount-approval is required; `handleDecideApproval` calls the identical function (line 978) once a manager approves a `POSCart` that was parked in `Draft`/`Pending Approval` for exceeding the discount threshold. Full ordered mutation sequence is in §5 below (that's 47.3's scope). Summary of tables/side effects touched, across both entry points:

- `<schema>.documents` (doctype `POSCart`) — claimed via an `INSERT ... ON CONFLICT (id) DO UPDATE ... WHERE status = 'Failed'` idempotency gate (handlers_pim_pos_finance.go:549-556), then flipped `Processing`/`Draft` → `Paid` or `Failed`.
- `<schema>.inventory_availability` — decremented per line via `PostInventoryLedgerWithVoucher` (`engines/inventory.go:187`).
- `<schema>.documents` (doctype `StockLedgerEntry`) — one append-only ledger row per line, written **after** the availability transaction commits (see §5, this is the exact A-03 gap).
- `<schema>.documents` (doctype `LoyaltyLedgerEntry`, via `insertLoyaltyLedgerEntry`) — burn (`RedeemLoyaltyPoints`, `engines/loyalty.go:98`) and, on later failure, a compensating reversal (`ReverseLoyaltyRedemption:138`); separate earn entry (`EarnLoyaltyPoints:154`) after the sale is marked Paid.
- `<schema>.gl_postings` — up to 5 separate `PostDoubleEntry` calls (`engines/finance.go:106`): revenue, COGS (both inside `PostSalesFinanceBooking:439`), GST output-tax split (`PostSalesGSTBooking:480`), exempt/nil/zero-rated reclass (`PostExemptSalesReclass:523`, only when applicable).
- `<schema>.documents` (doctype `POSOfflineSyncVariance`) — best-effort, only when an offline-synced sale went negative (`recordOfflineSyncVariance`, `engines/pos_checkout.go:188`).
- **Event/outbox: none.** Grepped `PublishEvent`/`integration_event_outbox` in `pos_checkout.go` — zero matches. No `sale.completed` event exists today in any form (webhook subscription event catalog in `engines/webhook.go` also has no sale/checkout/return event name — grepped and confirmed zero matches).
- **Audit log (`audit_logs` table via `engines.LogAuditEvent`): NOT written by the normal checkout path.** Grepped `handlers_pim_pos_finance.go` for `LogAuditEvent` — it is called for `POS_SESSION` open/close (lines 689, 718) and for `APPROVAL_DECISION` (line 970, only on the discount-approval branch), but **never** for a plain (non-discount) completed sale. A normal cash sale leaves no append-only `audit_logs` row at all — only the mutable `documents.status` field on the `POSCart` row itself.

### 2.2 `handleFulfillmentReturn` (`POST /api/v1/fulfillment/return`) — legacy/POS in-store return

Handler: `internal/server/handlers_operations.go:408`. Delegates to **`engines.ProcessReturnAnywhere`** (`engines/fulfillment.go:297`). Full trace, in order:

1. `resolveOriginalSale` + `sumPriorReturns` (`fulfillment.go:55`) — validates return window (SALESR-0129) and remaining-quantity (SALESR-0130) **against only the `SalesReturn` doctype** (`SELECT ... WHERE doctype = 'SalesReturn' AND data->>'invoice_id' = $1`, `fulfillment.go:61`). **It does not look at the `ReturnRequest` doctype at all.**
2. Opens one DB transaction (`fulfillment.go:348`), loops items, and for each does an `INSERT ... ON CONFLICT (sku, location_code) DO UPDATE` against `<schema>.inventory_availability` incrementing `on_hand`/`available` (`fulfillment.go:415-424`). **No idempotency key of any kind on this increment.**
3. `tx.Commit()` (`fulfillment.go:428`) — stock is now permanently incremented.
4. **After** that commit, two separate `PostDoubleEntry` calls run outside any shared transaction (`fulfillment.go:442`, `:449`) — revenue reversal (debit 4100/credit 1100) and inventory/COGS reversal (debit 1200/credit 5100). The function's own comment (`fulfillment.go:434-439`) states there is **no per-call idempotency key** for these postings, by design, "out of scope for this pass."
5. Back in the handler (`handlers_operations.go:462-476`), a `SalesReturn` document is inserted with a **deterministic ID `RET-<originalOrderID>`** and the insert's error is explicitly discarded (`_, _ = db.DB.Exec(...)`, line 476).

**This is the exact replay mechanism A-04 describes**, confirmed line-by-line: a retried/duplicate call with the same `original_order_id` and the same partial-quantity items will (a) pass the SALESR-0130 check again because `sumPriorReturns` only sees the total quantity recorded in the *existing* `RET-<id>` document's `items` array — a value that a **second** call never gets to add to, because step 5's insert against the already-used deterministic ID fails on the primary key and is silently swallowed; (b) re-increment `inventory_availability` a second time (step 2, no idempotency guard); (c) re-post the GL reversal a second time (step 4, no idempotency guard); (d) still return HTTP 200 `"status": "refunded"` to the caller. The stale `RET-<id>` document never reflects the second return, so a third replay is checked against the same (still understated) "already returned" total and succeeds again. Nothing here requires a network retry specifically — a normal double-click or two concurrent browser tabs against the same original order produce the same result, since steps 2 and 4 hold no row lock across the whole operation (the `inventory_availability` UPSERT in step 2 is not `SELECT ... FOR UPDATE`-gated the way checkout's decrement path is at `inventory.go:250-253`).
6. **Event/outbox: none.** **Audit log: not written** — `handleFulfillmentReturn` and `ProcessReturnAnywhere` call `LogAuditEvent` zero times (confirmed by grep; `handlers_operations.go`'s only `LogAuditEvent` calls are for `ASSET_CAPITALIZE`/`ASSET_DISPOSE`/`EXPENSE_VERIFY`/`EXPENSE_PAY`/`PRODUCTION_*`, none in the return handler).

### 2.3 `handleCreateReturnRequest` → `handleApproveReturnRequest` → `handleReceiveReturnRequest` → `handleApplyReturnQC` → (`handleScheduleReturnReversePickup`) → `handleApproveRefundRequest` → `handleProcessRefundRequest` — OMS `ReturnRequest`/`RefundRequest` aggregate

Handlers: `internal/server/handlers_returns.go`. Engine: `engines/returns.go` (Stage 26.12.5, extended by 35.9.1/35.9.2). This is a real state machine, not a single instant call:

- **`CreateReturnRequest`** (`returns.go:198`) — validates window/quantity via `resolveOriginalSale` **and checks both `sumPriorReturns` (the legacy `SalesReturn` doctype) and `sumPriorReturnRequests` (this file's own `ReturnRequest` doctype, `returns.go:159`)** before admitting a new request. **This is the one-directional gap**: this path defends against the legacy path's prior returns, but §2.2 showed the legacy path does *not* defend against this path's prior returns — a `ReturnRequest` that has already been Approved/Received/QC'd (stock and GL already moved) does not stop a subsequent `POST /api/v1/fulfillment/return` for the same `original_order_id`/SKU/quantity from also succeeding. Inserts `<schema>.documents` doctype `ReturnRequest`, status `Requested`. Idempotent on `booking_id` for RTO (`returns.go:272-280`); **no idempotency key for the Customer Return branch** beyond the quantity check above. Calls `DispatchNotification` (`returns.go:342`) — a notification-template dispatch (`engines/notifications.go:70`), not the `integration_event_outbox`/webhook mechanism.
- **`ApproveReturnRequest`/`RejectReturnRequest`** — pure `documents` status transitions (`Requested` → `Approved`/`Rejected`), plus `DispatchNotification`.
- **`ReceiveReturnRequest`** — status transition only (`Approved`/`Pickup Scheduled` → `Received`). No stock movement yet.
- **`ApplyReturnQC`** (`returns.go:500`) — the actual stock/GL mutation point. One DB transaction (`returns.go:542`) that: assigns a disposition per SKU (`Sellable/Damaged/Repairable/Missing/Wrong-Item/Rejected`), increments `<schema>.inventory_availability` into the matching bucket (`available`/`damaged`/`qc_hold`) via `applyReturnedStockToBucket` (`returns.go:451`), optionally deducts exchange-SKU stock **in the same transaction** (`deductExchangeStock`, `returns.go:660`, which also writes a `StockLedgerEntry` with an explicit `idempotency_key`), updates the `ReturnRequest` document, and conditionally inserts a `RefundRequest` document — **all inside one transaction**, then commits. **After** that commit, two more `PostDoubleEntry` calls run separately (inventory-received reversal, exchange-leg reversal if applicable) — same "commit stock first, post GL after, outside the transaction" shape as the legacy path, just with an internal state-machine idempotency layer around it that the legacy path lacks.
- **`ApproveRefundRequest`/`RejectRefundRequest`** — status transitions on `RefundRequest`.
- **`ProcessRefundRequest`** (`returns.go:776`) — the money-movement step, gated on `RefundRequest.status == 'Approved'`. One `PostDoubleEntry` call (revenue reversal only — inventory/COGS already posted in `ApplyReturnQC`), then closes the parent `ReturnRequest` (`Closed`) and dispatches a `"Refund Processed"` notification.
- **`ScheduleReturnReversePickup`** — courier integration (`LogisticsBooking`/`CreateLogisticsBooking`/`AllocateCourierAWB`/`ScheduleCourierPickup`), Customer-Return-only, orthogonal to the money/stock mutation this document is mapping.
- **Audit log: not written anywhere in `returns.go`** — no `LogAuditEvent` call in the entire file (confirmed by grep against the earlier 67-file list: `engines/returns.go`/`internal/server/handlers_returns.go` are absent from it).

---

## 3. A-04 confirmed: named dual path

| | Legacy / POS in-store path | OMS `ReturnRequest` aggregate |
|---|---|---|
| Endpoint | `POST /api/v1/fulfillment/return` | `POST /api/v1/returns` + 7 follow-on state-transition endpoints |
| Handler | `handleFulfillmentReturn` (`internal/server/handlers_operations.go:408`) | `handleCreateReturnRequest` + 6 more (`internal/server/handlers_returns.go`) |
| Engine entry point | `engines.ProcessReturnAnywhere` (`engines/fulfillment.go:297`) | `engines.CreateReturnRequest`/`ApproveReturnRequest`/`ReceiveReturnRequest`/`ApplyReturnQC`/`ProcessRefundRequest` (`engines/returns.go`) |
| Wired to a UI? | **Yes** — POS "Process a Return" panel, `public/app.js:4866` (`renderPOSReturnPanel`), submits to this endpoint at `app.js:4984` | **No** — zero references in `app.js`; API-only today, exercised only by Go tests |
| Price/cost source | **Client-submitted** `sale_price`/`cost_price` in the request body, used as-is (`handlers_operations.go:418-423`) | **Server-resolved** from the original `POSCart`/`SalesOrderLine` (`resolveOriginalSaleLinePrices`/`resolveSalesOrderLinePrices`, `returns.go:93`/`:130`) — never trusts the caller |
| Idempotency | None on the stock increment or the GL post; a deterministic-but-swallowed-on-conflict `SalesReturn` document ID is the only guard, and it fails open (§2.2) | Quantity check spans both doctypes on create; `ApplyReturnQC`'s stock/GL/RefundRequest-creation step is one transaction; RTO branch is idempotent on `booking_id` |
| Stock vs. finance ordering | Stock committed (own tx) → finance posted after, no shared tx, no idempotency on either | Stock committed (own tx, alongside RefundRequest creation) → finance posted after, no shared tx — same ordering weakness, but wrapped in an approval/QC state machine that prevents blind replay of the *same* request |
| Cross-path awareness | **Does not check the other path's returns at all** (`sumPriorReturns` only reads `SalesReturn`) | **Does check the legacy path's returns** (`sumPriorReturnRequests` + `sumPriorReturns` both queried) |

**Confirmed finding for the design:** both `handleFulfillmentReturn` (legacy) and the `handleCreateReturnRequest`→…→`handleProcessRefundRequest` chain (OMS aggregate) are live, routed, and can independently mutate `inventory_availability` and `gl_postings` for the same original order, with only one-directional awareness of each other. This is precisely the "parallel legacy path" 47.0.2 asks the design to name and 47.4.1 is scheduled to retire.

---

## 4. Event → endpoint → mutation → authoritative-command recommendation

| Event | Endpoint(s) that can currently produce it | Tables/events mutated | **Recommended single authoritative command** | **Path(s) to mark deprecated in the 47.4 design (do not remove yet)** |
|---|---|---|---|---|
| `sale.completed` | `POST /api/v1/checkout` (direct); `POST /api/v1/approval/decide` (discount-approved `POSCart`, indirect) | `documents` (`POSCart`, `StockLedgerEntry`, `LoyaltyLedgerEntry`, `POSOfflineSyncVariance`), `inventory_availability`, `gl_postings`; no outbox event, no `audit_logs` row | `engines.FinalizePOSCheckout` — **already the single engine-level choke point for both entry points; no change of engine needed**, only its internal atomicity (47.3) and its price/cost trust boundary (47.2) | None — this event has no parallel implementation to deprecate. (47.3 should still decide whether the *two entry points* — direct vs. via approval-decide — collapse into one idempotency scope, since each currently claims the cart independently.) |
| `return.processed` (customer return, walk-in/instant) | `POST /api/v1/fulfillment/return` | `inventory_availability` (increment), `gl_postings` (2 reversal postings), `documents` (`SalesReturn`, ID `RET-<order>`, duplicate-insert error discarded) | **New `ReturnRequest`-based command** built out from `engines.CreateReturnRequest` → `ApplyReturnQC` (collapsed into one atomic instant-return mode for the walk-in case, per 47.4.5's "receipt return" workflow) — this is the aggregate 47.4.1 says to route POS/OMS/API through | **`handleFulfillmentReturn` / `engines.ProcessReturnAnywhere`** — mark deprecated; the POS "Process a Return" screen (`public/app.js` `renderPOSReturnPanel`) is the UI caller to repoint at the new command in 47.4.6 |
| `return.requested` / `return.approved` / `return.received` / `return.qc_completed` | `POST /api/v1/returns`, `.../approve`, `.../reject`, `.../receive`, `.../qc` | `documents` (`ReturnRequest`, `RefundRequest`, `StockLedgerEntry` for exchanges), `inventory_availability` (bucketed), `gl_postings` (inventory/COGS reversal + exchange leg) | `engines.CreateReturnRequest`/`ApproveReturnRequest`/`ReceiveReturnRequest`/`ApplyReturnQC` — **keep and harden**, this is the shape 47.4 should generalize (add: shared idempotency key across the whole request, atomic stock+GL per 47.3's pattern, cross-check against `SalesReturn` retained only until the legacy path is actually removed) | None to deprecate here — this chain is the survivor. Note for 47.4.2/47.4.3: it still needs its own idempotency key (today only the RTO `booking_id` branch has one) and its stock-then-GL ordering still needs the same atomicity fix as checkout. |
| `refund.processed` | `POST /api/v1/refunds/{id}/process` (OMS path); implicitly, the GL reversal inside `handleFulfillmentReturn`/`ProcessReturnAnywhere` (legacy path has no separate "refund" concept — it is folded into the same instant call) | `gl_postings` (revenue reversal), `documents` (`RefundRequest` → `Processed`, parent `ReturnRequest` → `Closed`) | `engines.ProcessRefundRequest` | The legacy path's inline refund-as-part-of-return has no separate identity to deprecate on its own — it goes away with `ProcessReturnAnywhere` above. |
| `return.reverse_pickup_scheduled` | `POST /api/v1/returns/{id}/reverse-pickup` | `documents` (`LogisticsBooking`, `ReturnRequest.status = 'Pickup Scheduled'`) | `engines.ScheduleReturnReversePickup` | None — Customer-Return-only, orthogonal, no legacy equivalent exists. |

**Design note carried forward, not resolved here:** none of the above five events are published to `integration_event_outbox` today (`engines.PublishEvent`, `engines/outbox.go:15`, is used only by PIM/dashboard/report/channel-order/CleverTap/Unicommerce integrations — grepped, zero hits in `pos_checkout.go`, `fulfillment.go`, or `returns.go`), and none of the sale/return/refund handlers call `engines.LogAuditEvent`. Whoever designs the single authoritative command in 47.2-47.4 should decide explicitly whether `sale.completed`/`return.*`/`refund.processed` become real outbox events (Stage 38.4's webhook-subscription machinery already exists and currently has no sale/return event to subscribe to) and whether they get an `audit_logs` row — both are currently silent gaps, not stated design decisions.

---

## 5. Checkout mutation sequence, in order (for 47.3)

This is the exact current sequence `engines.FinalizePOSCheckout` (`engines/pos_checkout.go:17`) runs once `handleCheckout` (or `handleDecideApproval`'s approval branch) calls it. Each numbered step is a **separate** DB round trip; steps marked "own tx" open and commit/rollback their own `*sql.Tx` independently of every other step. **There is no outer transaction wrapping any of this.** This is what A-03 means by "not atomic" and is the map 47.3.2 needs to turn into one transaction.

1. **Read** the `POSCart` document (`documents` table, doctype `POSCart`) for its stored items/GST breakdown/loyalty intent. (Already claimed `Processing`/`Draft` by `handleCheckout`'s own idempotency INSERT before this function is even called — see §2.1.)
2. **`PostInventoryLedgerWithVoucher`** (`engines/inventory.go:187`) — **own tx**:
   a. For each line, `SELECT ... FOR UPDATE` the `inventory_availability` row (only when the delta is negative), floor-checks against `allowNegative`.
   b. `INSERT ... ON CONFLICT DO UPDATE` on `inventory_availability` (on_hand/available decrement).
   c. **`tx.Commit()`** — availability change is now permanent and visible to every other transaction. **No idempotency key guards this commit itself.**
3. **`WriteStockLedgerEntry`** (`engines/inventory.go:81`), once per line, called **after** step 2's commit, each as its **own separate `Exec`** (no shared tx, no lock): checks `idempotency_key` (`VoucherType:VoucherID:Location:SKU[:BatchNo]`) against existing `StockLedgerEntry` documents first — **this is the only idempotency check in the whole sequence, and it is not able to undo step 2 if it fires.** A write failure here is logged as `WARN` only and does not fail the sale (`pos_checkout.go:311-313`).
4. **`RedeemLoyaltyPoints`** (`engines/loyalty.go:98`), only if `redeem_points > 0` — re-checks balance, then inserts a `LoyaltyLedgerEntry` (`Burn`) document. Failure here calls `markFailed()` (flips `POSCart.status = 'Failed'`) and returns — **steps 2-3's mutations are NOT rolled back or compensated at this point**, they simply stay committed against a cart the caller is told failed.
5. If loyalty discount exceeds the sale total: **`ReverseLoyaltyRedemption`** (compensating `Earn` entry) — best-effort, logs on failure, does not fail the sale.
6. **`PostSalesFinanceBooking`** (`engines/finance.go:439`) — internally **two separate `PostDoubleEntry` calls**, each its **own tx** against `gl_postings`: (a) revenue debit/credit, (b) COGS debit/credit. Failure here calls `failAndRefundPoints` — reverses the loyalty burn from step 4 (a compensating ledger row, not a delete) and marks the cart `Failed`. **Steps 2-3 (stock) are still not reversed.**
7. **`PostSalesGSTBooking`** (`engines/finance.go:480`) — one more `PostDoubleEntry` call, **own tx**. Same failure handling as step 6 (loyalty reversed, stock not).
8. **`PostExemptSalesReclass`** (`engines/finance.go:523`) — one more `PostDoubleEntry` call when the cart has exempt/nil/zero-rated lines, **own tx**. Same failure handling.
9. **`UPDATE documents SET status = 'Paid'`** on the `POSCart` row — a standalone `Exec`, errors discarded (`_, _ = db.DB.Exec(...)`, `pos_checkout.go:164`).
10. **`EarnLoyaltyPoints`** (`engines/loyalty.go:154`), only if `customer_id` set — inserts an `Earn` `LoyaltyLedgerEntry`, best-effort (logs, does not fail the sale), plus a conditional tier-recompute (`RecomputeLoyaltyTier`).

**Retry/failure surface this creates (concrete, matching A-03):**
- A failure at step 4, 6, 7, or 8 leaves the `POSCart` in status `Failed` with stock already decremented (steps 2-3 already committed). `handleCheckout`'s claim query (`handlers_pim_pos_finance.go:549-556`) explicitly allows re-claiming a `Failed` cart (`WHERE status = 'Failed'`) for a retry with the *same* `cart_number`. A retry re-enters this whole sequence: step 2 decrements `inventory_availability` **a second time**; step 3's `StockLedgerEntry` idempotency key is unchanged (same `voucher_id`/location/sku), so the **ledger row is correctly suppressed as a duplicate**, but the **availability decrement is not** — this is the exact "retry can decrement availability again while suppressing only the duplicate ledger row" mechanism the audit names.
- Steps 6-8 (GL/GST/exempt-reclass) each independently commit; a failure partway through step 6-8 can leave revenue posted without GST posted, or vice versa, with no automatic reconciliation.

**What 47.3.2 needs to build:** one PostgreSQL transaction (or a formally designed reservation/saga per 47.3's own acceptance text) containing steps 2, 3, 4/5, 6, 7, 8, and 9, keyed by one tenant-scoped idempotency record inserted *before* step 2 runs (not after, as `WriteStockLedgerEntry`'s key currently is) — per 47.3.1's own wording, "idempotency protects availability itself, not only the ledger row." Step 10 (loyalty earn) can legitimately stay outside the transaction/best-effort as it is today, since 47.3's own note treats it as purely additive.

---

## 6. Summary for a future session

- **19 routes** inventoried (grep terms: pos/checkout/sale/return/refund/fulfillment against `internal/server/routes.go`), of which **10** are load-bearing sale/return/refund mutators traced in full (§2); the rest are read-only, WMS-task-status, or B2B-invoice paths out of this item's scope.
- **A-04 dual-path confirmed by name:** `handleFulfillmentReturn` (`engines.ProcessReturnAnywhere`) is the live, UI-wired legacy path; `handleCreateReturnRequest` → … → `handleProcessRefundRequest` (`engines.CreateReturnRequest`/`ApplyReturnQC`/`ProcessRefundRequest`) is the newer, more correct but **UI-orphaned** OMS aggregate. They are cross-aware in only one direction (§3 table), so a return can currently be double-processed by mixing both paths against the same order — a sharper version of A-04's own single-path replay finding.
- **A-03 mutation sequence mapped step-by-step** (§5) with exact file:line references and tx boundaries, ready for 47.3 to collapse into one transaction.
- Two gaps found beyond what A-02/A-03/A-04 stated, worth carrying into the 47.2-47.4 design: (1) none of the sale/return/refund paths write to `audit_logs` today (only `POS_SESSION` open/close and `APPROVAL_DECISION` do); (2) none of them publish to `integration_event_outbox`, so Stage 38.4's webhook-subscription mechanism has no sale/return event to offer integrators yet.
