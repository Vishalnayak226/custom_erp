-- ---------------------------------------------------------------------------
-- Stage 42.3.7 - Catch weight + dimensional capture at receipt. Item.data is
-- JSONB (same as every other Item field added post-launch), so this is a
-- doctype_fields row only - no ALTER TABLE, same convention
-- migrations_stage26_5_wms_p2.sql's Bin.owner_id note already explains.
--
-- Capture itself lives on GRN's existing received_items JSON (grnReceivedLine,
-- engines/transactional_validation.go) - actual_weight/weight_uom/length/
-- width/height/dim_uom, all optional there except actual_weight is required
-- (GOODSR-0098) when the line's Item has is_catch_weight = Yes. No new table:
-- 42.4.4/42.6.9 (not yet built) read this straight off the GRN document that
-- received the stock.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'is_catch_weight', 'Catch Weight Item (optional)', 'Select', FALSE, 'Yes,No', 40)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
