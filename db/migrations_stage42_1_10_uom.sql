-- ---------------------------------------------------------------------------
-- Stage 42.1.10 - UOM conversion (Infor §15).
--
-- Before this file there was no unit-of-measure concept anywhere in the
-- tree - every qty in engines/db is implicitly "one each," which is why
-- eaches/case/pallet picking, real cartonization, catch weight and
-- per-unit-type 3PL billing were all unbuildable. This closes the second
-- half of that gap (batch/serial was the first, 42.1.1-42.1.9).
--
-- Two masters, deliberately this shape:
--
--   UOM             - the reference list of valid unit codes (EA, CASE,
--                     PALLET, ...). Exists so UOMConversion has something
--                     real to validate from_uom/to_uom against, the same
--                     role HoldCode/ReasonCode play for their own domains.
--   UOMConversion   - (item, from_uom, to_uom, factor): "1 from_uom of THIS
--                     item equals `factor` to_uom." item is mandatory and
--                     Data (SKU code, not a Link - same reason Batch.item
--                     is Data: a UOM factor is genuinely per-item (one
--                     supplier's case is 12, another's is 24), so there is
--                     no tenant-wide default to fall back to, and the
--                     generic Link check would resolve against
--                     documents.id, not the SKU a person types.
--
-- No `stock_uom` added to Item: a direct (item, from_uom, to_uom) edge is
-- sufficient for every consumer this phase wires up (cartonization, pick
-- UoM display, 3PL billing) and adding a base-UOM field on Item now would be
-- a field nothing reads yet - exactly the premature-abstraction this repo's
-- guidelines warn against. If a real multi-hop conversion graph is ever
-- needed (EA -> CASE -> PALLET chained), that is a deliberate later addition,
-- not a guess made here.
--
-- Deliberately NOT wired into pricing in this phase (carried over from the
-- plan's own scope note) - a price list keyed by UOM is a distinct feature
-- with its own approval/versioning questions, not a side effect of a
-- warehouse unit-conversion table.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('UOM', 'Master Data', 'master_data', 'Master'),
('UOMConversion', 'Master Data', 'master_data', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('UOM', 'code', 'UOM Code', 'Data', TRUE, NULL, 1),
('UOM', 'description', 'Description', 'Data', FALSE, NULL, 2),
('UOM', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('UOMConversion', 'item', 'Item Code (SKU)', 'Data', TRUE, NULL, 1),
('UOMConversion', 'from_uom', 'From UOM', 'Data', TRUE, NULL, 2),
('UOMConversion', 'to_uom', 'To UOM', 'Data', TRUE, NULL, 3),
('UOMConversion', 'factor', 'Factor (1 From UOM = this many To UOM)', 'Number', TRUE, NULL, 4),
('UOMConversion', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Same split every constraint/reference master in this Stage uses: Store
-- Manager configures it, HR/Admin can delete a mistyped row, Cashier is
-- read-only.
INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'UOM', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'UOM', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'UOM', TRUE, FALSE, FALSE, FALSE),
('HR/Admin',       'UOMConversion', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'UOMConversion', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'UOMConversion', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 3PL billing units: StorageBillingRate (26.5.15) gets two optional columns.
-- Blank on every rate that exists today, which keeps GetStorageBillingReport
-- byte-identical for them - only a rate that names BOTH an item and a
-- billing_uom switches from the whole-location each-count to a per-item,
-- UOM-converted one (engines/wms_3pl_billing.go).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('StorageBillingRate', 'item', 'Item Code (SKU, optional - bill this item only)', 'Data', FALSE, NULL, 10),
('StorageBillingRate', 'billing_uom', 'Billing UOM (optional, requires Item)', 'Data', FALSE, NULL, 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
