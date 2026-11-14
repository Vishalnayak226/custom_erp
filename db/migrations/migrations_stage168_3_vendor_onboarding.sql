-- ---------------------------------------------------------------------------
-- Stage 168.3 (builder b): supplier onboarding and approved-vendor-only sourcing.
--
-- Vendor gets onboarding_status (Applied / Approved / Rejected / Blocked) and a
-- three-box documents checklist. All blank on every existing vendor, and a blank
-- means Approved, so no vendor or order changes behaviour. The behaviour sits
-- behind two settings that are off by default.
-- VendorOnboarding is the request that goes through the maker-checker engine;
-- ApprovedVendor is the approved vendor list (item or category -> vendor).
--
-- Additive and idempotent, replayed into every tenant schema. No existing row
-- is written except the metadata rows below.
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
        ('Vendor', 'onboarding_status', 'Onboarding', 'Select', FALSE, ',Applied,Approved,Rejected,Blocked', 90),
        ('Vendor', 'doc_gstin_checked', 'GSTIN checked', 'Select', FALSE, ',Yes', 91),
        ('Vendor', 'doc_pan_checked', 'PAN checked', 'Select', FALSE, ',Yes', 92),
        ('Vendor', 'doc_bank_proof_checked', 'Bank proof checked', 'Select', FALSE, ',Yes', 93)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('VendorOnboarding', 'Procurement', 'procurement', 'Master'),
             ('ApprovedVendor', 'Procurement', 'procurement', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('VendorOnboarding', 'vendor_id', 'Vendor', 'Data', TRUE, NULL, 1),
        ('VendorOnboarding', 'vendor_name', 'Vendor name', 'Data', FALSE, NULL, 2),
        ('VendorOnboarding', 'target_status', 'Decision', 'Select', FALSE, 'Approved,Blocked', 3),
        ('VendorOnboarding', 'summary', 'Summary', 'Data', FALSE, NULL, 4),
        ('VendorOnboarding', 'requested_by', 'Requested by', 'Data', FALSE, NULL, 5),
        ('VendorOnboarding', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 6),
        ('ApprovedVendor', 'item_id', 'Item', 'Data', FALSE, NULL, 1),
        ('ApprovedVendor', 'category', 'Category', 'Data', FALSE, NULL, 2),
        ('ApprovedVendor', 'vendor_id', 'Vendor', 'Link', TRUE, 'Vendor', 3),
        ('ApprovedVendor', 'note', 'Note', 'Data', FALSE, NULL, 4)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'VendorOnboarding', TRUE, FALSE, FALSE, FALSE),
        ('Accountant', 'VendorOnboarding', TRUE, FALSE, FALSE, FALSE),
        ('Store Manager', 'VendorOnboarding', TRUE, FALSE, FALSE, FALSE),
        ('Super Admin', 'ApprovedVendor', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'ApprovedVendor', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'ApprovedVendor', TRUE, TRUE, TRUE, TRUE),
        ('Accountant', 'ApprovedVendor', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('VendorOnboarding', 0, NULL, 'Store Manager')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('VendorOnboarding', 'VON', '-', 4, 'ANNUAL', FALSE),
               ('ApprovedVendor', 'AVL', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
