-- ---------------------------------------------------------------------------
-- Stage 52 - Category-based sticker/label templates.
--
-- StickerTemplate lets a tenant define its own label layout (a JSON array of
-- positioned elements, authored via the drag-and-drop designer in the
-- Sticker Printing > Templates tab) and map it to one or more free-text
-- Item.category values (Item.category has no master/taxonomy behind it -
-- db/migration.sql:345 - so `categories` here is deliberately a plain
-- comma-separated Data field matched case-insensitively against whatever a
-- clerk actually typed on the Item, not a Link to something that doesn't
-- exist). `is_default` marks at most one template as the fallback used when
-- an item's category matches nothing configured; when neither a category
-- match nor a default exists, engines.ResolveStickerTemplate returns nil and
-- the caller keeps the pre-Stage-52 hardcoded 3-line label untouched - this
-- feature is purely additive, a tenant that never opens the Templates tab
-- sees no behavior change at all.
--
-- `elements` is JSONTable with no column spec (options left NULL), which
-- validateJSONTableValue (engines/doctype.go) checks is a JSON array of
-- objects but does not force a fixed shape on - exactly right, since a
-- static-text element carries a `text` field a field-bound element doesn't.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('StickerTemplate', 'Inventory', 'stickers', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('StickerTemplate', 'code', 'Template Code', 'Data', TRUE, NULL, 1),
('StickerTemplate', 'name', 'Template Name', 'Data', TRUE, NULL, 2),
('StickerTemplate', 'categories', 'Categories (comma-separated, matched against Item > Category)', 'Data', FALSE, NULL, 3),
('StickerTemplate', 'is_default', 'Default Template (used when no category matches)', 'Check', FALSE, NULL, 4),
('StickerTemplate', 'label_width_mm', 'Label Width (mm)', 'Number', TRUE, NULL, 5),
('StickerTemplate', 'label_height_mm', 'Label Height (mm)', 'Number', TRUE, NULL, 6),
('StickerTemplate', 'elements', 'Layout Elements (JSON)', 'JSONTable', FALSE, NULL, 7),
('StickerTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'StickerTemplate', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'StickerTemplate', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'StickerTemplate', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Additive audit-trail columns: which document a sticker run was printed
-- from, and which template rendered each label - all nullable so the
-- existing manual-SKU-scan flow (which sets none of these) keeps writing
-- rows exactly as before.
ALTER TABLE tenant_default.sticker_print_log
    ADD COLUMN IF NOT EXISTS source_doctype VARCHAR(50),
    ADD COLUMN IF NOT EXISTS source_doc_id VARCHAR(100),
    ADD COLUMN IF NOT EXISTS template_id VARCHAR(100);
