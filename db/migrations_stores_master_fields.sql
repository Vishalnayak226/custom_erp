-- Fills in the "Stores" Master doctype, found completely empty (zero
-- fields, zero role_permissions rows) during a live UI walkthrough
-- (2026-07-20) of the Database Schema Design screen - it's the only
-- registered doctype in the whole system with no fields. Field shape
-- mirrors the closest existing analog, Location (code/name/status), plus
-- the contact fields Vendor already uses for a physical site.
--
-- module_key was also blank (every sibling Inventory & WMS doctype - Bin,
-- Location's own 'core' aside - carries one); backfilled to 'inventory' to
-- match Bin, its sidebar neighbor.

UPDATE tenant_default.doctype_meta
SET module_key = 'inventory'
WHERE name = 'Stores' AND (module_key IS NULL OR module_key = '');

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Stores', 'code', 'Store Code', 'Data', TRUE, NULL, 1),
('Stores', 'name', 'Store Name', 'Data', TRUE, NULL, 2),
('Stores', 'address', 'Address', 'Data', FALSE, NULL, 3),
('Stores', 'city', 'City', 'Data', FALSE, NULL, 4),
('Stores', 'contact_phone', 'Contact Phone', 'Data', FALSE, NULL, 5),
('Stores', 'manager', 'Store Manager', 'Data', FALSE, NULL, 6),
('Stores', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Without these, every role (including HR/Admin) gets a default-deny 403 on
-- Stores - handleCheckPermission denies whenever no row matches. Mirrors
-- Location's exact split: HR/Admin full CRUD, Store Manager read-only.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Stores', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Stores', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
