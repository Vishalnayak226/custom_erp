-- Stage 26.6.8: Intercompany/cost-center/profit-center postings and
-- reports - extends Stage 17.9's Department/CostCenter masters (which had
-- zero validation function and were never referenced in postings before
-- this) into finance postings. Additive/nullable, so all 28 pre-existing
-- PostDoubleEntry call sites are unaffected - only the new JournalVoucher
-- (Stage 26.6.4) passes a value initially.
ALTER TABLE tenant_default.gl_postings ADD COLUMN IF NOT EXISTS cost_center VARCHAR(50);
ALTER TABLE tenant_default.gl_postings ADD COLUMN IF NOT EXISTS department VARCHAR(50);

-- Additive: an optional whole-voucher cost center/department (not
-- per-line - PostDoubleEntry aggregates debits/credits per account_code
-- into one row per account, so a per-line dimension isn't representable
-- without restructuring that aggregation; whole-voucher is the documented
-- scope for this pass, matching many real small-business GL systems that
-- tag the journal entry itself rather than every individual line).
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('JournalVoucher', 'cost_center', 'Cost Center', 'Link', FALSE, 'CostCenter', 8),
('JournalVoucher', 'department', 'Department', 'Link', FALSE, 'Department', 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
