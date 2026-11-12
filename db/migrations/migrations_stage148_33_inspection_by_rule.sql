-- ---------------------------------------------------------------------------
-- Stage 148.33 + 146.3.11: inspection asked for by rule (setting
-- quality.inspection_by_rule, off by default) and re-inspection by stock age
-- (setting quality.age_inspection, off by default).
--
-- Additive and idempotent, replayed into every tenant schema. No client row is
-- written: only field lists, doctype registrations and role permissions. With
-- both settings off none of the new doctypes or fields is shown or accepted.
--   * Item, ItemType: quality_required (Yes / No) and qc_mode.
--   * InspectionPlan: acceptance_pct, limits (min / max) on each test, and a
--     "Stock age" value for applies_to.
--   * InspectionRule (master), InspectionLot (written by the receipt and the
--     results route only), StockAgeInspection (written by the daily job).
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL
              OR to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL;

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type, strict_status_transitions)
      VALUES ('InspectionRule', 'Quality', 'quality', 'Master', FALSE),
             ('InspectionLot', 'Quality', 'quality', 'Transaction', TRUE),
             ('StockAgeInspection', 'Quality', 'quality', 'Transaction', TRUE)
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('Item', 'quality_required', 'Quality check', 'Select', FALSE, ',Yes,No', 95),
        ('Item', 'qc_mode', 'Check is', 'Select', FALSE, ',QC mandatory,Warehouse may decide', 96),
        ('ItemType', 'quality_required', 'Quality check', 'Select', FALSE, ',Yes,No', 95),
        ('ItemType', 'qc_mode', 'Check is', 'Select', FALSE, ',QC mandatory,Warehouse may decide', 96),
        ('InspectionPlan', 'acceptance_pct', 'Accept lot at (%%)', 'Number', FALSE, NULL, 7),

        ('InspectionRule', 'code', 'Rule', 'Data', TRUE, NULL, 1),
        ('InspectionRule', 'rule_kind', 'Rule for', 'Select', TRUE, 'New vendor,Vendor reject rate,Skip lot,Stock age,Stock age (item)', 2),
        ('InspectionRule', 'vendor', 'Vendor', 'Link', FALSE, 'Vendor', 3),
        ('InspectionRule', 'ref', 'Category / item', 'Data', FALSE, NULL, 4),
        ('InspectionRule', 'threshold', 'Threshold', 'Number', FALSE, NULL, 5),
        ('InspectionRule', 'sample_pct', 'Sample (%%)', 'Number', FALSE, NULL, 6),
        ('InspectionRule', 'inspection_plan', 'Plan', 'Link', FALSE, 'InspectionPlan', 7),
        ('InspectionRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 8),

        ('InspectionLot', 'grn_id', 'Receipt', 'Data', FALSE, NULL, 1),
        ('InspectionLot', 'vendor', 'Vendor', 'Data', FALSE, NULL, 2),
        ('InspectionLot', 'sku', 'Item', 'Data', FALSE, NULL, 3),
        ('InspectionLot', 'batch_no', 'Lot', 'Data', FALSE, NULL, 4),
        ('InspectionLot', 'location', 'Location', 'Data', FALSE, NULL, 5),
        ('InspectionLot', 'qty', 'Qty', 'Number', FALSE, NULL, 6),
        ('InspectionLot', 'sample_size', 'Sample', 'Number', FALSE, NULL, 7),
        ('InspectionLot', 'reason', 'Asked by', 'Data', FALSE, NULL, 8),
        ('InspectionLot', 'inspection_plan', 'Plan', 'Data', FALSE, NULL, 9),
        ('InspectionLot', 'passed_qty', 'Passed', 'Number', FALSE, NULL, 10),
        ('InspectionLot', 'failed_qty', 'Failed', 'Number', FALSE, NULL, 11),
        ('InspectionLot', 'status', 'Status', 'Select', TRUE, 'Pending,Released,Part released,Rejected,Waived', 12),

        ('StockAgeInspection', 'location', 'Location', 'Data', FALSE, NULL, 1),
        ('StockAgeInspection', 'rule', 'Rule', 'Data', FALSE, NULL, 2),
        ('StockAgeInspection', 'age_band', 'Age band', 'Data', FALSE, NULL, 3),
        ('StockAgeInspection', 'due_date', 'Due', 'Date', FALSE, NULL, 4),
        ('StockAgeInspection', 'status', 'Status', 'Select', TRUE, 'Open,Done', 5)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- Each test on a plan may carry numeric limits; a reading outside them fails.
    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = '[{"key":"parameter_name","label":"Parameter","type":"text","required":true},{"key":"spec","label":"Specification","type":"text","required":true},{"key":"min","label":"Min","type":"number"},{"key":"max","label":"Max","type":"number"}]'
       WHERE doctype_name = 'InspectionPlan' AND fieldname = 'test_parameters' AND options NOT LIKE '%%"key":"min"%%'
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = 'Incoming,Production,Outgoing,Stock age'
       WHERE doctype_name = 'InspectionPlan' AND fieldname = 'applies_to' AND options NOT LIKE '%%Stock age%%'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'InspectionRule', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'InspectionRule', TRUE, TRUE, TRUE, FALSE),
        ('HR/Admin', 'InspectionLot', TRUE, FALSE, TRUE, FALSE),
        ('Store Manager', 'InspectionLot', TRUE, FALSE, TRUE, FALSE),
        ('Accountant', 'InspectionLot', TRUE, FALSE, FALSE, FALSE),
        ('HR/Admin', 'StockAgeInspection', TRUE, FALSE, TRUE, FALSE),
        ('Store Manager', 'StockAgeInspection', TRUE, FALSE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
