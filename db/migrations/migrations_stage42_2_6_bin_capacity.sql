-- ---------------------------------------------------------------------------
-- Stage 42.2.6 - Bin capacity + attributes, enforced on putaway.
--
-- `capacity` (a plain Number, 20.16) already exists and is already read as a
-- max-qty ceiling by wms_slotting.go's suggestion logic - this item reuses
-- it as `max_qty` rather than adding a duplicate field with the same
-- meaning under a different name, per the plan's own "reuse over duplicate"
-- rule. What's new: `max_weight`/`max_volume` (checked against Item.weight/
-- Item.volume, both of which already exist on Item - migration.sql - but
-- had no reader anywhere in this codebase until PutawayToBin's own change),
-- and `bin_status` (Available/Blocked/Full/Counting), an *operational*
-- state distinct from the existing `status` field (Active/Inactive, the
-- master-record enable/disable flag every doctype has) - a bin can be
-- Active as a record and still be temporarily Blocked or mid-cycle-count.
--
-- All three are optional and additive: a bin that never sets them keeps
-- putaway behaving exactly as it did before this migration (PutawayToBin's
-- capacity check is a no-op when max_qty/max_weight/max_volume are all
-- unset, and a blank bin_status is treated as Available).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Bin', 'max_weight', 'Max Weight (optional)', 'Number', FALSE, NULL, 8),
('Bin', 'max_volume', 'Max Volume (optional)', 'Number', FALSE, NULL, 9),
('Bin', 'bin_status', 'Bin Status (optional)', 'Select', FALSE, 'Available,Blocked,Full,Counting', 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
