-- Stage 26.12 OMS/Order Management Maturity Sprint - foundation items,
-- built first per the sprint's own recommended build order in
-- micro_checklist.md (26.12.9 + 26.12.6 are the two foundational items
-- with no dependencies; every later 26.12 item builds on one or both).

-- 26.12.9 - Configuration masters (AllocationRule/ReasonCode/
-- StatusTransitionRule) as ordinary doctypes on the existing doctype-meta
-- engine - no new mechanism, same shape as the Stage 17.9 Location/
-- LegalEntity/Department/CostCenter masters this is copied from. Tagged
-- module_key='oms' (Stage 27's product-packaging module key), so they
-- travel with the OMS product exactly like the wms/oms-gated routes do.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('AllocationRule', 'OMS', 'oms', 'Master'),
('ReasonCode', 'OMS', 'oms', 'Master'),
('StatusTransitionRule', 'OMS', 'oms', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('AllocationRule', 'code', 'Rule Code', 'Data', TRUE, NULL, 1),
('AllocationRule', 'rule_name', 'Rule Name', 'Data', TRUE, NULL, 2),
('AllocationRule', 'strategy', 'Strategy', 'Select', TRUE, 'Highest ATS,Nearest Pincode,Lowest Workload,Oldest Stock,Split Shipment,Manual', 3),
('AllocationRule', 'priority', 'Priority (lower runs first)', 'Number', TRUE, NULL, 4),
('AllocationRule', 'channel', 'Channel (blank = all channels)', 'Data', FALSE, NULL, 5),
('AllocationRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6),
('ReasonCode', 'code', 'Reason Code', 'Data', TRUE, NULL, 1),
('ReasonCode', 'description', 'Description', 'Data', TRUE, NULL, 2),
('ReasonCode', 'category', 'Category', 'Select', TRUE, 'Cancellation,Hold,Return,Allocation Exception,Other', 3),
('ReasonCode', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4),
('StatusTransitionRule', 'code', 'Rule Code', 'Data', TRUE, NULL, 1),
('StatusTransitionRule', 'entity', 'Entity', 'Select', TRUE, 'Order,OrderLine,FulfillmentOrder,Shipment', 2),
('StatusTransitionRule', 'from_status', 'From Status', 'Data', TRUE, NULL, 3),
('StatusTransitionRule', 'to_status', 'To Status', 'Data', TRUE, NULL, 4),
('StatusTransitionRule', 'allowed', 'Allowed', 'Select', TRUE, 'Yes,No', 5),
('StatusTransitionRule', 'requires_reason_code', 'Requires Reason Code', 'Select', TRUE, 'Yes,No', 6),
('StatusTransitionRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'AllocationRule', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'ReasonCode', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'StatusTransitionRule', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'AllocationRule', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'ReasonCode', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'StatusTransitionRule', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 26.12.6 - Inventory buckets: additive columns completing the ATP
-- formula. Blueprint's ATP is 7-term (Physical - Reserved - Blocked -
-- QC Hold - Damaged - Safety Stock - Channel Buffer); this repo's
-- GetAvailableToSell had only 3 (Available - Reserved - Safety Stock) -
-- see docs/specs/oms_master_blueprint_reference.md §4. Same
-- tenant_default-only-then-clone-at-provision shape as the Stage 17.5
-- in_transit column this precedes (migrations_stage17e_transfer_orders.sql).
-- Read/formula changes are in engines/inventory.go and engines/sourcing.go.
ALTER TABLE tenant_default.inventory_availability ADD COLUMN IF NOT EXISTS blocked INT NOT NULL DEFAULT 0;
ALTER TABLE tenant_default.inventory_availability ADD COLUMN IF NOT EXISTS qc_hold INT NOT NULL DEFAULT 0;
ALTER TABLE tenant_default.inventory_availability ADD COLUMN IF NOT EXISTS damaged INT NOT NULL DEFAULT 0;
ALTER TABLE tenant_default.inventory_availability ADD COLUMN IF NOT EXISTS channel_buffer INT NOT NULL DEFAULT 0;
