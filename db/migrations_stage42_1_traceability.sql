-- ---------------------------------------------------------------------------
-- Stage 42.1 - Traceability foundation (42.1.1 / 42.1.2 / 42.1.3 / 42.1.6).
--
-- Closes the first of the two foundational holes Stage 42's plan names: before
-- this file there was no batch/lot concept anywhere in the tree - no batch_no,
-- no expiry, no shelf life - which is why FEFO, expiry blocking and recall
-- traceability were all unbuildable, and why this WMS could not be sold into
-- food / pharma / cosmetics / electronics.
--
-- Everything here is additive. A tenant that never sets an Item's
-- tracking_mode is byte-identical to what it is today: tracking_mode defaults
-- to 'None', every gate in engines/traceability.go is a no-op for a None item,
-- and bin_stock/inventory_availability are not altered at all.
--
-- The one design decision worth recording, because it is the one a reader will
-- question: batch stock is a NEW table (bin_stock_batch), not a batch_no column
-- added to bin_stock. Adding the column would have forced batch_no into
-- bin_stock's PRIMARY KEY (two batches of one SKU in one bin must not collide),
-- and that means DROP CONSTRAINT bin_stock_pkey on a table holding live stock
-- on the droplet, plus rewriting every existing
-- `ON CONFLICT (bin_code, sku, condition)` in engines/wms.go and
-- wms_putaway_ext.go. That is a destructive rework of an existing table, which
-- this repo's first principle forbids. The separate table is the precedent
-- 26.5.4 already set with bin_stock_lpn: bin_stock is a breakdown of
-- inventory_availability, and bin_stock_batch is a *further* breakdown of
-- bin_stock, never a second source of truth for the bin's own total.
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- 42.1.1 - Item tracking flags.
--
-- tracking_mode is the master switch every other item in this phase reads. It
-- is deliberately Select-with-a-default rather than a boolean pair, because
-- "Batch and Serial" is a real fourth state (a serialised pharmaceutical still
-- belongs to a lot) and two booleans would let a tenant express it only by
-- accident.
--
-- The three shelf-life numbers are all optional and all in days:
--   shelf_life_days                 - used to DERIVE an expiry date at receipt
--                                     when the receiving clerk knows the
--                                     manufacture date but not the expiry.
--   min_shelf_life_on_receipt_days  - reject stock arriving too close to its
--                                     expiry (a supplier dumping short-dated
--                                     goods is the classic case).
--   min_shelf_life_on_pick_days     - do not allocate stock that will expire
--                                     before the customer can reasonably use
--                                     it. This is the gate that matters most;
--                                     it is what 42.1.6 enforces.
--
-- display_order 13+ continues after Stage 26.4's family/parent_product_code/
-- variant_option_values (10-12) so the Item form's existing field order is
-- untouched.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'tracking_mode', 'Traceability', 'Select', FALSE,
 'None,Batch,Serial,Batch and Serial', 13),
