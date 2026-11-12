-- ---------------------------------------------------------------------------
-- Stage 42.6 - Labour and billing depth (42.6.1-42.6.9).
--
-- This is deliberately metadata-led. Operations, engineered-time components,
-- schedules and commercial terms remain tenant-configurable masters; the Go
-- engine turns those records into plans and immutable billable events. No
-- existing rate or invoice document is reinterpreted, so tenants can adopt
-- these records incrementally.
-- ---------------------------------------------------------------------------

-- 42.6.1/42.6.2: an engineered standard is assembled from independently
-- maintained elements, allowances and a travel section. element_codes and
-- allowance_codes are comma-separated master codes because the generic form
-- engine has no child-table field type; each is resolved server-side at plan
-- time, so no flat seconds value can become stale on the LaborStandard.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('LaborOperation', 'Inventory', 'inventory', 'Master'),
('LaborElement', 'Inventory', 'inventory', 'Master'),
('LaborAllowance', 'Inventory', 'inventory', 'Master'),
('TravelSection', 'Inventory', 'inventory', 'Master'),
('LaborStandard', 'Inventory', 'inventory', 'Master'),
('Shift', 'HR', 'hr', 'Master'),
('WeeklySchedule', 'HR', 'hr', 'Master'),
('UserWorkSchedule', 'HR', 'hr', 'Master'),
('ChargeGroup', 'Inventory', 'inventory', 'Master'),
('RateGroup', 'Inventory', 'inventory', 'Master'),
('ChargeCode', 'Inventory', 'inventory', 'Master'),
('ChargeContract', 'Inventory', 'inventory', 'Master'),
('CapturedCharge', 'Inventory', 'inventory', 'Transaction'),
('StorageBalanceSnapshot', 'Inventory', 'inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('LaborOperation', 'code', 'Operation Code', 'Data', TRUE, NULL, 1),
('LaborOperation', 'name', 'Operation Name', 'Data', TRUE, NULL, 2),
('LaborOperation', 'task_type', 'Warehouse Task Type', 'Select', TRUE, 'Putaway,Pick,Replenish,Count,Move,VAS,Load,Unload', 3),
('LaborOperation', 'department', 'Department', 'Data', FALSE, NULL, 4),
('LaborOperation', 'labor_rate_per_hour', 'Labour Cost per Hour', 'Number', FALSE, NULL, 5),
('LaborOperation', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6),

('LaborElement', 'code', 'Element Code', 'Data', TRUE, NULL, 1),
('LaborElement', 'operation_code', 'Operation Code', 'Data', TRUE, NULL, 2),
('LaborElement', 'description', 'Description', 'Data', TRUE, NULL, 3),
('LaborElement', 'standard_seconds_per_unit', 'Engineered Seconds per Unit', 'Number', TRUE, NULL, 4),
('LaborElement', 'fixed_seconds', 'Fixed Seconds per Task', 'Number', FALSE, NULL, 5),
('LaborElement', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6),

('LaborAllowance', 'code', 'Allowance Code', 'Data', TRUE, NULL, 1),
('LaborAllowance', 'description', 'Description', 'Data', TRUE, NULL, 2),
('LaborAllowance', 'allowance_pct', 'Allowance %', 'Number', FALSE, NULL, 3),
('LaborAllowance', 'fixed_seconds', 'Fixed Allowance Seconds', 'Number', FALSE, NULL, 4),
('LaborAllowance', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),

('TravelSection', 'code', 'Travel Section Code', 'Data', TRUE, NULL, 1),
('TravelSection', 'description', 'Description', 'Data', TRUE, NULL, 2),
('TravelSection', 'seconds_per_task', 'Travel Seconds per Task', 'Number', FALSE, NULL, 3),
('TravelSection', 'seconds_per_unit', 'Travel Seconds per Unit', 'Number', FALSE, NULL, 4),
('TravelSection', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),

('LaborStandard', 'code', 'Standard Code', 'Data', TRUE, NULL, 1),
('LaborStandard', 'name', 'Standard Name', 'Data', TRUE, NULL, 2),
('LaborStandard', 'operation_code', 'Operation Code', 'Data', TRUE, NULL, 3),
('LaborStandard', 'element_codes', 'Element Codes (comma-separated)', 'Data', TRUE, NULL, 4),
('LaborStandard', 'allowance_codes', 'Allowance Codes (comma-separated)', 'Data', FALSE, NULL, 5),
('LaborStandard', 'travel_section_code', 'Travel Section Code', 'Data', FALSE, NULL, 6),
('LaborStandard', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7),

('Shift', 'code', 'Shift Code', 'Data', TRUE, NULL, 1),
('Shift', 'name', 'Shift Name', 'Data', TRUE, NULL, 2),
('Shift', 'start_time', 'Start Time (HH:MM)', 'Data', TRUE, NULL, 3),
('Shift', 'end_time', 'End Time (HH:MM)', 'Data', TRUE, NULL, 4),
('Shift', 'unpaid_break_minutes', 'Unpaid Break Minutes', 'Number', FALSE, NULL, 5),
('Shift', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6),

('WeeklySchedule', 'code', 'Schedule Code', 'Data', TRUE, NULL, 1),
('WeeklySchedule', 'name', 'Schedule Name', 'Data', TRUE, NULL, 2),
('WeeklySchedule', 'department', 'Department', 'Data', TRUE, NULL, 3),
('WeeklySchedule', 'shift_code', 'Shift Code', 'Data', TRUE, NULL, 4),
('WeeklySchedule', 'work_days', 'Work Days (comma-separated)', 'Data', TRUE, NULL, 5),
('WeeklySchedule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6),

('UserWorkSchedule', 'code', 'Assignment Code', 'Data', TRUE, NULL, 1),
('UserWorkSchedule', 'user_id', 'User', 'Data', TRUE, NULL, 2),
('UserWorkSchedule', 'weekly_schedule_code', 'Weekly Schedule Code', 'Data', TRUE, NULL, 3),
('UserWorkSchedule', 'effective_from', 'Effective From', 'Date', TRUE, NULL, 4),
('UserWorkSchedule', 'effective_to', 'Effective To', 'Date', FALSE, NULL, 5),
('UserWorkSchedule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6),

-- 42.6.6: commercial terms can be owned at the code, group or contract
-- layer. The engine combines markups/discounts and takes the greatest
-- configured minimum, while a contract's non-zero tax rate is the final tax
-- level for the captured event.
('ChargeGroup', 'code', 'Charge Group Code', 'Data', TRUE, NULL, 1),
('ChargeGroup', 'name', 'Charge Group Name', 'Data', TRUE, NULL, 2),
('ChargeGroup', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3),

('RateGroup', 'code', 'Rate Group Code', 'Data', TRUE, NULL, 1),
('RateGroup', 'name', 'Rate Group Name', 'Data', TRUE, NULL, 2),
('RateGroup', 'markup_pct', 'Markup %', 'Number', FALSE, NULL, 3),
('RateGroup', 'discount_pct', 'Discount %', 'Number', FALSE, NULL, 4),
('RateGroup', 'minimum_charge', 'Minimum Charge', 'Number', FALSE, NULL, 5),
('RateGroup', 'tax_rate', 'Tax Rate %', 'Number', FALSE, NULL, 6),
('RateGroup', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7),

('ChargeCode', 'code', 'Charge Code', 'Data', TRUE, NULL, 1),
('ChargeCode', 'name', 'Charge Name', 'Data', TRUE, NULL, 2),
('ChargeCode', 'charge_group_code', 'Charge Group Code', 'Data', FALSE, NULL, 3),
('ChargeCode', 'rate_group_code', 'Rate Group Code', 'Data', FALSE, NULL, 4),
('ChargeCode', 'trigger_event', 'Billable Trigger', 'Select', TRUE, 'Warehouse Task Completed,Manual,Storage Period', 5),
('ChargeCode', 'task_type', 'Warehouse Task Type (optional)', 'Select', FALSE, 'Putaway,Pick,Replenish,Count,Move,VAS,Load,Unload', 6),
('ChargeCode', 'owner_id', 'Owner / Customer (optional)', 'Data', FALSE, NULL, 7),
('ChargeCode', 'location_code', 'Location (optional)', 'Data', FALSE, NULL, 8),
('ChargeCode', 'default_rate', 'Rate per Unit', 'Number', TRUE, NULL, 9),
('ChargeCode', 'markup_pct', 'Markup %', 'Number', FALSE, NULL, 10),
('ChargeCode', 'discount_pct', 'Discount %', 'Number', FALSE, NULL, 11),
('ChargeCode', 'minimum_charge', 'Minimum Charge', 'Number', FALSE, NULL, 12),
('ChargeCode', 'tax_rate', 'Tax Rate %', 'Number', FALSE, NULL, 13),
('ChargeCode', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 14),

('ChargeContract', 'code', 'Contract Code', 'Data', TRUE, NULL, 1),
('ChargeContract', 'owner_id', 'Owner / Customer', 'Data', TRUE, NULL, 2),
('ChargeContract', 'rate_group_code', 'Rate Group Code', 'Data', FALSE, NULL, 3),
('ChargeContract', 'effective_from', 'Effective From', 'Date', TRUE, NULL, 4),
('ChargeContract', 'effective_to', 'Effective To', 'Date', FALSE, NULL, 5),
('ChargeContract', 'markup_pct', 'Markup %', 'Number', FALSE, NULL, 6),
('ChargeContract', 'discount_pct', 'Discount %', 'Number', FALSE, NULL, 7),
('ChargeContract', 'minimum_charge', 'Minimum Charge', 'Number', FALSE, NULL, 8),
('ChargeContract', 'tax_rate', 'Tax Rate %', 'Number', FALSE, NULL, 9),
('ChargeContract', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 10),

-- System-written evidence. event_key plus code is copied into a deterministic
-- documents.id by the engine, making a retry idempotent without a bespoke
-- table. No generic role may create/update/rewrite an amount after capture.
('CapturedCharge', 'code', 'Captured Charge ID', 'Data', TRUE, NULL, 1),
('CapturedCharge', 'event_key', 'Source Event Key', 'Data', TRUE, NULL, 2),
('CapturedCharge', 'trigger_event', 'Trigger Event', 'Data', TRUE, NULL, 3),
('CapturedCharge', 'charge_code', 'Charge Code', 'Data', TRUE, NULL, 4),
('CapturedCharge', 'owner_id', 'Owner / Customer', 'Data', TRUE, NULL, 5),
('CapturedCharge', 'location_code', 'Location', 'Data', FALSE, NULL, 6),
('CapturedCharge', 'quantity', 'Quantity', 'Number', TRUE, NULL, 7),
('CapturedCharge', 'net_amount', 'Net Amount', 'Number', TRUE, NULL, 8),
('CapturedCharge', 'tax_rate', 'Tax Rate %', 'Number', FALSE, NULL, 9),
('CapturedCharge', 'tax_amount', 'Tax Amount', 'Number', TRUE, NULL, 10),
('CapturedCharge', 'total_amount', 'Total Amount', 'Number', TRUE, NULL, 11),
('CapturedCharge', 'occurred_on', 'Occurred On', 'Date', TRUE, NULL, 12),
('CapturedCharge', 'invoice_id', 'Sales Invoice (system-set)', 'Data', FALSE, NULL, 13),
('CapturedCharge', 'status', 'Status', 'Select', TRUE, 'Captured,Billed,Cancelled', 14),

-- 42.6.9: daily end-of-day balances are the historical ledger that makes
-- average storage billing exact for the days actually snapshotted. A snapshot
-- is system-written and never editable through the generic document API.
('StorageBalanceSnapshot', 'code', 'Snapshot ID', 'Data', TRUE, NULL, 1),
('StorageBalanceSnapshot', 'snapshot_date', 'Snapshot Date', 'Date', TRUE, NULL, 2),
('StorageBalanceSnapshot', 'owner_id', 'Owner / Customer', 'Data', TRUE, NULL, 3),
('StorageBalanceSnapshot', 'location_code', 'Location', 'Data', TRUE, NULL, 4),
('StorageBalanceSnapshot', 'item', 'Item', 'Data', TRUE, NULL, 5),
('StorageBalanceSnapshot', 'quantity', 'Quantity (EA)', 'Number', TRUE, NULL, 6),
('StorageBalanceSnapshot', 'status', 'Status', 'Select', TRUE, 'Captured', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Existing StorageBillingRate becomes a valid v2 source without forcing a
-- data migration. These optional commercial fields apply only when present.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('StorageBillingRate', 'charge_code', 'Charge Code (optional)', 'Data', FALSE, NULL, 8),
('StorageBillingRate', 'rate_group_code', 'Rate Group Code (optional)', 'Data', FALSE, NULL, 9),
('StorageBillingRate', 'tax_rate', 'Tax Rate % (optional)', 'Number', FALSE, NULL, 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'LaborOperation', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'LaborOperation', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'LaborElement', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'LaborElement', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'LaborAllowance', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'LaborAllowance', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'TravelSection', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'TravelSection', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'LaborStandard', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'LaborStandard', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'Shift', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'WeeklySchedule', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'UserWorkSchedule', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'ChargeGroup', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ChargeGroup', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'RateGroup', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'RateGroup', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'ChargeCode', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ChargeCode', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'ChargeContract', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ChargeContract', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'CapturedCharge', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'CapturedCharge', TRUE, FALSE, FALSE, FALSE),
('HR/Admin', 'StorageBalanceSnapshot', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'StorageBalanceSnapshot', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
