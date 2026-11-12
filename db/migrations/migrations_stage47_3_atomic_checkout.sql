-- ---------------------------------------------------------------------------
-- Stage 47.3 - exactly-once, atomic sale/checkout (audit finding A-03).
--
-- What was actually wrong: handleCheckout's "idempotency guard" claimed the
-- POSCart row before running side effects, which stopped a duplicate cart
-- NUMBER being processed twice - but everything after that claim ran as five
-- independent, separately-committed transactions (inventory availability +
-- stock ledger, loyalty burn, revenue/COGS, GST, exempt reclass). A failure or
-- a process kill between any two of them left the sale half-posted: stock gone
-- with no revenue, or revenue booked with no COGS, and the cart marked Failed
-- with no way to tell which half had already committed. The cart claim also
-- protected only the cart id, not availability itself, so a retry under a NEW
-- cart number decremented stock again.
--
-- Two mechanisms close that, and this file is the storage for both:
--
--   1. command_idempotency - a tenant-scoped record of the COMMAND, claimed
--      before any mutation and completed inside the same transaction as the
--      mutation. A repeated key returns the original outcome verbatim; it can
--      never repeat the mutation, and it cannot be defeated by choosing a new
--      cart number, because the key is the caller's own idempotency key.
--
--   2. POSCart.payment_state - the Initiated/Authorized/Posted/Failed/Voided
--      taxonomy 47.3.3 requires, so a payment-provider round trip happens
--      BETWEEN transactions rather than inside one. No DB transaction is ever
--      held open across a network call.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS tenant_default.command_idempotency (
    -- The caller's key, namespaced by command so two unrelated commands
    -- cannot collide on a client that reuses a request id.
    idempotency_key VARCHAR(250) PRIMARY KEY,
    command         VARCHAR(100) NOT NULL,
    -- SHA-256 of the canonical request. A repeat of the SAME key with a
    -- DIFFERENT payload is a client bug (or an attack), not a retry, and must
    -- be refused rather than silently returning someone else's outcome.
    request_digest  VARCHAR(64)  NOT NULL,
    -- InProgress -> Completed | Failed. InProgress with an expired lease is
    -- recoverable (see ClaimCommand); Completed is terminal and replayable.
    status          VARCHAR(20)  NOT NULL DEFAULT 'InProgress',
    -- The exact response body the first successful call returned, replayed
    -- byte-for-byte to every duplicate. Storing the RESPONSE rather than
    -- recomputing it is what makes "returns the original outcome" literal.
    response        JSONB,
    -- Correlation of the request that owns the claim, so a stuck InProgress
    -- row can be traced to the request that abandoned it.
    correlation_id  VARCHAR(100),
    document_id     VARCHAR(100),
    claimed_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Claim lease. A process killed mid-checkout leaves InProgress behind
    -- forever otherwise, and the cashier could never retry that sale.
    lease_expires_at TIMESTAMP NOT NULL,
    completed_at    TIMESTAMP
);

-- The reconciliation and cleanup reads (47.3.6): by command and recency.
CREATE INDEX IF NOT EXISTS idx_command_idempotency_command
  ON tenant_default.command_idempotency (command, claimed_at DESC);
-- Recovering a stuck claim looks up InProgress rows past their lease.
CREATE INDEX IF NOT EXISTS idx_command_idempotency_stuck
  ON tenant_default.command_idempotency (lease_expires_at)
  WHERE status = 'InProgress';
CREATE INDEX IF NOT EXISTS idx_command_idempotency_document
  ON tenant_default.command_idempotency (document_id);

-- ---------------------------------------------------------------------------
-- 47.3.3 - the payment state machine, on the cart itself.
--
-- Deliberately doctype_fields on POSCart rather than a new table: the state
-- belongs to the sale, every existing reader of a cart already reads its data
-- JSONB, and the approval/replay paths that reload a cart get the state for
-- free with no join. A cart written before this stage has no payment_state at
-- all, which engines/pos_checkout.go reads as the legacy synchronous path -
-- so nothing about an existing tenant's completed sales changes.
--
--   Initiated  - cart claimed, stock reserved, nothing posted. The only state
--                in which a provider round trip may happen.
--   Authorized - the provider says the money is good; still nothing posted.
--   Posted     - the one transaction committed: stock, GL, GST, loyalty.
--   Failed     - terminal; the reservation is released and nothing is posted.
--   Voided     - an Authorized payment was reversed before posting.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('POSCart', 'payment_state', 'Payment State', 'Select', FALSE,
 'Initiated,Authorized,Posted,Failed,Voided', 60),
('POSCart', 'payment_reference', 'Payment Reference', 'Data', FALSE, NULL, 61),
('POSCart', 'idempotency_key', 'Idempotency Key', 'Data', FALSE, NULL, 62)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 47.3.2 - the availability lock's supporting index. FinalizePOSCheckout now
-- locks every line's inventory_availability row FOR UPDATE in a deterministic
-- SKU order inside one transaction (47.3.4); the primary key already covers
-- (sku, location_code), so no new index is needed for the lock itself. What
-- IS new is the reservation sweep for expired Initiated carts.
CREATE INDEX IF NOT EXISTS idx_poscart_payment_state
  ON tenant_default.documents ((data->>'payment_state'))
  WHERE doctype = 'POSCart' AND deleted_at IS NULL;
