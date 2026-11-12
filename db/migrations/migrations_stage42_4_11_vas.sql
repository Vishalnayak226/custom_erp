-- ---------------------------------------------------------------------------
-- Stage 42.4.11 - VAS / kitting / production staging as a WarehouseTask
-- type. task_type 'VAS' already exists on WarehouseTask (42.2.1's Select
-- options already list it) - this migration only adds the three optional
-- fields a VAS task needs beyond the base spine: which BOM it consumes
-- (reusing the existing Manufacturing BOM, engines/manufacturing_mrp.go's
-- fetchBOM/explodeBOMComponents - deliberately not a second BOM), how many
-- finished units it is producing, and the LPN code the finished output is
-- grouped into (bin_stock_lpn, the same 26.5.4 mechanism used everywhere
-- else in this tree an LPN needs recording). item/qty/to_bin already exist
-- on WarehouseTask and are reused as "the finished good code / qty / the bin
-- the output lands in" - no separate VASOrder doctype, per the plan text.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('WarehouseTask', 'bom_id', 'BOM (for VAS tasks, optional)', 'Data', FALSE, NULL, 17),
('WarehouseTask', 'output_qty', 'Output Qty (for VAS tasks, optional)', 'Number', FALSE, NULL, 18),
('WarehouseTask', 'output_lpn', 'Output LPN (for VAS tasks, system-set on completion)', 'Data', FALSE, NULL, 19)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
