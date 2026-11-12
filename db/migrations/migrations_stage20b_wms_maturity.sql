-- Stage 20 Track B.2: WMS Maturity (20.16-20.23).
-- Additive-only, matches migrations_stage20a_pos_maturity.sql's pattern.

-- 20.16: Bin master - warehouse/location + zone/aisle/rack/bin code + capacity.
-- Pure generic Master-doctype registration (doctype_meta/fields/permissions
-- only), same pattern as POSProfile in Stage 20a.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('Bin', 'Inventory', 'Master', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Bin', 'bin_code', 'Bin Code', 'Data', TRUE, NULL, 1),
('Bin', 'location', 'Location', 'Link', TRUE, 'Location', 2),
('Bin', 'zone', 'Zone', 'Data', FALSE, NULL, 3),
('Bin', 'aisle', 'Aisle', 'Data', FALSE, NULL, 4),
('Bin', 'rack', 'Rack', 'Data', FALSE, NULL, 5),
('Bin', 'capacity', 'Capacity', 'Number', FALSE, NULL, 6),
('Bin', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Bin', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Bin', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 20.17/20.23: bin-level stock read model, separate from the existing
-- location-level tenant_default.inventory_availability (which stays the
-- source of truth for sellable/available quantity - this table is a finer-
-- grained breakdown of that same on-hand stock by bin AND by condition).
-- condition is part of the primary key so Good/Damaged/QC-Hold/RTV
-- quantities of the same SKU in the same bin are tracked independently
-- rather than overwriting each other.
CREATE TABLE IF NOT EXISTS tenant_default.bin_stock (
    bin_code VARCHAR(100) NOT NULL,
    sku VARCHAR(100) NOT NULL,
    location_code VARCHAR(100) NOT NULL,
    condition VARCHAR(20) NOT NULL DEFAULT 'Good',
    qty INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (bin_code, sku, condition),
    CONSTRAINT bin_stock_condition_check CHECK (condition IN ('Good', 'Damaged', 'QC-Hold', 'RTV'))
);
CREATE INDEX IF NOT EXISTS idx_bin_stock_sku_location ON tenant_default.bin_stock (sku, location_code);

-- 20.20/20.21/20.22: cycle count lines. One document per SKU being counted
-- (not a nested-array parent doc) specifically so the existing generic
-- Stage 3.3 bulk-upload engine (BulkImportCSV, one CSV row -> one document)
-- can import counted quantities directly with zero new import code -
-- satisfies 20.21 "Physical stock upload" as a side effect of this doctype
-- shape, not a separate feature. count_session is a free-text grouping key
-- (e.g. a date+location string) a warehouse team agrees on before counting,
-- not a separate parent doctype - keeps this additive and avoids a second
-- new document type just to group rows.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('CycleCountLine', 'Inventory', 'Transaction', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CycleCountLine', 'count_session', 'Count Session', 'Data', TRUE, NULL, 1),
('CycleCountLine', 'location', 'Location', 'Data', TRUE, NULL, 2),
('CycleCountLine', 'bin', 'Bin (optional)', 'Data', FALSE, NULL, 3),
('CycleCountLine', 'sku', 'SKU', 'Data', TRUE, NULL, 4),
('CycleCountLine', 'counted_qty', 'Counted Qty', 'Number', TRUE, NULL, 5),
('CycleCountLine', 'system_qty', 'System Qty (filled on reconcile)', 'Number', FALSE, NULL, 6),
('CycleCountLine', 'variance', 'Variance (filled on reconcile)', 'Number', FALSE, NULL, 7),
('CycleCountLine', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Posted', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Bulk-CSV creation (Stage 3.3 engine) needs allow_create for whichever role
-- runs the physical count upload; reconcile/post are handler-only actions
-- (engines/wms.go), same "no direct generic-write for the state-transition
-- half" pattern as POSSession in Stage 20a - allow_update stays FALSE so a
-- counted line's system_qty/variance/status can't be hand-edited to skip
-- the approval gate via the generic doc API.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'CycleCountLine', TRUE, TRUE, FALSE, FALSE),
('Store Manager', 'CycleCountLine', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 20.19: TransferOrder gains an optional Packed status between Approved and
-- Dispatched (see engines/transfer_orders.go's PackTransferOrder) - extends
-- the existing options list rather than a new field, matching that this is
-- an optional, additive step on top of Stage 17.6's already-shipped
-- Approved -> Dispatched flow, not a breaking change to it.
UPDATE tenant_default.doctype_fields
SET options = 'Draft,Approved,Packed,Dispatched,Received'
WHERE doctype_name = 'TransferOrder' AND fieldname = 'status' AND options = 'Draft,Approved,Dispatched,Received';

-- 20.22: variance-approval, reusing engines/approval.go exactly as Stage
-- 20.10 did for POS discounts. "amount" here is the absolute variance
-- quantity (engines/wms.go stores it under extractAmount's "variance_qty"
-- key, see engines/approval.go) - any non-zero variance requires a Store
-- Manager (or HR/Admin) decision before the adjustment posts to inventory.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('CycleCountLine', 0, NULL, 'Store Manager')
ON CONFLICT (doctype, min_amount) DO NOTHING;
