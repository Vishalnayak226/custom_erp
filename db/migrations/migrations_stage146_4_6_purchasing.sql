-- Stage 146.4.6(e) - a default return store per sales channel. Empty = a return with no store is
-- routed as before. Additive and re-runnable.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Channel', 'default_return_location', 'Default Return Store', 'Link', FALSE, 'Location', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
