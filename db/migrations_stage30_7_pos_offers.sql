-- Stage 30.7: POS offer configuration. Offer is a flat-schema Master, so
-- create/list/edit come free from the generic doctype machinery (the same
-- zero-bespoke-code route Campaign took in 26.7.4) - the only new engine
-- logic is engines/pos_offers.go's evaluator, which POS checkout calls.
--
-- One doctype covers all four offer families rather than four doctypes:
-- offer_type selects which of the value fields below actually apply, exactly
-- as Campaign's trigger_type gates lapsed_days. Fields not relevant to the
-- chosen type are simply left blank.
--
--   Percentage Off  -> discount_pct
--   Flat Off        -> discount_amount
--   Buy X Get Y     -> buy_qty + get_qty      (cheapest qualifying lines free)
--   Bundle Price    -> bundle_qty + bundle_price
--
-- Conditions (all optional, AND-ed): min_bill_amount / min_qty give the
-- threshold family, coupon_code makes an offer cashier-entered instead of
-- automatic, customer_tier restricts it to a loyalty tier, and valid_from /
-- valid_to bound it in time.
-- module_key is 'sales', not a new 'pos' key: POSCart and POSSession are both
-- mapped to 'sales', and the module gate (handleGenericDoc's ModuleForDoctype
-- check) refuses any doctype whose key no plan grants. A new key would have
-- made every Offer request 403 with SAAS-0191 on every existing tenant.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('Offer', 'POS', 'Master', 'sales')
ON CONFLICT (name) DO UPDATE SET module_key = EXCLUDED.module_key;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Offer', 'name', 'Offer Name', 'Data', TRUE, NULL, 1),
('Offer', 'offer_type', 'Offer Type', 'Select', TRUE, 'Percentage Off,Flat Off,Buy X Get Y,Bundle Price', 2),
('Offer', 'scope', 'Applies To', 'Select', TRUE, 'Bill,Item,Category', 3),
('Offer', 'scope_value', 'Item SKU / Category (blank for whole bill)', 'Data', FALSE, NULL, 4),
('Offer', 'discount_pct', 'Discount % (Percentage Off)', 'Number', FALSE, NULL, 5),
('Offer', 'discount_amount', 'Discount Amount (Flat Off)', 'Number', FALSE, NULL, 6),
('Offer', 'buy_qty', 'Buy Qty (Buy X Get Y)', 'Number', FALSE, NULL, 7),
('Offer', 'get_qty', 'Free Qty (Buy X Get Y)', 'Number', FALSE, NULL, 8),
('Offer', 'bundle_qty', 'Bundle Qty (Bundle Price)', 'Number', FALSE, NULL, 9),
('Offer', 'bundle_price', 'Bundle Price (Bundle Price)', 'Number', FALSE, NULL, 10),
('Offer', 'min_bill_amount', 'Minimum Bill Amount', 'Number', FALSE, NULL, 11),
('Offer', 'min_qty', 'Minimum Qty', 'Number', FALSE, NULL, 12),
('Offer', 'max_discount_amount', 'Maximum Discount Cap', 'Number', FALSE, NULL, 13),
('Offer', 'coupon_code', 'Coupon Code (blank = applies automatically)', 'Data', FALSE, NULL, 14),
('Offer', 'customer_tier', 'Customer Tier (blank = all customers)', 'Data', FALSE, NULL, 15),
('Offer', 'valid_from', 'Valid From', 'Date', FALSE, NULL, 16),
('Offer', 'valid_to', 'Valid To', 'Date', FALSE, NULL, 17),
('Offer', 'priority', 'Priority (lower applies first)', 'Number', FALSE, NULL, 18),
('Offer', 'stackable', 'Stackable With Other Offers', 'Select', FALSE, 'Yes,No', 19),
('Offer', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 20)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Same role split Campaign uses: admins manage offers, store managers can
-- create/adjust them for their own store, cashiers only ever read (the POS
-- evaluator runs server-side under the cashier's own token).
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Offer', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Offer', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'Offer', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Offer lookups on the checkout path filter Active offers by doctype; this
-- keeps that from degrading into a sequential scan as the catalog grows.
CREATE INDEX IF NOT EXISTS idx_documents_offer_active
  ON tenant_default.documents (doctype, status)
  WHERE doctype = 'Offer';
