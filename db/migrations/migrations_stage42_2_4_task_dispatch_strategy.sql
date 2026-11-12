-- ---------------------------------------------------------------------------
-- Stage 42.2.4 - TaskDispatchStrategy: lifts GetNextTask's hardcoded ordering
-- (priority descending, then ageing) out of code and into configurable data,
-- per-location or global.
--
-- Deliberately three criteria, not the plan's four: priority, ageing, type.
-- "Proximity" (order by nearest bin to the picker's current position) is
-- explicitly NOT buildable yet - it needs real distance data, which needs
-- 42.2.5's Zone master (today Bin.zone is free text) at a minimum, and
-- arguably real bin coordinates beyond that. Modelling a "proximity" sort key
-- now that silently falls back to something arbitrary would be worse than
-- not offering it - a tenant could configure a strategy that claims to sort
-- by proximity and get a meaningless order instead. Add it as a fourth
-- criterion once 42.2.5 (or later) gives it something real to sort by.
--
-- module_key 'inventory' matches WarehouseTask/FulfillmentTask/LPN - the
-- established precedent for this Stage's masters, not the 'wms' route-gate
-- key.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('TaskDispatchStrategy', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('TaskDispatchStrategy', 'code', 'Strategy Code', 'Data', TRUE, NULL, 1),
('TaskDispatchStrategy', 'location_code', 'Location (optional - blank applies everywhere)', 'Data', FALSE, NULL, 2),
('TaskDispatchStrategy', 'sort_order', 'Sort Order (comma-separated: priority, ageing, type)', 'Data', TRUE, NULL, 3),
('TaskDispatchStrategy', 'notes', 'Notes', 'Data', FALSE, NULL, 4),
('TaskDispatchStrategy', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'TaskDispatchStrategy', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'TaskDispatchStrategy', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'TaskDispatchStrategy', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
