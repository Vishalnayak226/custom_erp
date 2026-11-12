-- ---------------------------------------------------------------------------
-- Stage 42.4.4 - Cartonization v2: dimensional and weight-aware, replacing
-- SuggestCartonization's qty-capacity-only first-fit-decreasing with
-- SuggestCartonizationV2 (engines/wms_cartonization_v2.go), which also
-- respects a carton's weight and volume ceiling.
--
-- "Dimensional" here means volume, not three separate L/W/H fields.
-- Item.volume already exists (added for 42.2.6's enforceBinCapacity) and a
-- first-fit-decreasing packer only ever needs a single scalar capacity per
-- axis to bin-pack against - true 3D nesting (does this box's shape fit
-- along with that one) is out of scope for both SuggestCartonization and
-- this v2, same as it always was; only the packing rule (qty-only vs
-- qty+weight+volume) changed. Item.weight also already exists (Stage-1-era
-- field). Both are optional, so an item never given a volume/weight is
-- packed exactly as SuggestCartonization always packed it - v1 stays
-- untouched and is still what /wms/cartonization/suggest calls.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CartonType', 'max_weight_capacity', 'Max Weight Capacity (optional)', 'Number', FALSE, NULL, 5),
('CartonType', 'max_volume_capacity', 'Max Volume Capacity (optional)', 'Number', FALSE, NULL, 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
