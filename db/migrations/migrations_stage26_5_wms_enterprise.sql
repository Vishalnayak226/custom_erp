-- Stage 26.5: WMS Enterprise Maturity Sprint (26.5.1-26.5.10).
-- Additive-only, matches migrations_stage20b_wms_maturity.sql's pattern -
-- extends Stage 20 Track B.2's Bin/putaway/pick/pack/cycle-count engines.
-- bin_stock stays a breakdown of inventory_availability, never a second
-- source of truth (Stage 20.17's own precedent, restated in
-- docs/specs/wms_master_blueprint_reference.md).

-- ============================================================
-- 26.5.1: ASN (Advance Shipment Notice) - captured before a GRN references
-- it. `ASN` already existed as a bare stub in the original db/migration.sql
-- (module 'Inbound', fields asn_number/po_number/status/location, status
-- options 'Expected,Received,Cancelled') but was never wired to any engine
-- or screen before this Stage - confirmed via a repo-wide search, zero Go
-- references to the doctype existed. Per this repo's "reuse the existing
-- doctype, don't build a parallel third way" rule, this section is additive
-- fields on top of that stub (asn_number/po_number/status/location are left
-- exactly as they were, including the existing 3-state status vocabulary -
-- Expected/Received/Cancelled already covers everything an ASN's lifecycle
-- needs) rather than a competing code/status pair. expected_items mirrors
-- GRN's own received_items convention (a single Data field holding JSON, not
-- a nested doctype) so the GRN Workbench can read one the same way it
-- already reads a PO's items.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ASN', 'po_id', 'PO Reference (Link)', 'Link', FALSE, 'PurchaseOrder', 5),
('ASN', 'vendor', 'Vendor', 'Link', FALSE, 'Vendor', 6),
('ASN', 'carrier', 'Carrier', 'Data', FALSE, NULL, 7),
('ASN', 'tracking_number', 'Tracking Number', 'Data', FALSE, NULL, 8),
('ASN', 'expected_date', 'Expected Date', 'Date', FALSE, NULL, 9),
('ASN', 'expected_items', 'Expected Items JSON', 'Data', TRUE, NULL, 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Store Manager', 'ASN', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- GRN gains an optional back-reference to the ASN it was received against -
-- additive, existing GRNs (and any GRN never created from an ASN) are
-- unaffected since the field is not mandatory.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('GRN', 'asn_id', 'ASN Reference (optional)', 'Link', FALSE, 'ASN', 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ============================================================
-- 26.5.2: QC sampling on GRN. grnReceivedLine (engines/transactional_
-- validation.go) already carried accepted_qty/rejected_qty for the
-- GOODSR-0089/0090 validation checks - this adds damaged_qty as a genuine
-- third bucket (accept/reject/damage, not just accept/reject) alongside it.
-- Both are JSON keys inside GRN's existing received_items field, so no new
-- column is needed here; this section exists only to note the field-shape
-- extension for whoever next reads this migration looking for it.
-- ============================================================

-- ============================================================
-- 26.5.3: Cross-dock/flow-through putaway. No new schema - CrossDockPutaway
-- (engines/wms_putaway_ext.go) reuses tenant_default.bin_stock exactly as
-- PutawayToBin does, just against a synthetic per-location staging bin code
-- instead of a real Bin document, so the cross-docked qty still shows up on
-- GenerateBinPickList like any other Good-condition bin stock.
-- ============================================================

-- ============================================================
-- 26.5.4: LPN/carton/pallet grouping on top of bin_stock - additive. LPN is
-- a lightweight master (the container's own identity/status); bin_stock_lpn
-- is a *further* breakdown of bin_stock the same way bin_stock is a
-- breakdown of inventory_availability - never a second source of truth for
-- a bin's total qty, only for which container within that bin holds it.
-- ============================================================
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('LPN', 'Inventory', 'Master', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('LPN', 'lpn_code', 'LPN Code', 'Data', TRUE, NULL, 1),
('LPN', 'container_type', 'Container Type', 'Select', TRUE, 'Carton,Pallet', 2),
('LPN', 'status', 'Status', 'Select', TRUE, 'Open,Closed,Shipped', 3),
('LPN', 'notes', 'Notes', 'Data', FALSE, NULL, 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'LPN', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'LPN', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

CREATE TABLE IF NOT EXISTS tenant_default.bin_stock_lpn (
    lpn_code VARCHAR(100) NOT NULL,
    bin_code VARCHAR(100) NOT NULL,
    sku VARCHAR(100) NOT NULL,
    condition VARCHAR(20) NOT NULL DEFAULT 'Good',
    qty INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (lpn_code, bin_code, sku, condition)
);
CREATE INDEX IF NOT EXISTS idx_bin_stock_lpn_bin_sku ON tenant_default.bin_stock_lpn (bin_code, sku, condition);

-- ============================================================
-- 26.5.5: Bin-to-bin replenishment min/max triggers. bin_type lets a Bin be
-- optionally tagged as a Reserve location the replenishment suggester
-- prefers to draw from first; unset (existing Bins) means "no preference,"
-- so this is backward compatible with every Bin created before this Stage.
-- ============================================================
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Bin', 'bin_type', 'Bin Type (optional)', 'Select', FALSE, 'PickFace,Reserve', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('BinReplenishmentRule', 'Inventory', 'Master', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('BinReplenishmentRule', 'bin_code', 'Bin Code', 'Link', TRUE, 'Bin', 1),
('BinReplenishmentRule', 'sku', 'SKU', 'Link', TRUE, 'Item', 2),
('BinReplenishmentRule', 'min_qty', 'Min Qty', 'Number', TRUE, NULL, 3),
('BinReplenishmentRule', 'max_qty', 'Max Qty', 'Number', TRUE, NULL, 4),
('BinReplenishmentRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'BinReplenishmentRule', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'BinReplenishmentRule', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- ============================================================
-- 26.5.6: Wave/batch pick-list grouping. wave_id is an additive JSON key on
-- the existing FulfillmentTask (no column change - same "additive JSON key"
-- convention engines/fulfillment_pickpack.go's picked_qty/packed_qty/
-- short_qty already use), registered here purely so it's visible/filterable
-- in FulfillmentTask's generic table.
-- ============================================================
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('FulfillmentTask', 'wave_id', 'Wave ID (optional)', 'Data', FALSE, NULL, 20)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ============================================================
-- 26.5.7: Short-pick handling - already fully covered by Stage 26.12.3's
-- ShortPickLine (engines/fulfillment_pickpack.go, mandatory ReasonCode,
-- blocks pack completion on an unresolved shortfall). Stage 26.5's own
-- addition is only the wave-level shortfall visibility surfaced by
-- GenerateWavePickList's WaveOrderAllocation.Shortfall field - no schema
-- change needed for that, it's computed at read time.
-- ============================================================

-- ============================================================
-- 26.5.8: Cartonization at pack step - extends Stage 20.19's
-- PackTransferOrder (which already accepts a free-form boxes array) with a
-- suggestion engine. CartonType is the only new schema: the box capacity
-- catalogue the suggester packs against. A deliberately simple qty-capacity
-- model (not 3D dimensional bin-packing) per this repo's lightweight-first
-- rule.
-- ============================================================
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('CartonType', 'Inventory', 'Master', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CartonType', 'code', 'Carton Code', 'Data', TRUE, NULL, 1),
('CartonType', 'name', 'Carton Name', 'Data', TRUE, NULL, 2),
('CartonType', 'max_qty_capacity', 'Max Unit Capacity', 'Number', TRUE, NULL, 3),
('CartonType', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'CartonType', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'CartonType', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- ============================================================
-- 26.5.9: ABC cycle-count planner - a pure report over existing
-- CycleCountLine/inventory_availability/POSCart data (Stage 10's
-- CalculateSalesVelocity), same "read-only suggestion, no auto-created
-- documents" precedent GetReplenishmentSuggestions already set. No new
-- schema.
-- ============================================================

-- ============================================================
-- 26.5.10: Blind-count + recount workflow + variance root-cause codes.
-- recount_of links a recount line back to the original it re-counts (a
-- fresh Draft line - counted_qty/system_qty/variance never carried over, so
-- the second counter is blind to both the first count and the system qty).
-- variance_reason_code is set via a dedicated handler action (same
-- "handler-only, allow_update stays FALSE" convention the rest of this
-- doctype already uses for system_qty/variance/status) before a non-zero-
-- variance line is allowed to post.
-- ============================================================
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CycleCountLine', 'variance_reason_code', 'Variance Reason Code (set on reconcile)', 'Data', FALSE, NULL, 9),
('CycleCountLine', 'recount_of', 'Recount Of (line ID)', 'Data', FALSE, NULL, 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

UPDATE tenant_default.doctype_fields
SET options = options || ',Recount Requested'
WHERE doctype_name = 'CycleCountLine' AND fieldname = 'status'
  AND options NOT LIKE '%Recount Requested%';

-- ReasonCode.category gains 'Cycle Count Variance' - appended rather than a
-- hardcoded full replacement (unlike migrations_stage26_12_3_pick_pack.sql's
-- literal SET) so this migration is correct regardless of whether that
-- concurrent Stage 26.12.3 migration has run yet on a given database: an
-- append is safe either way, a full-string replace is only safe if this file
-- happens to run after it.
UPDATE tenant_default.doctype_fields
SET options = options || ',Cycle Count Variance'
WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
  AND options NOT LIKE '%Cycle Count Variance%';
