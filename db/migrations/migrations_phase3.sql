-- Seed Phase 2 & 3 Transactional Metadata

-- 1. Register DocTypes in doctype_meta
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('PurchaseOrder', 'Procurement', 'Transaction'),
('GRN', 'Procurement', 'Transaction'),
('StockLedgerEntry', 'Inventory', 'Transaction'),
('POSInvoice', 'POS', 'Transaction'),
('GLPost', 'Finance', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- 2. Register fields for PurchaseOrder
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PurchaseOrder', 'code', 'PO Number', 'Data', TRUE, NULL, 1),
('PurchaseOrder', 'vendor_id', 'Vendor Code', 'Data', TRUE, NULL, 2),
('PurchaseOrder', 'items', 'PO Items JSON', 'Data', TRUE, NULL, 3),
('PurchaseOrder', 'total_amount', 'Total Cost', 'Number', TRUE, NULL, 4),
('PurchaseOrder', 'status', 'Approval Status', 'Select', TRUE, 'Draft,Approved,Cancelled', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 3. Register fields for GRN
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('GRN', 'code', 'GRN Number', 'Data', TRUE, NULL, 1),
('GRN', 'po_id', 'PO Reference', 'Link', TRUE, 'PurchaseOrder', 2),
('GRN', 'received_items', 'Received Items JSON', 'Data', TRUE, NULL, 3),
('GRN', 'status', 'Status', 'Select', TRUE, 'Pending,Approved,Cancelled', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 4. Register fields for StockLedgerEntry
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('StockLedgerEntry', 'code', 'Voucher Code', 'Data', TRUE, NULL, 1),
('StockLedgerEntry', 'item_id', 'Item ID', 'Data', TRUE, NULL, 2),
('StockLedgerEntry', 'warehouse_id', 'Warehouse ID', 'Data', TRUE, NULL, 3),
('StockLedgerEntry', 'qty', 'Quantity Delta', 'Number', TRUE, NULL, 4),
('StockLedgerEntry', 'voucher_type', 'Voucher Type', 'Select', TRUE, 'GRN,POSInvoice,StockTransfer', 5),
('StockLedgerEntry', 'voucher_id', 'Voucher Reference ID', 'Data', TRUE, NULL, 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 5. Register fields for POSInvoice
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('POSInvoice', 'code', 'Invoice Number', 'Data', TRUE, NULL, 1),
('POSInvoice', 'customer_phone', 'Customer Mobile', 'Data', TRUE, NULL, 2),
('POSInvoice', 'items', 'Sales Items JSON', 'Data', TRUE, NULL, 3),
('POSInvoice', 'payment_mode', 'Payment Mode', 'Select', TRUE, 'Cash,Card,UPI', 4),
('POSInvoice', 'grand_total', 'Grand Total', 'Number', TRUE, NULL, 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 6. Register fields for GLPost
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('GLPost', 'code', 'Journal Number', 'Data', TRUE, NULL, 1),
('GLPost', 'debit_account', 'Debit Account', 'Data', TRUE, NULL, 2),
('GLPost', 'credit_account', 'Credit Account', 'Data', TRUE, NULL, 3),
('GLPost', 'amount', 'Amount', 'Number', TRUE, NULL, 4),
('GLPost', 'voucher_id', 'Voucher Reference ID', 'Data', TRUE, NULL, 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 7. Add default permissions for HR/Admin and Cashier roles on transactions
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PurchaseOrder', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'GRN', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'StockLedgerEntry', TRUE, TRUE, FALSE, FALSE),
('HR/Admin', 'POSInvoice', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'GLPost', TRUE, TRUE, FALSE, FALSE),
('Cashier', 'POSInvoice', TRUE, TRUE, FALSE, FALSE),
('Cashier', 'StockLedgerEntry', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
