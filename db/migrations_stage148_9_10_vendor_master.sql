-- ---------------------------------------------------------------------------
-- Stage 148.9 / 148.10 (builder i): vendor master basics and vendor changes
-- that wait for approval.
--
-- 148.9: Vendor gets hold (No new PO / No payment / Both), payment_term,
-- tds_section (so the bill's TDS section is a registered field), msme and
-- credit_limit; PaymentTerm gets end_of_month, discount_days, discount_pct.
-- All blank on every existing row, and a blank means exactly what it meant
-- before, so no vendor, term, bill or payment changes behaviour.
--
-- 148.10: the VendorChange doctype - a request to change a vendor's bank,
-- GSTIN, terms, credit limit or group, filed by the save / import path and
-- applied when approved (the same maker-checker engine as GLAccountChange).
--
-- Additive and idempotent, replayed into every tenant schema. No existing
-- row is written except the metadata rows below.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('Vendor', 'hold', 'Hold', 'Select', FALSE, ',No new PO,No payment,Both', 81),
        ('Vendor', 'payment_term', 'Payment term', 'Link', FALSE, 'PaymentTerm', 82),
        ('Vendor', 'tds_section', 'TDS section', 'Data', FALSE, NULL, 83),
        ('Vendor', 'msme', 'MSME', 'Select', FALSE, ',No,Yes', 84),
        ('Vendor', 'credit_limit', 'Credit limit', 'Number', FALSE, NULL, 85),
        ('PaymentTerm', 'end_of_month', 'End of month', 'Select', FALSE, ',No,Yes', 5),
        ('PaymentTerm', 'discount_days', 'Discount days', 'Number', FALSE, NULL, 6),
        ('PaymentTerm', 'discount_pct', 'Discount %%', 'Number', FALSE, NULL, 7)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('VendorChange', 'Procurement', 'procurement', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('VendorChange', 'vendor_id', 'Vendor', 'Data', TRUE, NULL, 1),
        ('VendorChange', 'vendor_name', 'Vendor name', 'Data', FALSE, NULL, 2),
        ('VendorChange', 'fields', 'Fields', 'Data', FALSE, NULL, 3),
        ('VendorChange', 'summary', 'What changes', 'Data', FALSE, NULL, 3),
        ('VendorChange', 'old_values', 'Before', 'Data', FALSE, NULL, 4),
        ('VendorChange', 'new_values', 'After', 'Data', FALSE, NULL, 5),
        ('VendorChange', 'requested_by', 'Requested by', 'Data', FALSE, NULL, 6),
        ('VendorChange', 'applied_at', 'Applied at (system)', 'Data', FALSE, NULL, 7),
        ('VendorChange', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 8)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'VendorChange', TRUE, FALSE, FALSE, FALSE),
        ('Accountant', 'VendorChange', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('VendorChange', 0, NULL, 'Accountant')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('VendorChange', 'VCH', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
