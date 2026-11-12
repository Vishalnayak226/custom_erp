-- Stage 26.7.2: Voucher/coupon doctype - a flat-schema Master (like
-- Location/Department, Stage 17.9), reachable via the generic doctype
-- table/CSV-import machinery with zero new create/list code. Redemption is
-- the only part that needs bespoke logic (engines/voucher.go).
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('Voucher', 'CRM', 'Master', 'crm_loyalty')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Voucher', 'code', 'Voucher Code', 'Data', TRUE, NULL, 1),
('Voucher', 'discount_type', 'Discount Type', 'Select', TRUE, 'Percentage,Flat', 2),
('Voucher', 'discount_value', 'Discount Value', 'Number', TRUE, NULL, 3),
('Voucher', 'expiry_date', 'Expiry Date', 'Date', FALSE, NULL, 4),
('Voucher', 'max_uses', 'Max Uses', 'Number', FALSE, NULL, 5),
('Voucher', 'customer_id', 'Restricted to Customer (optional)', 'Data', FALSE, NULL, 6),
('Voucher', 'status', 'Status', 'Select', TRUE, 'Active,Inactive,Expired,Exhausted', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Voucher', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Voucher', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Stage 26.7.3: Loyalty tiering + accrual/expiry.
-- loyalty_tier_rules is a small self-service admin config table, same
-- pattern as approval_rules (Stage 24.8) - min_spend -> earn_multiplier.
CREATE TABLE IF NOT EXISTS tenant_default.loyalty_tier_rules (
    id SERIAL PRIMARY KEY,
    tier VARCHAR(50) NOT NULL UNIQUE,
    min_spend NUMERIC NOT NULL DEFAULT 0,
    earn_multiplier NUMERIC NOT NULL DEFAULT 1
);

INSERT INTO tenant_default.loyalty_tier_rules (tier, min_spend, earn_multiplier) VALUES
('Bronze', 0, 1),
('Silver', 5000, 1.25),
('Gold', 20000, 1.5),
('Platinum', 50000, 2)
ON CONFLICT (tier) DO NOTHING;

-- Additive: Customer's current tier (recomputed by engines.RecomputeLoyaltyTier
-- after each earn, off lifetime POSCart spend vs. the thresholds above).
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Customer', 'loyalty_tier', 'Loyalty Tier', 'Select', FALSE, 'Bronze,Silver,Gold,Platinum', 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Additive: when an Earn row's points lapse. NULL for Burn rows (and for
-- Earn rows recorded before this migration) - a lapsed lot is swept by
-- engines.StartLoyaltyExpiryWorker into a normal Burn row (reference_doctype
-- 'LoyaltyExpiry'), not a new transaction_type value, so every existing
-- balance query (GetLoyaltyBalance, the loyalty-summary/points-liability/RFM
-- reports) already treats it correctly with zero changes to any of them.
ALTER TABLE tenant_default.loyalty_point_ledger ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP;
