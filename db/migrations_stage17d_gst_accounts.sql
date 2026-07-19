-- Stage 17.5: GST output-tax liability accounts for checkout's GST split.
-- The existing '1500' GST Input Credit Account already covers the
-- purchase-side (PurchaseOrder gate stores its breakdown on the document
-- itself; PO creation posts no GL entries in this system - GRN receipt does).
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('2200', 'GST Output Payable - CGST', 'Liability'),
('2201', 'GST Output Payable - SGST', 'Liability'),
('2202', 'GST Output Payable - IGST', 'Liability')
ON CONFLICT (account_code) DO NOTHING;
