-- Stage 17.8: VendorInvoice + 3-way match (PO/GRN/Invoice) + payment.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('VendorInvoice', 'Procurement', 'Transaction', 'procurement')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('VendorInvoice', 'invoice_number', 'Invoice Number', 'Data', TRUE, NULL, 1),
('VendorInvoice', 'vendor_id', 'Vendor Code', 'Link', TRUE, 'Vendor', 2),
('VendorInvoice', 'po_id', 'PO Reference', 'Link', TRUE, 'PurchaseOrder', 3),
('VendorInvoice', 'grn_id', 'GRN Reference', 'Link', TRUE, 'GRN', 4),
('VendorInvoice', 'invoice_amount', 'Invoice Amount', 'Number', TRUE, NULL, 5),
('VendorInvoice', 'financial_year', 'Financial Year', 'Data', TRUE, NULL, 6),
('VendorInvoice', 'status', 'Status', 'Select', TRUE, 'Draft,Matched,MismatchHold,Approved,Paid', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'VendorInvoice', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'VendorInvoice', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Closes the duplicate-vendor-invoice loophole at the database level, not
-- just in application code - same vendor + invoice number + financial year
-- can only exist once. A partial expression index since doctype/data are
-- shared columns across every doctype in this generic-document schema.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vendor_invoice_dedup
    ON tenant_default.documents ((data->>'vendor_id'), (data->>'invoice_number'), (data->>'financial_year'))
    WHERE doctype = 'VendorInvoice';
