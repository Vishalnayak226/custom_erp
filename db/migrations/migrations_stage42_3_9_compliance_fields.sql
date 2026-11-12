-- ---------------------------------------------------------------------------
-- Stage 42.3.9 - Compliance fields: COOL (Country of Origin Labeling), GTIN,
-- hazmat class on Item, plus the putaway compatibility check against Zone's
-- existing hazmat_allowed flag (42.2.5). Item.data/Zone.data are JSONB, so
-- both are doctype_fields rows only - no ALTER TABLE.
--
-- The compatibility check lives in PutawayToBin itself (engines/wms.go), the
-- one choke point every putaway (manual, cross-dock, planned cross-dock)
-- already runs through - not a new parallel check per caller. hazmat_class
-- 'None' (the default for every existing item) never triggers it, so this is
-- purely additive for a tenant that never classifies an item as hazmat.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'gtin', 'GTIN (optional)', 'Data', FALSE, NULL, 41),
('Item', 'country_of_origin', 'Country of Origin (optional)', 'Data', FALSE, NULL, 42),
('Item', 'hazmat_class', 'Hazmat Class (optional)', 'Select', FALSE,
 'None,Class 1 Explosives,Class 2 Gases,Class 3 Flammable Liquids,Class 4 Flammable Solids,Class 5 Oxidizers,Class 6 Toxic,Class 7 Radioactive,Class 8 Corrosive,Class 9 Miscellaneous', 43)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
