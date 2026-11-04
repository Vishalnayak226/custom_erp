-- Stage 146.3.7 / 146.3.14 - put-away rules, and bin / zone blocks for receipts or picks only.
-- Additive and re-runnable. A blank block reads as no block and no rules read as today's put-away,
-- so nothing changes until someone sets one. Who and when a block was set are kept by the server.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Zone', 'flow_block', 'Block (blank = open)', 'Select', FALSE, 'No receipts,No picks', 30),
('Zone', 'flow_block_reason', 'Block Reason', 'Data', FALSE, NULL, 31),
('Zone', 'flow_block_by', 'Blocked By', 'Data', FALSE, NULL, 32),
('Zone', 'flow_block_at', 'Blocked At', 'Data', FALSE, NULL, 33),
('Bin', 'flow_block', 'Block (blank = open)', 'Select', FALSE, 'No receipts,No picks', 30),
('Bin', 'flow_block_reason', 'Block Reason', 'Data', FALSE, NULL, 31),
('Bin', 'flow_block_by', 'Blocked By', 'Data', FALSE, NULL, 32),
('Bin', 'flow_block_at', 'Blocked At', 'Data', FALSE, NULL, 33)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('PutawayRule', 'Inventory', 'Master', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PutawayRule', 'item', 'Item', 'Link', FALSE, 'Item', 1),
('PutawayRule', 'design', 'Design', 'Link', FALSE, 'ProductFamily', 2),
('PutawayRule', 'category', 'Category', 'Data', FALSE, NULL, 3),
('PutawayRule', 'location_code', 'Location (blank = all)', 'Data', FALSE, NULL, 4),
('PutawayRule', 'zone', 'Zone', 'Data', FALSE, NULL, 5),
('PutawayRule', 'bin_code', 'Bin', 'Data', FALSE, NULL, 6),
('PutawayRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PutawayRule', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PutawayRule', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'PutawayRule', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
