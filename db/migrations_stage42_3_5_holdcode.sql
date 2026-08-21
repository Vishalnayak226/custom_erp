-- ---------------------------------------------------------------------------
-- Stage 42.3.5 - HoldCode master + hold/release workflow.
--
-- Placing a hold (PlaceHold, engines/wms_holds.go) is immediate - no
-- approval - because a hold is a defensive action; gating it would leave a
-- known-bad lot sellable while a checker's queue caught up. Releasing one is
-- the dangerous direction, so HoldReleaseRequest is a second, ordinary
-- doctype riding the existing generic maker-checker engine unchanged
-- (approval_rules + Submit/Decide), the same "the request to act is a
-- separate doctype from the thing it acts on" shape LoyaltyRedemptionRequest
-- already uses (migrations_stage26_7_5_fraud_otp.sql). DecideApproval
-- (engines/approval.go) gets one new `doctype == "HoldReleaseRequest"`
-- branch to call ReleaseHold on Approved, mirroring its existing branches
-- for JournalVoucher/LoyaltyRedemptionRequest/SupplierSubmission.
--
-- hold_qty is a new aggregate column on inventory_availability, updated
-- directly by PlaceHold/ReleaseHold exactly the way qc_hold and blocked
-- already are (wms_receiving.go, returns.go) - not derived by a join, so
-- every existing ATS reader keeps working by simply gaining one more
-- subtracted term in computeATS (engines/inventory.go) rather than needing
-- to know Hold exists at all.
-- ---------------------------------------------------------------------------
ALTER TABLE tenant_default.inventory_availability ADD COLUMN IF NOT EXISTS hold_qty INT NOT NULL DEFAULT 0;

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('HoldCode', 'Inventory', 'inventory', 'Master'),
('Hold', 'Inventory', 'inventory', 'Transaction'),
('HoldReleaseRequest', 'Inventory', 'inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('HoldCode', 'code', 'Hold Code', 'Data', TRUE, NULL, 1),
('HoldCode', 'description', 'Description (optional)', 'Data', FALSE, NULL, 2),
('HoldCode', 'category', 'Category', 'Select', TRUE, 'Quality,Compliance,Damage,Investigation,Other', 3),
('HoldCode', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4),

('Hold', 'hold_code', 'Hold Code', 'Data', TRUE, NULL, 1),
('Hold', 'sku', 'SKU', 'Data', TRUE, NULL, 2),
('Hold', 'location_code', 'Location', 'Data', TRUE, NULL, 3),
('Hold', 'batch_no', 'Batch / Lot No (optional)', 'Data', FALSE, NULL, 4),
('Hold', 'qty', 'Quantity Held', 'Number', TRUE, NULL, 5),
('Hold', 'reason', 'Reason (optional)', 'Data', FALSE, NULL, 6),
('Hold', 'status', 'Status', 'Select', TRUE, 'Active,Released', 7),

('HoldReleaseRequest', 'hold_id', 'Hold Reference', 'Link', TRUE, 'Hold', 1),
('HoldReleaseRequest', 'reason', 'Release Reason (optional)', 'Data', FALSE, NULL, 2),
('HoldReleaseRequest', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 3)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'HoldCode', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'HoldCode', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'HoldCode', TRUE, FALSE, FALSE, FALSE),
-- Hold is deliberately not creatable/editable through the generic doc
-- endpoint (allow_create/allow_update FALSE for every role, including
-- HR/Admin) - it is only ever written by PlaceHold (its own POST
-- /api/v1/wms/hold/place action, engines/wms_holds.go, the same "an
-- inventory-affecting write is a dedicated action, not a bare field edit"
-- shape PutawayToBin already sets) and by ReleaseHold, itself only reachable
-- through an approved HoldReleaseRequest. Read-only here so it still shows
-- up in the ordinary doctype-table browser.
('HR/Admin',       'Hold', TRUE, FALSE, FALSE, FALSE),
('Store Manager',  'Hold', TRUE, FALSE, FALSE, FALSE),
('Cashier',        'Hold', TRUE, FALSE, FALSE, FALSE),
('HR/Admin',       'HoldReleaseRequest', TRUE, TRUE, TRUE, FALSE),
('Store Manager',  'HoldReleaseRequest', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Flat gate, same shape as LoyaltyRedemptionRequest's: every release request
-- requires a Store Manager decision regardless of held quantity (Hold has no
-- monetary amount for the amount-slab check to key off).
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('HoldReleaseRequest', 0, NULL, 'Store Manager')
ON CONFLICT (doctype, min_amount) DO NOTHING;
