-- Stage 146.3.1 / 146.3.4 - one item per bin, home bins, bin level / position / pick sequence.
-- Additive and re-runnable. A blank bin mix rule reads as Mixed, so nothing changes until a
-- zone or bin is set. Home bins are their own small master (BinReplenishmentRule is a min / max
-- pick-face rule; a home bin has a priority and an "allow other items" flag instead).
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Zone', 'bin_mix_rule', 'Bin Mix (blank = Mixed)', 'Select', FALSE, 'Mixed,Single item,Single item single lot', 9),
('Bin', 'bin_mix_rule', 'Bin Mix (blank = zone rule)', 'Select', FALSE, 'Mixed,Single item,Single item single lot', 20),
('Bin', 'level', 'Shelf / Level', 'Data', FALSE, NULL, 21),
('Bin', 'position', 'Position', 'Data', FALSE, NULL, 22),
('Bin', 'pick_sequence', 'Pick Sequence (blank = code order)', 'Number', FALSE, NULL, 23)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('HomeBin', 'Inventory', 'Master', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('HomeBin', 'sku', 'SKU', 'Data', TRUE, NULL, 1),
('HomeBin', 'bin_code', 'Bin Code', 'Data', TRUE, NULL, 2),
('HomeBin', 'priority', 'Priority (1 = first)', 'Number', FALSE, NULL, 3),
('HomeBin', 'allow_other_items', 'Allow Other Items', 'Select', FALSE, 'Yes,No', 4),
('HomeBin', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'HomeBin', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'HomeBin', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'HomeBin', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
