-- ---------------------------------------------------------------------------
-- Stage 49.2.4 (last two open clauses) - admin-assisted "helpdesk" password
-- reset, closing the gap the 2026-09-08 handover note names explicitly:
-- there was no admin-driven way to reset another user's password at all -
-- only self-service change (24.x) and self-service emailed-token reset
-- (24.28). handleAdminResetUserMFA already had the single-admin shape for
-- MFA; this gives password the same shape, but for a PRIVILEGED target (a
-- Super Admin account) it requires a second, different Super Admin to
-- approve first - "auditable dual control for privileged users", the exact
-- wording of the still-open clause.
--
-- PasswordResetRequest rides the existing generic maker-checker engine
-- unchanged (approval_rules + Submit/Decide), the same "the request to act
-- is a separate doctype from the thing it acts on" shape HoldReleaseRequest
-- and LoyaltyRedemptionRequest already use (migrations_stage42_3_5_holdcode.sql,
-- migrations_stage26_7_5_fraud_otp.sql). An ordinary (non-Super-Admin) target
-- is still a single immediate admin action - engines.RequestAdminPasswordReset
-- inserts the row already Approved in that case, purely for an evidence
-- trail, and never calls SubmitForApproval. handleDecideApproval (not
-- DecideApproval itself) runs the actual reset on an Approved decision,
-- because - unlike every other doctype's approval hook - this one has a
-- result (a one-time password) that only the approver, never the requester,
-- may ever see.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PasswordResetRequest', 'HR', 'hr', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PasswordResetRequest', 'target_user_id', 'Target User', 'Link', TRUE, 'User', 1),
('PasswordResetRequest', 'target_username', 'Target Username', 'Data', TRUE, NULL, 2),
('PasswordResetRequest', 'target_role', 'Target Role (at request time)', 'Data', TRUE, NULL, 3),
('PasswordResetRequest', 'reason', 'Reason', 'Data', FALSE, NULL, 4),
('PasswordResetRequest', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Failed', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PasswordResetRequest', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Flat gate, same shape as HoldReleaseRequest's and LoyaltyRedemptionRequest's:
-- every request against a privileged target needs a Super Admin decision
-- regardless of amount (there is none to slab on). required_role is
-- 'Super Admin' mainly for self-documentation - DecideApproval already lets
-- any Super Admin approve regardless of a rule's required_role, and its
-- own maker-checker check is what actually forces a SECOND, different one.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('PasswordResetRequest', 0, NULL, 'Super Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;
