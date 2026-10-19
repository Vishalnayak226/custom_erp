-- ---------------------------------------------------------------------------
-- Stage 94.5: the suggested MRP at a goods receipt's MRP step is cost x the
-- category's markup (plus GST, rounded to the price ending). Category gets an
-- optional MRP markup %; blank uses the org-wide one
-- (Configuration > Procurement > Goods receipt: suggested MRP markup).
--
-- Additive: one optional field, into tenant_default (SyncAllTenantSchemas
-- copies it to every client).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Category', 'mrp_markup_pct', 'MRP markup % (suggested MRP at goods receipt)', 'Number', FALSE, NULL, 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
