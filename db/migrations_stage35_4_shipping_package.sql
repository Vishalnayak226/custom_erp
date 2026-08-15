-- Stage 35.4: the missing link in the outbound document chain.
--
-- Today the chain runs FulfillmentTask -> LogisticsBooking and stops. There is
-- no object representing "the box", so there is nothing to split before
-- invoicing, nothing to invoice per-parcel, and nothing for a gate pass to
-- reference. Uniware's model puts a shipping package between the pack task and
-- the courier booking, and that is what this migration adds.
--
-- Everything here is additive. No existing doctype loses a field or a status
-- option, and every pre-35.4 LogisticsBooking keeps working: its new
-- shipping_package_id is simply NULL, and the ordering guard below treats a
-- booking with no package as a legacy booking and lets it through rather than
-- refusing to label stock that shipped fine yesterday.

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ShippingPackage', 'OMS', 'oms', 'Transaction'),
('GatePass', 'OMS', 'oms', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- ShippingPackage.
--
-- items is a JSONTable of {sku, qty} rather than a child doctype: a package's
-- contents are only ever read as a whole (invoice it, label it, split it), and
-- SalesOrderLine already exists as the per-line object anyone needs to query.
-- Adding a second per-line doctype would give the same order two competing
-- line tables.
--
-- The status lifecycle is deliberately short. Draft is the only mutable state;
-- Invoiced freezes contents (35.4.3's ordering rule needs an unambiguous "the
-- invoice is out" marker); Shipped follows handover; Cancelled is the exit.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ShippingPackage', 'code', 'Package ID', 'Data', TRUE, NULL, 1),
('ShippingPackage', 'fulfillment_task_id', 'Fulfillment Task', 'Link', TRUE, 'FulfillmentTask', 2),
('ShippingPackage', 'order_id', 'Order', 'Link', FALSE, 'SalesOrder', 3),
('ShippingPackage', 'location_code', 'Location', 'Data', FALSE, NULL, 4),
('ShippingPackage', 'items', 'Contents', 'JSONTable', FALSE,
 '[{"key":"sku","label":"SKU","type":"data","required":true},{"key":"qty","label":"Qty","type":"number","required":true}]', 5),
('ShippingPackage', 'weight_kg', 'Weight (kg)', 'Number', FALSE, NULL, 6),
('ShippingPackage', 'length_cm', 'Length (cm)', 'Number', FALSE, NULL, 7),
('ShippingPackage', 'width_cm', 'Width (cm)', 'Number', FALSE, NULL, 8),
('ShippingPackage', 'height_cm', 'Height (cm)', 'Number', FALSE, NULL, 9),
('ShippingPackage', 'package_type', 'Package Type', 'Data', FALSE, NULL, 10),
('ShippingPackage', 'sales_invoice_id', 'Sales Invoice', 'Link', FALSE, 'SalesInvoice', 11),
('ShippingPackage', 'split_from', 'Split From', 'Link', FALSE, 'ShippingPackage', 12),
('ShippingPackage', 'status', 'Status', 'Select', TRUE, 'Draft,Invoiced,Shipped,Cancelled', 13)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- GatePass (35.4.4) - the security-gate record for outbound movement. It
-- references the manifest rather than a single package because a gate pass is
-- issued per vehicle leaving the dock, and a vehicle carries a manifest's
-- worth of parcels. Vehicle/driver fields are free text: they are copied off a
-- licence at the gate, not selected from a master this repo maintains.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('GatePass', 'code', 'Gate Pass No', 'Data', TRUE, NULL, 1),
('GatePass', 'manifest_id', 'Manifest', 'Link', FALSE, 'Manifest', 2),
('GatePass', 'location_code', 'Location', 'Data', TRUE, NULL, 3),
('GatePass', 'carrier', 'Carrier', 'Data', FALSE, NULL, 4),
('GatePass', 'vehicle_number', 'Vehicle Number', 'Data', FALSE, NULL, 5),
('GatePass', 'driver_name', 'Driver Name', 'Data', FALSE, NULL, 6),
('GatePass', 'driver_phone', 'Driver Phone', 'Data', FALSE, NULL, 7),
('GatePass', 'package_count', 'Package Count', 'Number', FALSE, NULL, 8),
('GatePass', 'remarks', 'Remarks', 'Data', FALSE, NULL, 9),
('GatePass', 'reason_code', 'Discard Reason', 'Data', FALSE, NULL, 10),
('GatePass', 'status', 'Status', 'Select', TRUE, 'Draft,Issued,Completed,Discarded', 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ShippingPackage', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'GatePass', TRUE, TRUE, TRUE, TRUE),
('Super Admin', 'ShippingPackage', TRUE, TRUE, TRUE, TRUE),
('Super Admin', 'GatePass', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ShippingPackage', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'GatePass', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'ShippingPackage', TRUE, FALSE, FALSE, FALSE),
('Cashier', 'GatePass', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO UPDATE SET
    allow_read = EXCLUDED.allow_read,
    allow_create = EXCLUDED.allow_create,
    allow_update = EXCLUDED.allow_update,
    allow_delete = EXCLUDED.allow_delete;

-- Link the booking to the package it carries. Nullable, because every booking
-- created before this migration has no package and must keep working.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('LogisticsBooking', 'shipping_package_id', 'Shipping Package', 'Link', FALSE, 'ShippingPackage', 12),
('LogisticsBooking', 'label_generated_at', 'Label Generated At', 'Data', FALSE, NULL, 13)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- CreditNote gains the two links 35.4.5 needs to answer "which cancellation
-- produced this note". reference_cart already exists for the POS path and is
-- left alone - a SalesOrder is not a cart, and overloading the field would
-- make the POS reconciliation query wrong.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CreditNote', 'sales_order_id', 'Sales Order', 'Link', FALSE, 'SalesOrder', 7),
('CreditNote', 'sales_invoice_id', 'Sales Invoice', 'Link', FALSE, 'SalesInvoice', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- SalesInvoice gains the package link and the GST columns the pack-generated
-- invoice computes. The pre-existing total_amount keeps its meaning exactly -
-- what the customer pays - so PostSalesInvoice and the receivables ageing
-- report are untouched.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SalesInvoice', 'shipping_package_id', 'Shipping Package', 'Link', FALSE, 'ShippingPackage', 20),
('SalesInvoice', 'taxable_amount', 'Taxable Value', 'Number', FALSE, NULL, 21),
('SalesInvoice', 'cgst', 'CGST', 'Number', FALSE, NULL, 22),
('SalesInvoice', 'sgst', 'SGST', 'Number', FALSE, NULL, 23),
('SalesInvoice', 'igst', 'IGST', 'Number', FALSE, NULL, 24),
('SalesInvoice', 'total_tax', 'Total Tax', 'Number', FALSE, NULL, 25),
('SalesInvoice', 'items', 'Invoice Lines (JSON)', 'Data', FALSE, NULL, 26)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Status transition rules (Stage 29.8's strict map). Only legal edges are
-- listed; anything absent is denied. Note what is NOT here: Invoiced -> Draft.
-- Un-invoicing a package after the tax document has been raised is exactly the
-- move the 35.4.3 ordering rule exists to prevent, so it is simply not an edge.
INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
SELECT
  'STR-' || v.entity || '-' || v.from_status || '-' || v.to_status,
  'StatusTransitionRule',
  jsonb_build_object(
    'code',                 'STR-' || v.entity || '-' || v.from_status || '-' || v.to_status,
    'entity',               v.entity,
    'from_status',          v.from_status,
    'to_status',            v.to_status,
    'allowed',              'Yes',
    'requires_reason_code', v.needs_reason,
    'status',               'Active'
  ),
  'Active',
  'system'
FROM (VALUES
  ('ShippingPackage', 'Draft',    'Invoiced',  'No'),
  ('ShippingPackage', 'Draft',    'Cancelled', 'Yes'),
  ('ShippingPackage', 'Invoiced', 'Shipped',   'No'),
  ('ShippingPackage', 'Invoiced', 'Cancelled', 'Yes'),

  ('GatePass',        'Draft',    'Issued',    'No'),
  ('GatePass',        'Issued',   'Completed', 'No'),
  ('GatePass',        'Draft',    'Discarded', 'Yes'),
  ('GatePass',        'Issued',   'Discarded', 'Yes')
) AS v(entity, from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;

-- One package per (task, split lineage) is not enforceable as a unique index -
-- a task legitimately has several packages after a split. What IS enforced is
-- that a package code is unique, the same guard 36.1 puts on its group code
-- and for the same reason: the code is the operator-facing reference.
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_shipping_package_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'ShippingPackage' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_gate_pass_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'GatePass' AND deleted_at IS NULL;

-- The three lookups the engine does on every call: packages for a task,
-- packages for an order, and the invoice-exists check that keeps invoicing
-- idempotent.
CREATE INDEX IF NOT EXISTS idx_documents_shipping_package_task
    ON tenant_default.documents ((data->>'fulfillment_task_id'))
    WHERE doctype = 'ShippingPackage' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_shipping_package_order
    ON tenant_default.documents ((data->>'order_id'))
    WHERE doctype = 'ShippingPackage' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_sales_invoice_order
    ON tenant_default.documents ((data->>'sales_order_id'))
    WHERE doctype = 'SalesInvoice' AND deleted_at IS NULL;
