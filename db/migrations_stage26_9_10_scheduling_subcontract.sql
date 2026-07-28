-- Stage 26.9.10/26.9.11 (Manufacturing/MRP Sprint P2 follow-up): finite/
-- infinite capacity scheduling + subcontracting/outside-processing.
-- Go-ahead given 2026-07-27 for all five P2 bundles previously deferred
-- pending a real pilot customer - built as generic, tenant-configurable
-- capability like the rest of Stage 26, not against one guessed process.

-- 26.9.10: ProductionOrder.due_date is additive, purely advisory input for
-- GetProductionSchedule's earliest-due-first ordering (engines/
-- manufacturing_scheduling.go) - orders without one sort last by created_at.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductionOrder', 'due_date', 'Due Date (optional)', 'Data', FALSE, NULL, 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 26.9.11: SubcontractOrder - a flat-schema Transaction doctype, same
-- "generic form/table covers create/edit, only the state-changing actions
-- (Send/Receive) get bespoke handlers" shape as PurchaseRequisition/
-- QualityInspection before it. Raw material moves out on Send, the
-- processed/finished good moves back in on Receive, both through the
-- existing inventory ledger (PostInventoryLedgerWithVoucher) rather than a
-- parallel stock model.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('SubcontractOrder', 'Manufacturing', 'Transaction', 'manufacturing')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SubcontractOrder', 'code', 'Subcontract Order Number', 'Data', TRUE, NULL, 1),
('SubcontractOrder', 'vendor_id', 'Subcontractor Vendor', 'Link', TRUE, 'Vendor', 2),
('SubcontractOrder', 'location', 'Sending Location', 'Data', TRUE, NULL, 3),
('SubcontractOrder', 'sent_item_id', 'Raw Material SKU', 'Data', TRUE, NULL, 4),
('SubcontractOrder', 'sent_qty', 'Qty to Send', 'Number', TRUE, NULL, 5),
('SubcontractOrder', 'received_item_id', 'Processed/Finished SKU', 'Data', TRUE, NULL, 6),
('SubcontractOrder', 'expected_received_qty', 'Expected Qty Back', 'Number', FALSE, NULL, 7),
('SubcontractOrder', 'actual_received_qty', 'Actual Qty Received', 'Number', FALSE, NULL, 8),
('SubcontractOrder', 'sent_date', 'Sent Date', 'Data', FALSE, NULL, 9),
('SubcontractOrder', 'received_date', 'Received Date', 'Data', FALSE, NULL, 10),
('SubcontractOrder', 'status', 'Status', 'Select', TRUE, 'Draft,Sent,Received,Cancelled', 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'SubcontractOrder', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'SubcontractOrder', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- SendToSubcontractor/ReceiveFromSubcontractor (engines/
-- manufacturing_scheduling.go) tag their PostInventoryLedgerWithVoucher
-- calls voucher_type='SubcontractOrder' - widen the Stock Ledger report's
-- (26.10.1) Select options the same additive way 26.6.8/26.7.x etc. each
-- added their own voucher_type value.
UPDATE tenant_default.doctype_fields
SET options = options || ',SubcontractOrder'
WHERE doctype_name = 'StockLedgerEntry' AND fieldname = 'voucher_type' AND options NOT LIKE '%SubcontractOrder%';