('Item', 'shelf_life_days', 'Shelf Life (days)', 'Number', FALSE, NULL, 14),
('Item', 'min_shelf_life_on_receipt_days', 'Min Shelf Life on Receipt (days)', 'Number', FALSE, NULL, 15),
('Item', 'min_shelf_life_on_pick_days', 'Min Shelf Life on Pick (days)', 'Number', FALSE, NULL, 16)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 42.1.2 - the Batch master.
--
-- module_key 'master_data' is not a fresh choice: db/migrations_stage14a_
-- modules.sql already maps `WHEN 'Batch' THEN 'master_data'` and the module's
-- own description already reads "Brand/Style/Color/Size/Model/Batch masters".
-- The taxonomy anticipated this doctype in Stage 14; only the doctype itself
-- was missing. master_data is a core, always-enabled module, so no moduleGate
-- can hide a batch from a tenant that has stock of it.
--
-- The global-unique document-id gotcha applies here and is the reason batch_no
-- is a plain field rather than the document id: documents.id is the PRIMARY KEY
-- across every doctype, but a batch number is only ever unique WITHIN an item -
-- two suppliers both shipping "LOT-001" of different SKUs is normal, and using
-- batch_no as the id would make the second one unsaveable. The id stays a
-- generated UUID and (item, batch_no) uniqueness is enforced in
-- engines/master_data_validation.go instead, where it can produce a real
-- message instead of a primary-key violation.
--
-- `item` is deliberately Data holding the SKU CODE, not a Link to Item. The
-- generic Link check (META-0198, engines/doctype.go) resolves its value against
-- documents.id, and an Item's document id is not its code in this tree (tests
-- and the live data both carry ids like 'ITEM-<sku>'), so a Link here would
-- reject the very value a warehouse types off a carton. Every other stock-side
-- reference to an item - bin_stock.sku, inventory_availability.sku,
-- CycleCountLine.sku - is the code as plain Data for exactly this reason, and
-- existence is checked against data->>'code' in
-- engines/master_data_validation.go instead, where the message can say so.
-- `vendor` is Data for a related reason: it is provenance stamped from the GRN
-- that received the lot, not a field anyone picks.
--
-- `attributes` is Infor's "lottable" concept (§16 Outbound Lottable
-- Validation): free-form JSON key/values a customer contract can then be
-- validated against at allocation time (42.1.7), e.g. {"country_of_origin":
-- "IN", "grade": "A"}. Modelling it as JSON in a Data field is the same
-- convention GRN.received_items and BOM.components already use, so the generic
-- JSON line editor renders it with no new frontend code.
--
-- status carries the quarantine states 42.1.6 moves a batch through. Expired is
-- set by the near-expiry sweep, not typed by a human; Quarantined and Blocked
-- are manual holds. Only Active batches are allocatable.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('Batch', 'Master Data', 'master_data', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Batch', 'batch_no', 'Batch / Lot No', 'Data', TRUE, NULL, 1),
('Batch', 'item', 'Item Code (SKU)', 'Data', TRUE, NULL, 2),
('Batch', 'mfg_date', 'Manufacture Date', 'Date', FALSE, NULL, 3),
('Batch', 'expiry_date', 'Expiry Date', 'Date', FALSE, NULL, 4),
('Batch', 'supplier_batch', 'Supplier Batch No', 'Data', FALSE, NULL, 5),
('Batch', 'vendor', 'Supplier', 'Data', FALSE, NULL, 6),
('Batch', 'received_qty', 'Originally Received Qty', 'Number', FALSE, NULL, 7),
('Batch', 'attributes', 'Lottable Attributes JSON', 'Data', FALSE, NULL, 8),
('Batch', 'notes', 'Notes', 'Data', FALSE, NULL, 9),
('Batch', 'status', 'Status', 'Select', TRUE,
 'Active,Quarantined,Expired,Blocked,Consumed', 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Store Manager can create a batch by hand (a receipt that arrives without an
-- ASN, a re-labelled lot) but cannot delete one: a Batch is the evidence base
-- for a recall, so it is retired by setting status = Consumed/Blocked, never by
-- being removed. HR/Admin keeps delete for the mistyped-row case, the same
-- split every other master in this tree uses.
INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'Batch', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'Batch', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'Batch', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 42.1.3 - batch on the stock record.
--
-- Same shape as bin_stock (bin + sku + condition + qty at a location), one
-- column wider. batch_no is part of the primary key so two lots of the same SKU
-- in the same bin are tracked independently rather than summing into a single
-- untraceable number - which is the entire point.
--
-- location_code is carried (denormalised from bin_stock, exactly as bin_stock
-- itself denormalises it) so FEFO allocation can filter a location's batch
-- stock in one index-backed query without joining through Bin documents.
--
-- The invariant engines/traceability.go enforces, and the reason this table can
-- never drift into a second source of truth:
--     SUM(bin_stock_batch.qty) for (bin, sku, condition)
--         <= bin_stock.qty for that same (bin, sku, condition)
-- i.e. a bin's batch breakdown can be incomplete (stock received before this
-- Stage has no batch, and a None-tracked item never will) but can never claim
-- more than the bin holds. AssignToLPN (26.5.4) enforces the identical
-- invariant for containers; this is the same check against the same parent row.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant_default.bin_stock_batch (
    bin_code VARCHAR(100) NOT NULL,
    sku VARCHAR(100) NOT NULL,
    batch_no VARCHAR(100) NOT NULL,
    condition VARCHAR(20) NOT NULL DEFAULT 'Good',
    location_code VARCHAR(100) NOT NULL,
    qty INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (bin_code, sku, condition, batch_no),
    CONSTRAINT bin_stock_batch_condition_check CHECK (condition IN ('Good', 'Damaged', 'QC-Hold', 'RTV'))
);

-- The FEFO allocation query's access path: "every Good-condition batch of this
-- SKU at this location, earliest expiry first". The expiry itself lives on the
-- Batch document, so this index covers the filter half and the join to
-- documents does the ordering half.
CREATE INDEX IF NOT EXISTS idx_bin_stock_batch_sku_location
    ON tenant_default.bin_stock_batch (sku, location_code, condition);

-- Recall traceability, backward direction: "which bins hold anything from this
-- batch, right now".
CREATE INDEX IF NOT EXISTS idx_bin_stock_batch_batch
    ON tenant_default.bin_stock_batch (batch_no, sku);

-- The Batch document lookup that every allocation and every expiry gate makes:
-- (item, batch_no) -> expiry/status. Without this, FEFO does a sequential scan
-- of the whole documents table once per SKU per wave.
CREATE INDEX IF NOT EXISTS idx_documents_batch_item_no
    ON tenant_default.documents ((data->>'item'), (data->>'batch_no'))
    WHERE doctype = 'Batch';

-- ---------------------------------------------------------------------------
-- The stock ledger learns about batches (42.1.3, forward recall direction).
--
-- Additive field registration only, exactly as 26.10.1 did for
-- idempotency_key/from_location_id/...: the column is a JSON key inside
-- documents.data, so this row only teaches the generic doc viewer and the
-- report drill-down to render it. The writing is Go-side.
--
-- This is what makes "which orders shipped units of batch X" answerable: every
-- movement of batch-tracked stock writes a StockLedgerEntry carrying batch_no,
-- and the ledger is already append-only.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('StockLedgerEntry', 'batch_no', 'Batch / Lot No', 'Data', FALSE, NULL, 14)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- BatchPutaway/BatchConsume are real voucher types written by
-- engines/traceability.go; widening the Select keeps the generic doc form's
-- dropdown honest rather than showing a value it claims is invalid. Matched on
-- the exact prior value so re-running after a later stage widens it again is a
-- no-op instead of a silent revert.
UPDATE tenant_default.doctype_fields
SET options = 'GRN,POSInvoice,StockTransfer,TransferOrder,Putaway,BinReplenishment,ConditionChange,CycleCount,StockAdjustment,ProductionOrder,BatchPutaway,BatchConsume'
WHERE doctype_name = 'StockLedgerEntry' AND fieldname = 'voucher_type'
  AND options = 'GRN,POSInvoice,StockTransfer,TransferOrder,Putaway,BinReplenishment,ConditionChange,CycleCount,StockAdjustment,ProductionOrder';

-- ---------------------------------------------------------------------------
-- 42.1.6 - the quarantine transitions a Batch can make.
--
-- Stage 29.8's StatusTransitionRule master is the single place this repo
-- declares what a document's status may become, so a new master with a real
-- lifecycle registers there rather than growing a second rule set. Batch is not
-- picked up by 29.8's auto-generating block (that one covers masters whose
-- status options are exactly 'Active,Inactive'), so its edges are listed
-- explicitly here, in the same shape 29.8 uses for the transactional
-- lifecycles.
--
-- Active -> Expired is the only machine-driven edge (42.1.6's near-expiry sweep
-- sets it); everything else is a human decision. Coming back OUT of a hold
-- requires a reason code, because "who released this quarantined lot and why"
-- is the first question a recall audit asks. Consumed is terminal - a batch
-- that has been fully picked and shipped never becomes Active again; a new
-- receipt gets a new batch row.
--
-- Note that doctype_meta.strict_status_transitions is deliberately left FALSE
-- for Batch, matching 29.8's documented opt-in-strict posture. These rules are
-- seeded and enforceable, and strict can be switched on for Batch once this
-- phase's own flows have run against live data - flipping it now would mean
-- betting that this list is complete on day one, which is exactly the bet 29.8
-- decided not to make.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
SELECT
  'STR-Batch-' || v.from_status || '-' || v.to_status,
  'StatusTransitionRule',
  jsonb_build_object(
    'code',                 'STR-Batch-' || v.from_status || '-' || v.to_status,
    'entity',               'Batch',
    'from_status',          v.from_status,
    'to_status',            v.to_status,
    'allowed',              'Yes',
    'requires_reason_code', v.needs_reason,
    'status',               'Active'
  ),
  'Active',
  'system'
FROM (VALUES
  ('Active',      'Quarantined', 'No'),
  ('Active',      'Blocked',     'No'),
  ('Active',      'Expired',     'No'),
  ('Active',      'Consumed',    'No'),
  ('Quarantined', 'Active',      'Yes'),
  ('Quarantined', 'Blocked',     'No'),
  ('Quarantined', 'Expired',     'No'),
  ('Quarantined', 'Consumed',    'Yes'),
  ('Blocked',     'Active',      'Yes'),
  ('Blocked',     'Expired',     'No'),
  ('Blocked',     'Consumed',    'Yes'),
  ('Expired',     'Blocked',     'No'),
  ('Expired',     'Consumed',    'No')
) AS v(from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;
