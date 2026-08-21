-- ---------------------------------------------------------------------------
-- Stage 42.3.8 - Planned cross-dock + flow-through/transship. 26.5.3's
-- CrossDockPutaway (engines/wms_putaway_ext.go) is opportunistic-only: it
-- scans for open outbound demand at the moment putaway happens, with no way
-- to decide ahead of receipt that an inbound shipment is destined straight
-- through. CrossDockPlan is that ahead-of-time decision - made against an
-- ASN/PO before the truck even arrives, so receiving staff route the
-- shipment to staging on sight instead of shelving it and hoping
-- CheckCrossDockOpportunity finds a match later.
--
-- plan_type distinguishes CrossDock/FlowThrough (destination is an existing
-- internal FulfillmentTask or TransferOrder, same as 26.5.3) from Transship
-- (destination is a location this tenant has no TransferOrder for yet - the
-- stock is staged for one to be raised, rather than assuming it already
-- exists). destination_ref is Data, not Link, for the same reason every
-- other WMS cross-reference this Stage adds is Data - see Zone's migration
-- note.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('CrossDockPlan', 'Inventory', 'inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CrossDockPlan', 'asn_id', 'ASN Reference (optional)', 'Link', FALSE, 'ASN', 1),
('CrossDockPlan', 'po_id', 'PO Reference (optional)', 'Link', FALSE, 'PurchaseOrder', 2),
('CrossDockPlan', 'sku', 'SKU', 'Data', TRUE, NULL, 3),
('CrossDockPlan', 'qty', 'Planned Qty', 'Number', TRUE, NULL, 4),
('CrossDockPlan', 'plan_type', 'Plan Type', 'Select', TRUE, 'CrossDock,FlowThrough,Transship', 5),
('CrossDockPlan', 'destination_type', 'Destination Type', 'Select', TRUE, 'FulfillmentTask,TransferOrder,Location', 6),
('CrossDockPlan', 'destination_ref', 'Destination Reference', 'Data', TRUE, NULL, 7),
('CrossDockPlan', 'status', 'Status', 'Select', TRUE, 'Planned,Fulfilled,Cancelled', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'CrossDockPlan', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'CrossDockPlan', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'CrossDockPlan', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
