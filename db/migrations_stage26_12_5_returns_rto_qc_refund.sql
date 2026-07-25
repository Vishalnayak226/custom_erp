-- Stage 26.12.5: Returns/RTO/QC/Refund. Extends the existing instant
-- ProcessReturnAnywhere (engines/fulfillment.go, unchanged - that stays the
-- POS in-store walk-in-return path, a legitimately different product from
-- an OMS/e-commerce return) with a request/approval-gated workflow: a
-- ReturnRequest for both a customer-initiated return AND a courier RTO
-- (request_type distinguishes them), QC disposition buckets driving which
-- 26.12.6 inventory bucket a line lands in, and a RefundRequest distinct
-- from the immediate GL post ProcessReturnAnywhere still does for its own
-- in-store path. Both Transaction doctypes (created only via
-- engines/returns.go's dedicated entry points, same reasoning as
-- SalesOrder/SalesOrderLine), module_key='oms'.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ReturnRequest', 'OMS', 'oms', 'Transaction'),
('RefundRequest', 'OMS', 'oms', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ReturnRequest', 'code', 'Return Request Number', 'Data', TRUE, NULL, 1),
('ReturnRequest', 'request_type', 'Request Type', 'Select', TRUE, 'Customer Return,RTO', 2),
('ReturnRequest', 'original_order_id', 'Original Order Reference', 'Data', FALSE, NULL, 3),
('ReturnRequest', 'booking_id', 'Shipment/Booking Reference (RTO only)', 'Data', FALSE, NULL, 4),
('ReturnRequest', 'return_location', 'Return Location', 'Data', TRUE, NULL, 5),
('ReturnRequest', 'status', 'Status', 'Select', TRUE, 'Requested,Approved,Rejected,Received,QC Complete,Closed', 6),
('ReturnRequest', 'requested_by', 'Requested By', 'Data', FALSE, NULL, 7),
('ReturnRequest', 'approved_by', 'Approved By', 'Data', FALSE, NULL, 8),
('ReturnRequest', 'rejection_reason', 'Rejection Reason Code', 'Data', FALSE, NULL, 9),
('ReturnRequest', 'items', 'Items (JSON)', 'Data', FALSE, NULL, 10),
('ReturnRequest', 'total_refund_eligible', 'Total Refund Eligible', 'Number', FALSE, NULL, 11),
('RefundRequest', 'code', 'Refund Request Number', 'Data', TRUE, NULL, 1),
('RefundRequest', 'return_request_id', 'Return Request', 'Link', TRUE, 'ReturnRequest', 2),
('RefundRequest', 'amount', 'Refund Amount', 'Number', TRUE, NULL, 3),
('RefundRequest', 'status', 'Status', 'Select', TRUE, 'Pending,Approved,Processed,Rejected', 4),
('RefundRequest', 'refund_method', 'Refund Method', 'Select', FALSE, 'Original Payment Method,Store Credit,Bank Transfer', 5),
('RefundRequest', 'approved_by', 'Approved By', 'Data', FALSE, NULL, 6),
('RefundRequest', 'processed_by', 'Processed By', 'Data', FALSE, NULL, 7),
('RefundRequest', 'rejection_reason', 'Rejection Reason Code', 'Data', FALSE, NULL, 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Same role shape as SalesOrder/SalesOrderLine (Stage 26.12.1): HR/Admin
-- full CRUD; Store Manager day-to-day read/create/update, no hard delete
-- (a request is Rejected/Closed via the engine, never deleted); Cashier can
-- read/create (initiate a return) but not update/delete.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ReturnRequest', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'RefundRequest', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ReturnRequest', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'RefundRequest', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'ReturnRequest', TRUE, TRUE, FALSE, FALSE),
('Cashier', 'RefundRequest', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
