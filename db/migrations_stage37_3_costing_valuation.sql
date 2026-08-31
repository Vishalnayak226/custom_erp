-- ---------------------------------------------------------------------------
-- Stage 37.3: Costing & valuation, incl. landed cost allocation.
--
-- Audit before building this (see engines/costing.go's own header comment
-- for the full citation trail): this codebase had NO costing method at all.
-- StockLedgerEntry/bin_stock/inventory_availability track quantity only.
-- "COGS" posted at POS checkout (PostSalesFinanceBooking, Dr 5100/Cr 1200)
-- used whatever cost_price the POS terminal's client JSON happened to send -
-- never verified against anything. And PostGRNFinanceBooking (the intended
-- Dr 1200/Cr 2100 GRN-receipt posting) was dead code with zero callers, which
-- means NOTHING has ever credited 2100 GRN Suspense in this codebase's
-- history - only PayVendorInvoice's debit exists, monotonically driving it
-- further into an incorrect balance for every tenant that has ever paid a
-- vendor invoice. This migration's schema, plus engines/costing.go's wiring,
-- closes both gaps.
--
-- Design: a single company-wide (not per-location) moving-weighted-average
-- unit cost per item - the standard Ind AS 2 / AS 2 acceptable method,
-- avoiding FIFO/LIFO layer-tracking this system has no infrastructure for.
-- item_cost stores CUMULATIVE received qty/value, not a live "remaining
-- pool": the average only needs to change on RECEIPT (a GRN or a landed-cost
-- top-up), never on issue, so nothing here needs to intercept this
-- codebase's many stock-out paths (sales, transfers, returns, disassembly,
-- wastage...) to stay correct - a real "current on-hand value" figure is
-- computed at report time instead, by multiplying the real, already-correct
-- inventory_availability.on_hand against this table's rate (see
-- engines/costing.go's GetInventoryValuation).
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS tenant_default.item_cost (
    item_code VARCHAR(100) PRIMARY KEY,
    cumulative_qty_received NUMERIC(18,4) NOT NULL DEFAULT 0,
    cumulative_value_received_paise BIGINT NOT NULL DEFAULT 0,
    avg_unit_cost_paise BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Two new GL accounts: 1710/2110 continue the codebase's per-stage numbering
-- (37.2 used 1700/2500); 2110 sits beside 2100 GRN Suspense as the landed-
-- cost equivalent - a charge whose vendor bill (freight/customs/insurance)
-- has not necessarily been matched yet, same posture GRN Suspense already
-- has for the goods themselves.
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('2110', 'Landed Cost Clearing', 'Liability')
ON CONFLICT (account_code) DO NOTHING;

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('LandedCostVoucher', 'Finance', 'finance', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- 37.3.2: landed cost allocation. charge_lines is a JSONTable, the same
-- "only ever read as part of its parent document" convention every other
-- line-item JSONTable in this codebase already uses (JournalVoucher.lines,
-- PIMExportTemplate.column_mappings, ...). Applying is one-shot (Draft ->
-- Applied, refused a second time) - engines/costing.go's ApplyLandedCostVoucher.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('LandedCostVoucher', 'code', 'Voucher Code', 'Data', TRUE, NULL, 1),
('LandedCostVoucher', 'grn_reference', 'GRN Reference', 'Link', TRUE, 'GRN', 2),
('LandedCostVoucher', 'charge_lines', 'Charges', 'JSONTable', TRUE,
 '[{"key":"charge_type","label":"Charge Type","type":"select","options":"Freight,Customs,Duty,Insurance,Other","required":true},
   {"key":"amount","label":"Amount","type":"number","required":true}]', 3),
('LandedCostVoucher', 'status', 'Status', 'Select', TRUE, 'Draft,Applied', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'LandedCostVoucher', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'LandedCostVoucher', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- Stage 35-37 migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'CREATE TABLE IF NOT EXISTS %I.item_cost (
         item_code VARCHAR(100) PRIMARY KEY,
         cumulative_qty_received NUMERIC(18,4) NOT NULL DEFAULT 0,
         cumulative_value_received_paise BIGINT NOT NULL DEFAULT 0,
         avg_unit_cost_paise BIGINT NOT NULL DEFAULT 0,
         updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
       )', schema_rec.schema_name
    );
    EXECUTE format(
      'INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES '
      '(''2110'', ''Landed Cost Clearing'', ''Liability'') '
      'ON CONFLICT (account_code) DO NOTHING',
      schema_rec.schema_name
    );

    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = 'LandedCostVoucher'
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module, module_key = EXCLUDED.module_key, document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'LandedCostVoucher'
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label, fieldtype = EXCLUDED.fieldtype, mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options, display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'LandedCostVoucher'
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read, allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update, allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
