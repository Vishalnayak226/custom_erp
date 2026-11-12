-- ---------------------------------------------------------------------------
-- Stage 42.3.2 - Appointment doctype. dock_door stays Data (not Link),
-- validated against DockDoor.code by validateAppointmentMasterRules -
-- the same "value a person actually types/scans is the human code, not a
-- generated document id" choice 42.2.5's Zone note explains and every WMS
-- cross-reference since has followed. appointment_date/start_time/end_time
-- split (Date + two HH:MM Data fields) for the same reason DockDoor's
-- service window is split: no Datetime fieldtype exists to hold either in
-- one field.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('Appointment', 'Inventory', 'inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Appointment', 'dock_door', 'Dock Door', 'Data', TRUE, NULL, 1),
('Appointment', 'appointment_type', 'Appointment Type', 'Select', TRUE, 'Inbound,Outbound', 2),
('Appointment', 'carrier', 'Carrier (optional)', 'Data', FALSE, NULL, 3),
('Appointment', 'trailer_no', 'Trailer No (optional)', 'Data', FALSE, NULL, 4),
('Appointment', 'po_id', 'PO Reference (optional)', 'Link', FALSE, 'PurchaseOrder', 5),
('Appointment', 'asn_id', 'ASN Reference (optional)', 'Link', FALSE, 'ASN', 6),
('Appointment', 'appointment_date', 'Appointment Date', 'Date', TRUE, NULL, 7),
('Appointment', 'start_time', 'Start Time (HH:MM)', 'Data', TRUE, NULL, 8),
('Appointment', 'end_time', 'End Time (HH:MM)', 'Data', TRUE, NULL, 9),
('Appointment', 'status', 'Status', 'Select', TRUE, 'Scheduled,CheckedIn,InProgress,Completed,Cancelled,NoShow', 10),
('Appointment', 'notes', 'Notes (optional)', 'Data', FALSE, NULL, 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'Appointment', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'Appointment', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'Appointment', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
