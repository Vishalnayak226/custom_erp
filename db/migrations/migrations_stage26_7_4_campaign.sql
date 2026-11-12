-- Stage 26.7.4: Campaign definition (birthday/lapsed-customer triggers) +
-- communication log. Campaign is a flat-schema Master (create/list free
-- via the generic doctype machinery). Delivery reuses the existing
-- CleverTap outbound integration (Stage 9.1) - StartCampaignWorker calls
-- the already-built-but-previously-uncalled LogCustomerEventToCleverTap.
-- The communication log is the existing clevertap_event_log/
-- ListCleverTapEventLogs, extended with an additive campaign_id column
-- rather than a new log table.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('Campaign', 'CRM', 'Master', 'crm_loyalty')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Campaign', 'name', 'Campaign Name', 'Data', TRUE, NULL, 1),
('Campaign', 'trigger_type', 'Trigger Type', 'Select', TRUE, 'Birthday,Lapsed Customer', 2),
('Campaign', 'lapsed_days', 'Lapsed After (days, Lapsed Customer only)', 'Number', FALSE, NULL, 3),
('Campaign', 'message_template', 'Message Template', 'Data', TRUE, NULL, 4),
('Campaign', 'cost', 'Campaign Cost (optional, for ROI)', 'Number', FALSE, NULL, 5),
('Campaign', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Campaign', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Campaign', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Additive: birthday-trigger campaigns need a DOB to match against - no
-- such field existed on Customer before this.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Customer', 'date_of_birth', 'Date of Birth', 'Date', FALSE, NULL, 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Additive: which Campaign (if any) triggered this event - lets
-- StartCampaignWorker check "already sent today" without a new table, and
-- lets the campaign-ROI report attribute revenue back to a campaign.
ALTER TABLE tenant_default.clevertap_event_log ADD COLUMN IF NOT EXISTS campaign_id VARCHAR(100);
CREATE INDEX IF NOT EXISTS idx_clevertap_event_log_campaign ON tenant_default.clevertap_event_log (campaign_id, customer_id);
