-- Stage 146.4.4 - ship-from-store limits. Location gets four optional fields; the PIN-code
-- master is a small master the admin imports (never bundled). All blank = today's routing.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Location', 'ship_channels', 'Online Channels Served (blank = all)', 'Data', FALSE, NULL, 40),
('Location', 'ship_online', 'Ships Online Orders', 'Select', FALSE, 'Yes,No', 41),
('Location', 'ship_daily_cap', 'Online Orders per Day (blank = no cap)', 'Number', FALSE, NULL, 42),
('Location', 'ship_priority', 'Sourcing Priority (1 = first)', 'Number', FALSE, NULL, 43)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('PinCode', 'Inventory', 'Master', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PinCode', 'code', 'PIN Code', 'Data', TRUE, NULL, 1),
('PinCode', 'latitude', 'Latitude', 'Number', TRUE, NULL, 2),
('PinCode', 'longitude', 'Longitude', 'Number', TRUE, NULL, 3),
('PinCode', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PinCode', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PinCode', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'PinCode', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
