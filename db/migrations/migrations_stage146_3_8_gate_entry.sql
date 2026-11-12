-- Stage 146.3.8 - inward gate-entry register.
-- A GateEntry is the security-desk record of a delivery arriving: vendor, PO, parcels, transporter,
-- LR no., vehicle (the photo is an attachment). A goods receipt can start from it (GRN.gate_entry_id).
-- GatePass is the outbound pass (it hangs off a Manifest), so this is a new record, not an extension.
-- Additive and re-runnable.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('GateEntry', 'Procurement', 'Transaction', 'procurement')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('GateEntry', 'code', 'Gate Entry No', 'Data', TRUE, NULL, 1),
('GateEntry', 'location_code', 'Location', 'Link', TRUE, 'Location', 2),
('GateEntry', 'vendor', 'Vendor', 'Link', TRUE, 'Vendor', 3),
('GateEntry', 'po_id', 'PO Reference', 'Link', FALSE, 'PurchaseOrder', 4),
('GateEntry', 'parcels', 'Parcels', 'Number', FALSE, NULL, 5),
('GateEntry', 'transporter', 'Transporter', 'Data', FALSE, NULL, 6),
('GateEntry', 'lr_no', 'LR / AWB No', 'Data', FALSE, NULL, 7),
('GateEntry', 'vehicle_number', 'Vehicle No', 'Data', FALSE, NULL, 8),
('GateEntry', 'remarks', 'Remarks', 'Data', FALSE, NULL, 9),
('GateEntry', 'grn_id', 'Goods Receipt', 'Link', FALSE, 'GRN', 10),
('GateEntry', 'status', 'Status', 'Select', TRUE, 'Open,GRN Made,Cancelled', 11),
('GRN', 'gate_entry_id', 'Gate Entry', 'Link', FALSE, 'GateEntry', 101)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'GateEntry', TRUE, TRUE, TRUE, TRUE),
('Super Admin', 'GateEntry', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'GateEntry', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'GateEntry', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- The number series GE (client schemas that already exist; the sync gives it to the rest).
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs');
    EXECUTE format($sql$
      INSERT INTO %1$I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store, year_format)
      SELECT 'GE', 'GE', '/', 5, 'ANNUAL', TRUE, TRUE, 'FY_COMPACT'
      WHERE NOT EXISTS (SELECT 1 FROM %1$I.prefix_configs WHERE doc_type = 'GE' OR UPPER(prefix) = 'GE')
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
