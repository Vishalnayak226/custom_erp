-- ---------------------------------------------------------------------------
-- Stage 148.15: a vendor advance against a purchase order.
--
-- New asset account 1810 Vendor Advances in every tenant's chart (as 148.1 added
-- 5195): Dr 1810 / Cr bank when an advance is paid; applying it to a bill is a
-- settle line, Dr 2100 / Cr 1810. ON CONFLICT DO NOTHING, so a client who
-- already uses 1810 keeps their own row. This writes one new gl_accounts row
-- per tenant, so the release counts it as a data change.
--
-- VendorAdvance is the document that tracks one advance. Read-only for every
-- role: it is written only by the advance routes, which post the money.
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type)
        VALUES ('1810', 'Vendor Advances', 'Asset')
        ON CONFLICT DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('VendorAdvance', 'Procurement', 'procurement', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('VendorAdvance', 'vendor_id', 'Vendor', 'Link', TRUE, 'Vendor', 1),
        ('VendorAdvance', 'po_id', 'Purchase order', 'Link', FALSE, 'PurchaseOrder', 2),
        ('VendorAdvance', 'amount', 'Amount', 'Number', TRUE, NULL, 3),
        ('VendorAdvance', 'advance_date', 'Date', 'Date', FALSE, NULL, 4),
        ('VendorAdvance', 'reference', 'Reference', 'Data', FALSE, NULL, 5),
        ('VendorAdvance', 'open_amount', 'Open', 'Number', FALSE, NULL, 6),
        ('VendorAdvance', 'applied_amount', 'Applied', 'Number', FALSE, NULL, 7),
        ('VendorAdvance', 'refunded_amount', 'Refunded', 'Number', FALSE, NULL, 8),
        ('VendorAdvance', 'status', 'Status', 'Select', TRUE, 'Open,Part Applied,Applied,Refunded,Cancelled', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'VendorAdvance', TRUE, FALSE, FALSE, FALSE),
        ('Accountant', 'VendorAdvance', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('VendorAdvance', 'VADV', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
