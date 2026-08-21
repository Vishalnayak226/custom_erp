-- ---------------------------------------------------------------------------
-- Stage 42.3.1 - DockDoor master. Door type, equipment, and a service window
-- (HH:MM strings, same convention as every other time-of-day field in this
-- tree - there is no Datetime fieldtype in the generic doc engine, see
-- engines/doctype.go's fieldTypeAllowed set). max_concurrent_appointments
-- is the door's simultaneous-trailer capacity that 42.3.2's scheduling
-- validator checks against; blank/zero means "1" (a door is single-trailer
-- unless explicitly widened).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('DockDoor', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('DockDoor', 'code', 'Door Code', 'Data', TRUE, NULL, 1),
('DockDoor', 'location', 'Location (optional)', 'Link', FALSE, 'Location', 2),
('DockDoor', 'door_type', 'Door Type', 'Select', TRUE, 'Inbound,Outbound,Both', 3),
('DockDoor', 'equipment', 'Equipment (optional, comma-separated)', 'Data', FALSE, NULL, 4),
('DockDoor', 'service_window_start', 'Service Window Start (HH:MM, optional)', 'Data', FALSE, NULL, 5),
('DockDoor', 'service_window_end', 'Service Window End (HH:MM, optional)', 'Data', FALSE, NULL, 6),
('DockDoor', 'max_concurrent_appointments', 'Max Concurrent Appointments (optional, default 1)', 'Number', FALSE, NULL, 7),
('DockDoor', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'DockDoor', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'DockDoor', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'DockDoor', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
