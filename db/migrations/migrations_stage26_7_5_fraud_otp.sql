-- Stage 26.7.5: fraud/staff-restriction rules + OTP redemption on loyalty
-- burn. LoyaltyRedemptionRequest is the staff-restriction gate (a flat-
-- schema Transaction doctype - create/list reuse the generic doc
-- machinery; only VerifyAndRedeemLoyaltyOTP's own insert and the
-- DecideApproval hook that redeems it need bespoke code).
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('LoyaltyRedemptionRequest', 'CRM', 'Transaction', 'crm_loyalty')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('LoyaltyRedemptionRequest', 'customer_id', 'Customer', 'Data', TRUE, NULL, 1),
('LoyaltyRedemptionRequest', 'points', 'Points', 'Number', TRUE, NULL, 2),
('LoyaltyRedemptionRequest', 'points_value', 'Redemption Value', 'Number', TRUE, NULL, 3),
('LoyaltyRedemptionRequest', 'reference_id', 'Reference', 'Data', FALSE, NULL, 4),
('LoyaltyRedemptionRequest', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Redeemed', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'LoyaltyRedemptionRequest', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'LoyaltyRedemptionRequest', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Redemptions worth 500+ rupees need a Store Manager's sign-off
-- (VerifyAndRedeemLoyaltyOTP routes here) before the points actually burn;
-- below that, no rule matches and it redeems immediately once the OTP
-- checks out. Tenant-adjustable later via the existing approval_rules
-- admin screen, same as every other amount-slab doctype.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('LoyaltyRedemptionRequest', 500, NULL, 'Store Manager')
ON CONFLICT (doctype, min_amount) DO NOTHING;

-- Additive: "Loyalty Redemption OTP" joins NotificationTemplate.event's
-- existing option list (appended, not replaced, same convention Stage
-- 26.5.10's ReasonCode-category append already used) so a tenant can
-- configure a template for InitiateSecureLoyaltyRedemption's dispatch.
UPDATE tenant_default.doctype_fields
SET options = options || ',Loyalty Redemption OTP'
WHERE doctype_name = 'NotificationTemplate' AND fieldname = 'event' AND options NOT LIKE '%Loyalty Redemption OTP%';

-- OTP challenges are short-lived and never need cross-tenant querying, so
-- a small dedicated table (not a generic doctype) - never stores the
-- plaintext code, only its SHA-256 hash.
CREATE TABLE IF NOT EXISTS tenant_default.loyalty_redemption_otp_challenges (
    id VARCHAR(100) PRIMARY KEY,
    customer_id VARCHAR(100) NOT NULL,
    points INT NOT NULL,
    reference_id VARCHAR(100),
    initiated_by VARCHAR(100) NOT NULL,
    otp_hash VARCHAR(64) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
