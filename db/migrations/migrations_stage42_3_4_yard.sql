-- ---------------------------------------------------------------------------
-- Stage 42.3.4 - Yard check-in/check-out: Trailer master + YardCheckIn
-- status log. Infor's 3D yard view is explicitly out of scope (micro
-- checklist item text) - this is a flat status log (InYard/AtDoor/Departed),
-- not a spatial yard map.
--
-- checked_out_at is a plain Data field the frontend's "Check Out" action
-- fills with the current timestamp before saving (same reason Appointment
-- splits date/time - no Datetime fieldtype). validateYardCheckInMasterRules
-- enforces it is set exactly when status becomes Departed, and enforces the
-- one-way transition order (no skipping back from Departed to InYard) - see
-- engines/master_data_validation.go.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('Trailer', 'Inventory', 'inventory', 'Master'),
('YardCheckIn', 'Inventory', 'inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Trailer', 'code', 'Trailer Code', 'Data', TRUE, NULL, 1),
('Trailer', 'carrier', 'Carrier (optional)', 'Data', FALSE, NULL, 2),
('Trailer', 'trailer_type', 'Trailer Type', 'Select', FALSE, 'Dry Van,Reefer,Flatbed,Container,Other', 3),
('Trailer', 'license_plate', 'License Plate (optional)', 'Data', FALSE, NULL, 4),
('Trailer', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),

('YardCheckIn', 'trailer_no', 'Trailer No', 'Data', TRUE, NULL, 1),
('YardCheckIn', 'carrier', 'Carrier (optional)', 'Data', FALSE, NULL, 2),
('YardCheckIn', 'driver_name', 'Driver Name (optional)', 'Data', FALSE, NULL, 3),
('YardCheckIn', 'appointment_id', 'Appointment Reference (optional)', 'Link', FALSE, 'Appointment', 4),
('YardCheckIn', 'dock_door', 'Dock Door (optional, set on spotting)', 'Data', FALSE, NULL, 5),
('YardCheckIn', 'yard_location', 'Yard Slot (optional)', 'Data', FALSE, NULL, 6),
('YardCheckIn', 'status', 'Status', 'Select', TRUE, 'InYard,AtDoor,Departed', 7),
('YardCheckIn', 'checked_out_at', 'Checked Out At (optional, set on departure)', 'Data', FALSE, NULL, 8),
('YardCheckIn', 'notes', 'Notes (optional)', 'Data', FALSE, NULL, 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'Trailer', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'Trailer', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'Trailer', TRUE, FALSE, FALSE, FALSE),
('HR/Admin',       'YardCheckIn', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'YardCheckIn', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'YardCheckIn', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
