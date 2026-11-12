-- ---------------------------------------------------------------------------
-- Stage 42.4.5 - PackStation + PackTemplate masters: per-station
-- configuration and per-item/customer packing rules (carton type, dunnage,
-- required documents, required labels). Both are plain Masters with zero
-- bespoke frontend, the same free ride Zone/PutawayStrategy/
-- AllocationStrategy/TaskDispatchStrategy already get from the sidebar's
-- generic Master browser.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PackStation', 'Inventory', 'inventory', 'Master'),
('PackTemplate', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PackStation', 'code', 'Station Code', 'Data', TRUE, NULL, 1),
('PackStation', 'location_code', 'Location (optional)', 'Link', FALSE, 'Location', 2),
('PackStation', 'zone', 'Zone (optional)', 'Data', FALSE, NULL, 3),
('PackStation', 'default_pack_template', 'Default Pack Template (optional)', 'Data', FALSE, NULL, 4),
('PackStation', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),

('PackTemplate', 'code', 'Template Code', 'Data', TRUE, NULL, 1),
('PackTemplate', 'name', 'Name', 'Data', TRUE, NULL, 2),
('PackTemplate', 'customer', 'Customer Name (optional - blank applies to all)', 'Data', FALSE, NULL, 3),
('PackTemplate', 'item', 'Item SKU (optional - blank applies to all)', 'Data', FALSE, NULL, 4),
('PackTemplate', 'carton_type', 'Carton Type (optional)', 'Data', FALSE, NULL, 5),
('PackTemplate', 'dunnage', 'Dunnage (optional)', 'Data', FALSE, NULL, 6),
('PackTemplate', 'documents_required', 'Documents Required (comma-separated, optional)', 'Data', FALSE, NULL, 7),
('PackTemplate', 'labels_required', 'Labels Required (comma-separated, optional)', 'Data', FALSE, NULL, 8),
('PackTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'PackStation', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'PackStation', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'PackStation', TRUE, FALSE, FALSE, FALSE),
('HR/Admin',       'PackTemplate', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'PackTemplate', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'PackTemplate', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
