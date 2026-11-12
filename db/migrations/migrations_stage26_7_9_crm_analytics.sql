-- Stage 26.7.9/26.7.10/26.7.11 (CRM/Loyalty Sprint P2 follow-up): customer
-- householding/merge, CLV/cohort/churn analytics, two-way CleverTap segment
-- sync. Go-ahead given 2026-07-27 for all five P2 bundles previously
-- deferred pending a real pilot customer/measured need.

-- 26.7.9: MergeCustomers (engines/crm_analytics.go) marks the losing
-- record's Customer.status 'Merged' rather than deleting it (the existing
-- append-only audit trigger already logs every reassignment UPDATE this
-- makes to POSCart/SalesInvoice/Voucher/loyalty_point_ledger rows) and
-- stamps merged_into so a lookup on the old id can still resolve forward.
UPDATE tenant_default.doctype_fields
SET options = 'Active,Inactive,Merged'
WHERE doctype_name = 'Customer' AND fieldname = 'status' AND options = 'Active,Inactive';

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Customer', 'merged_into', 'Merged Into Customer ID', 'Data', FALSE, NULL, 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 26.7.11: two-way CleverTap segment sync - additive Customer field storing
-- the segment names CleverTap last pushed for this customer via the new
-- inbound webhook (engines/clevertap.go's ReceiveCleverTapSegmentSync).
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Customer', 'clevertap_segments', 'CleverTap Segments (synced)', 'Data', FALSE, NULL, 12)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
