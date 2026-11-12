-- Stage 26.4: PIM/PXM Maturity Sprint (26.4.1-26.4.9, docs/micro_checklist.md).
-- Every item here extends the existing PIM module (module_key='pim', Stage
-- 15/15.2/16.1) - no parallel PIM model, per CLAUDE.md's "reuse the one
-- choke point" principle. All changes are additive (new optional columns,
-- new doctypes, new system tables) - nothing here can break an existing
-- Item/ProductContent/ProductMedia/Channel row.

-- 26.4.1: Attribute groups (organizes ProductAttributeDef for the UI) +
-- locale/channel-scoped ProductAttributeValue overrides. A value row with
-- blank locale/channel is the global default (unchanged from Stage 15); a
-- row with either set is a scoped override resolved by
-- engines.ResolveAttributeValue (most-specific match wins, falls back to
-- the global row) - see engines/pim.go.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ProductAttributeGroup', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductAttributeGroup', 'code', 'Group Code', 'Data', TRUE, NULL, 1),
('ProductAttributeGroup', 'name', 'Group Name', 'Data', TRUE, NULL, 2),
('ProductAttributeGroup', 'display_order', 'Display Order', 'Number', FALSE, NULL, 3),
('ProductAttributeGroup', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductAttributeDef', 'group', 'Attribute Group', 'Link', FALSE, 'ProductAttributeGroup', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductAttributeValue', 'locale', 'Locale Override (blank = all locales)', 'Data', FALSE, NULL, 6),
('ProductAttributeValue', 'channel', 'Channel Override (blank = all channels)', 'Link', FALSE, 'Channel', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ProductAttributeGroup', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ProductAttributeGroup', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 26.4.4: Media versioning (version_no now genuinely increments per
-- item+role, see engines.SaveMediaFile) + alt text + expiry + a thumbnail
-- rendition flag (thumbnails are generated server-side for jpg/png only,
-- pure stdlib image/jpeg+image/png, no new dependency - see
-- engines/pim_media.go's header note on the webp/gif/pdf scope limit).
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductMedia', 'alt_text', 'Alt Text', 'Data', FALSE, NULL, 12),
('ProductMedia', 'expiry_date', 'Expiry Date (YYYY-MM-DD)', 'Data', FALSE, NULL, 13),
('ProductMedia', 'has_thumbnail', 'Has Thumbnail', 'Select', FALSE, 'Yes,No', 14)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 26.4.5: Content workflow - owner assignment + SLA due date. "owner" is a
-- plain username Data field, same convention ReportFilterPreset.owner
-- already uses (migrations_stage20d_reports_engine.sql) - there is no
-- generic "User" link target doctype to point at instead. Rejection
-- comments already exist (approval_log.comment, APPROV-0159 makes them
-- mandatory) - engines.ListApprovalLog (new) surfaces that existing data,
-- no new column needed for that part.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductContent', 'owner', 'Owner (Username)', 'Data', FALSE, NULL, 10),
('ProductContent', 'sla_due_date', 'SLA Due Date (YYYY-MM-DD)', 'Data', FALSE, NULL, 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 26.4.6: Content version snapshots, taken on every successful "Approved"
-- decision (engines.DecideApproval) so a bad later edit can be rolled back
-- to the last known-good approved copy. A dedicated system-written table,
-- not a doctype - same reasoning as pim_publish_queue/log (Stage 15.2):
-- never authored directly by a user.
CREATE TABLE IF NOT EXISTS tenant_default.product_content_versions (
    id SERIAL PRIMARY KEY,
    content_id VARCHAR(100) NOT NULL,
    version_no INT NOT NULL,
    data JSONB NOT NULL,
    status VARCHAR(20) NOT NULL,
    saved_by VARCHAR(100),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_product_content_versions_content ON tenant_default.product_content_versions (content_id, version_no DESC);

-- 26.4.7: Channel validation packs (business rules beyond simple field
-- presence, e.g. minimum image count / title length / a required tag) +
-- payload snapshot/error code on the publish log so a later publish
-- attempt can show a real diff against what was last sent, and so a
-- classified connector error code survives as its own column instead of
-- only ever being embedded in the free-text error_message (26.4.8).
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ChannelValidationRule', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ChannelValidationRule', 'code', 'Code', 'Data', TRUE, NULL, 1),
('ChannelValidationRule', 'channel', 'Channel', 'Link', TRUE, 'Channel', 2),
('ChannelValidationRule', 'rule_type', 'Rule Type', 'Select', TRUE, 'Min Images,Max Title Length,Required Tag', 3),
('ChannelValidationRule', 'rule_value', 'Rule Value', 'Data', TRUE, NULL, 4),
('ChannelValidationRule', 'message', 'Failure Message (optional)', 'Data', FALSE, NULL, 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ChannelValidationRule', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ChannelValidationRule', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

ALTER TABLE tenant_default.pim_publish_log ADD COLUMN IF NOT EXISTS payload_snapshot JSONB;
ALTER TABLE tenant_default.pim_publish_log ADD COLUMN IF NOT EXISTS error_code VARCHAR(20);
