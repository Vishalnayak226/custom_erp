-- Stage 26.9: Manufacturing/MRP Maturity Sprint (26.9.1-26.9.8).
-- Additive-only, extends Stage 13.13e's scoped-MVP single-level BOM +
-- linear Production Order (Draft -> Material Issued -> Completed) rather
-- than replacing it. 26.9.9 (finite/infinite capacity scheduling,
-- subcontracting) stays out of scope per the checklist's own [P2] note.

-- ============================================================
-- 26.9.1/26.9.2/26.9.4: BOM additive fields.
-- is_default/effective_from/effective_to back GetActiveBOMForItem
-- (alternate BOM + effective-dating); by_products/qc_required/standard_cost
-- back finishProductionQty's by-product posting, QC gate, and the
-- production-cost-variance report. Multi-level BOM (sub_bom) and per-line
-- scrap (scrap_percent) live inside the existing "components" JSON field
-- itself (bomComponent struct, engines/manufacturing.go) - no schema change
-- needed for those two.
-- ============================================================
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('BOM', 'is_default', 'Default BOM for this Item', 'Select', FALSE, 'Yes,No', 5),
('BOM', 'effective_from', 'Effective From', 'Date', FALSE, NULL, 6),
('BOM', 'effective_to', 'Effective To', 'Date', FALSE, NULL, 7),
('BOM', 'by_products', 'By-Products JSON ([{sku, qty_per_unit}])', 'Data', FALSE, NULL, 8),
('BOM', 'qc_required', 'Quality Inspection Required', 'Select', FALSE, 'Yes,No', 9),
('BOM', 'standard_cost', 'Standard Cost per Unit', 'Number', FALSE, NULL, 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ============================================================
-- 26.9.3/26.9.6/26.9.2/26.9.8: ProductionOrder additive fields + status
-- vocabulary widened to add "In Process" (WIP). Existing Draft/Material
-- Issued/Completed values and every existing ProductionOrder row are
-- unaffected - this only adds a new allowed value to the Select field.
-- ============================================================
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductionOrder', 'routing_id', 'Routing (optional)', 'Link', FALSE, 'Routing', 6),
('ProductionOrder', 'completed_qty', 'Completed Quantity', 'Number', FALSE, NULL, 7),
('ProductionOrder', 'rework_qty', 'Rework Quantity', 'Number', FALSE, NULL, 8),
('ProductionOrder', 'actual_cost', 'Actual Cost', 'Number', FALSE, NULL, 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

UPDATE tenant_default.doctype_fields
SET options = 'Draft,Material Issued,In Process,Completed'
WHERE doctype_name = 'ProductionOrder' AND fieldname = 'status' AND options = 'Draft,Material Issued,Completed';

-- ============================================================
-- 26.9.3: Work Centers + Routing - a new doctype pair feeding the existing
-- Production Order (routing_id above), same "Master doctype gets a free
-- Setup-submenu entry" precedent as every other master this Stage adds.
-- ============================================================
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('WorkCenter', 'Manufacturing', 'manufacturing', 'Master'),
('Routing', 'Manufacturing', 'manufacturing', 'Master'),
('QualityInspection', 'Manufacturing', 'manufacturing', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('WorkCenter', 'code', 'Work Center Code', 'Data', TRUE, NULL, 1),
('WorkCenter', 'name', 'Name', 'Data', TRUE, NULL, 2),
('WorkCenter', 'capacity_hours_per_day', 'Capacity (Hours/Day)', 'Number', FALSE, NULL, 3),
('WorkCenter', 'cost_per_hour', 'Cost per Hour', 'Number', FALSE, NULL, 4),
('WorkCenter', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),

('Routing', 'code', 'Routing Code', 'Data', TRUE, NULL, 1),
('Routing', 'item', 'Finished Good SKU', 'Data', TRUE, NULL, 2),
('Routing', 'operations', 'Operations JSON ([{seq, operation_name, work_center_id, setup_time_mins, run_time_mins_per_unit}])', 'Data', TRUE, NULL, 3),
('Routing', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4),

-- 26.9.7: QC gate before a Production Order can complete, reusing the
-- existing generic approval engine (SubmitForApproval/DecideApproval) via
-- this doctype's own status field, same as every other approval-gated
-- Transaction doctype - no new mechanism needed.
('QualityInspection', 'code', 'Inspection Code', 'Data', TRUE, NULL, 1),
('QualityInspection', 'production_order_id', 'Production Order', 'Link', TRUE, 'ProductionOrder', 2),
('QualityInspection', 'result', 'Result', 'Select', TRUE, 'Pass,Fail', 3),
('QualityInspection', 'remarks', 'Remarks', 'Data', FALSE, NULL, 4),
('QualityInspection', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'WorkCenter', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'Routing', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'QualityInspection', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'WorkCenter', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'Routing', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'QualityInspection', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- A single flat 0..NULL approval rule, same convention 24.11's VendorInvoice
-- override used for a doctype with no natural "amount" - extractAmount
-- returns 0 for QualityInspection, so this one rule always matches.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role)
SELECT 'QualityInspection', 0, NULL, 'Store Manager'
WHERE NOT EXISTS (SELECT 1 FROM tenant_default.approval_rules WHERE doctype = 'QualityInspection');
