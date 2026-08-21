-- ---------------------------------------------------------------------------
-- Stage 42.4.3 - Sortation / put-wall: SortStation + SortSlot. Closes the
-- "batch pick with no sortation" hole in 26.5.6's wave pick list - a wave
-- pick consolidates N orders' demand into one walk (GenerateWavePickList),
-- but nothing before this item ever un-consolidated it back to a specific
-- order at the pack bench. A SortSlot is where that happens: one slot per
-- order/FulfillmentTask on a station, the picker (or a second sorter)
-- confirms a scan against the slot it belongs to.
--
-- zone (like Bin.zone/PackStation.zone) is Data validated against Zone.code,
-- not a Link - the same reason given in migrations_stage42_2_5_zone.sql.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('SortStation', 'Inventory', 'inventory', 'Master'),
('SortSlot', 'Inventory', 'inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SortStation', 'code', 'Station Code', 'Data', TRUE, NULL, 1),
('SortStation', 'location_code', 'Location (optional)', 'Link', FALSE, 'Location', 2),
('SortStation', 'zone', 'Zone (optional)', 'Data', FALSE, NULL, 3),
('SortStation', 'num_slots', 'Number of Slots', 'Number', TRUE, NULL, 4),
('SortStation', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),

('SortSlot', 'station', 'Sort Station', 'Data', TRUE, NULL, 1),
('SortSlot', 'slot_no', 'Slot Number', 'Number', TRUE, NULL, 2),
('SortSlot', 'wave_id', 'Wave (optional)', 'Data', FALSE, NULL, 3),
('SortSlot', 'fulfillment_task_id', 'Fulfillment Task / Order (optional)', 'Link', FALSE, 'FulfillmentTask', 4),
('SortSlot', 'sku', 'SKU Being Sorted (optional)', 'Data', FALSE, NULL, 5),
('SortSlot', 'qty_expected', 'Qty Expected (optional)', 'Number', FALSE, NULL, 6),
('SortSlot', 'qty_confirmed', 'Qty Confirmed (system-set)', 'Number', FALSE, NULL, 7),
('SortSlot', 'status', 'Status', 'Select', TRUE, 'Empty,Assigned,Filled,Cleared', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'SortStation', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'SortStation', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'SortStation', TRUE, FALSE, FALSE, FALSE),
('HR/Admin',       'SortSlot', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'SortSlot', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'SortSlot', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
