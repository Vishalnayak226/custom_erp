-- Stage 26.5.13/26.5.15 (WMS Enterprise Maturity Sprint P2 follow-up):
-- labor standards/productivity dashboard + 3PL multi-owner billing. Go-
-- ahead given 2026-07-27 for all five P2 bundles previously deferred
-- pending a real warehouse-scale pilot.

-- 26.5.13: TaskCompletionLog is a plain system-written log doctype (same
-- "engine writes directly into documents, no bespoke table" precedent
-- StockLedgerEntry/ReportRunLog already set) - written by PutawayToBin,
-- PackTransferOrder, and PostCycleCountAdjustment (engines/wms.go,
-- engines/transfer_orders.go). Picking is a known, deliberate gap: the
-- granular per-scan ScanPickItem (engines/fulfillment_pickpack.go) has no
-- per-user tracking today, and adding it would mean changing that
-- function's signature across every caller - out of scope for this pass,
-- left as a documented gap rather than force-fit.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('TaskCompletionLog', 'Inventory', 'Transaction', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('TaskCompletionLog', 'task_type', 'Task Type', 'Select', TRUE, 'Putaway,Pack,CycleCount', 1),
('TaskCompletionLog', 'user_id', 'User', 'Data', TRUE, NULL, 2),
('TaskCompletionLog', 'location_code', 'Location', 'Data', FALSE, NULL, 3),
('TaskCompletionLog', 'reference_id', 'Reference', 'Data', FALSE, NULL, 4),
('TaskCompletionLog', 'qty', 'Qty', 'Number', FALSE, NULL, 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'TaskCompletionLog', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'TaskCompletionLog', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 26.5.15: StorageBillingRate - per-owner per-UOM storage/handling rate for
-- 3PL multi-owner billing, a plain Master (rates are config, not a
-- transaction). GetStorageBillingReport (engines/wms_3pl_billing.go)
-- computes period charges from bin_stock occupancy and TaskCompletionLog
-- above, per owner - read-only report, no new invoice/GL document created
-- (that integration is a future pass, same "read-only suggestion first"
-- precedent as everything else added this pass).
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('StorageBillingRate', 'Inventory', 'Master', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('StorageBillingRate', 'code', 'Rate Code', 'Data', TRUE, NULL, 1),
('StorageBillingRate', 'owner_id', 'Owner (3PL Client)', 'Data', TRUE, NULL, 2),
('StorageBillingRate', 'location_code', 'Location', 'Data', TRUE, NULL, 3),
('StorageBillingRate', 'storage_rate_per_unit_per_day', 'Storage Rate (per unit/day)', 'Number', TRUE, NULL, 4),
('StorageBillingRate', 'handling_rate_per_task', 'Handling Rate (per task)', 'Number', TRUE, NULL, 5),
('StorageBillingRate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'StorageBillingRate', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'StorageBillingRate', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 26.5.15: bin_stock has no owner concept today (single-tenant-per-schema
-- assumption) - additive owner_id field on Bin itself (a bin is assigned
-- to one 3PL client owner at a time, the same granularity bin_type already
-- uses) rather than restructuring bin_stock's own schema. Bin.data is
-- JSONB, so this is a doctype_fields row only, no ALTER TABLE needed.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Bin', 'owner_id', 'Owner (3PL Client, optional)', 'Data', FALSE, NULL, 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
