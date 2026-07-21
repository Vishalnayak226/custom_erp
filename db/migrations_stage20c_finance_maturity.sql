-- Stage 20 Track B.3: Finance Maturity (20.25-20.29, 20.32-20.34).
-- Additive-only, same generic Master/Transaction-doctype registration
-- pattern as migrations_stage20a/20b. 20.30 (e-invoice/IRN) and 20.31
-- (e-way bill) are NOT in this file - both are explicitly blocked on real
-- GSP/government API credentials per docs/micro_checklist.md and stay [ ].

-- 20.25: BankAccount master - one row per bank account this business holds,
-- linked to the GL cash/bank account code it settles into (existing 1100,
-- or a future dedicated code) so reconciliation (20.26) knows which
-- gl_postings to match against.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('BankAccount', 'Finance', 'Master', 'finance')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('BankAccount', 'bank_name', 'Bank Name', 'Data', TRUE, NULL, 1),
('BankAccount', 'account_number', 'Account Number', 'Data', TRUE, NULL, 2),
('BankAccount', 'ifsc_code', 'IFSC Code', 'Data', FALSE, NULL, 3),
('BankAccount', 'branch', 'Branch', 'Data', FALSE, NULL, 4),
('BankAccount', 'gl_account_code', 'GL Account Code', 'Data', TRUE, NULL, 5),
('BankAccount', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'BankAccount', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'BankAccount', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 20.25 (statement import): BankStatementLine - one document per imported
-- bank-statement transaction, not a nested-array parent, specifically so
-- the existing Stage 3.3 BulkImportCSV engine can import it with zero new
-- import code - same shape decision Stage 20.20/20.21's CycleCountLine
-- made for the same reason. match_status defaults to blank/Unmatched at
-- read-time in the reconciliation engine (engines/bank_reconciliation.go);
-- CSV imports never need to set it.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('BankStatementLine', 'Finance', 'Transaction', 'finance')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('BankStatementLine', 'bank_account', 'Bank Account', 'Link', TRUE, 'BankAccount', 1),
('BankStatementLine', 'txn_date', 'Transaction Date', 'Date', TRUE, NULL, 2),
('BankStatementLine', 'description', 'Description', 'Data', FALSE, NULL, 3),
('BankStatementLine', 'reference_number', 'Reference Number', 'Data', FALSE, NULL, 4),
('BankStatementLine', 'amount', 'Amount', 'Number', TRUE, NULL, 5),
('BankStatementLine', 'dr_cr', 'Debit/Credit', 'Select', TRUE, 'Debit,Credit', 6),
('BankStatementLine', 'match_status', 'Match Status', 'Select', FALSE, 'Unmatched,Matched', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'BankStatementLine', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'BankStatementLine', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 20.26: bank reconciliation matches a BankStatementLine to the gl_postings
-- row it corresponds to. gl_postings is a real dedicated SQL table (not a
-- generic JSONB document), so this needs a real additive column rather than
-- a data-key convention - nullable, no default behavior change for any
-- existing posting/query.
ALTER TABLE tenant_default.gl_postings ADD COLUMN IF NOT EXISTS matched_statement_line_id VARCHAR(100);

-- 20.27: PaymentProposal - groups multiple Matched VendorInvoices into one
-- payment run. Registered read-only (no role gets generic create/update),
-- same reasoning as Stage 20.7's POSSession: every write must go through
-- the dedicated endpoints in engines/payment_proposal.go so total_amount is
-- always server-computed from the invoices it actually paid, never
-- client-supplied.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('PaymentProposal', 'Finance', 'Transaction', 'finance')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PaymentProposal', 'proposal_number', 'Proposal Number', 'Data', TRUE, NULL, 1),
('PaymentProposal', 'invoice_ids', 'Invoice IDs (JSON)', 'Data', TRUE, NULL, 2),
('PaymentProposal', 'total_amount', 'Total Amount', 'Number', TRUE, NULL, 3),
('PaymentProposal', 'status', 'Status', 'Select', TRUE, 'Draft,Executed', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PaymentProposal', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'PaymentProposal', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 20.28: TDSSection master - configurable section/threshold/rate table
-- (calc-only, matching how Stage 13.10/17.5 scoped GST - no e-filing), plus
-- the liability account TDS withheld on a vendor payment settles into.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('TDSSection', 'Finance', 'Master', 'finance')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('TDSSection', 'section_code', 'Section Code', 'Data', TRUE, NULL, 1),
('TDSSection', 'description', 'Description', 'Data', FALSE, NULL, 2),
('TDSSection', 'threshold_amount', 'Threshold Amount', 'Number', TRUE, NULL, 3),
('TDSSection', 'rate_percent', 'Rate (%)', 'Number', TRUE, NULL, 4),
('TDSSection', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'TDSSection', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'TDSSection', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Seed common Indian TDS sections (thresholds/rates are the standard
-- statutory defaults; admins can retune via the generic doctype-table edit
-- like any other master, same as approval_rules/prefix_configs).
INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
('194C', 'TDSSection', '{"id":"194C","code":"194C","section_code":"194C","description":"Payments to Contractors","threshold_amount":30000,"rate_percent":1,"status":"Active"}', 'Active', 'system'),
('194J', 'TDSSection', '{"id":"194J","code":"194J","section_code":"194J","description":"Professional/Technical Fees","threshold_amount":30000,"rate_percent":10,"status":"Active"}', 'Active', 'system'),
('194Q', 'TDSSection', '{"id":"194Q","code":"194Q","section_code":"194Q","description":"Purchase of Goods","threshold_amount":5000000,"rate_percent":0.1,"status":"Active"}', 'Active', 'system')
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('2300', 'TDS Payable Account', 'Liability')
ON CONFLICT (account_code) DO NOTHING;

-- 20.32: DebitNote (vendor-facing, e.g. purchase return/overbilling
-- correction - reduces what we owe) and CreditNote (customer-facing, e.g.
-- sales return/goodwill adjustment - reduces revenue recognized), each
-- linked to the original document and GL-reversing via new contra accounts
-- rather than misusing 1200/5100's sale-specific COGS meaning.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('DebitNote', 'Finance', 'Transaction', 'finance'),
('CreditNote', 'Finance', 'Transaction', 'finance')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('DebitNote', 'note_number', 'Note Number', 'Data', TRUE, NULL, 1),
('DebitNote', 'vendor_id', 'Vendor', 'Link', TRUE, 'Vendor', 2),
('DebitNote', 'reference_po', 'Reference PO', 'Data', FALSE, NULL, 3),
('DebitNote', 'amount', 'Amount', 'Number', TRUE, NULL, 4),
('DebitNote', 'reason', 'Reason', 'Data', TRUE, NULL, 5),
('DebitNote', 'status', 'Status', 'Select', TRUE, 'Draft,Posted', 6),
('CreditNote', 'note_number', 'Note Number', 'Data', TRUE, NULL, 1),
('CreditNote', 'customer_id', 'Customer', 'Data', FALSE, NULL, 2),
('CreditNote', 'reference_cart', 'Reference Cart', 'Data', FALSE, NULL, 3),
('CreditNote', 'amount', 'Amount', 'Number', TRUE, NULL, 4),
('CreditNote', 'reason', 'Reason', 'Data', TRUE, NULL, 5),
('CreditNote', 'status', 'Status', 'Select', TRUE, 'Draft,Posted', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'DebitNote', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'DebitNote', TRUE, TRUE, FALSE, FALSE),
('HR/Admin', 'CreditNote', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'CreditNote', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('5150', 'Purchase Returns & Allowances', 'Expense'),
('4150', 'Sales Returns & Allowances', 'Revenue')
ON CONFLICT (account_code) DO NOTHING;

-- 20.33: Receivables Ageing needs a real outstanding-receivable source.
-- SalesInvoice has existed since Stage 1 (db/migration.sql) as a registered
-- Transaction doctype with a Draft/Approved/Paid/Cancelled status flow, but
-- was never wired to an amount field, GL postings, or a frontend - a
-- dormant shell, not a working credit-sales flow. Rather than invent a
-- parallel doctype, this closes that gap directly: adds the missing amount
-- field so engines/sales_invoice.go (new) can post real AR on "Approved"
-- and settle it on "Paid", giving GetReceivablesAgeingReport the same kind
-- of real, mutable-over-time balance GetPayablesAgeingReport already reads
-- off PurchaseOrder's "Approved" status.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SalesInvoice', 'total_amount', 'Total Amount', 'Number', TRUE, NULL, 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Store Manager', 'SalesInvoice', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
