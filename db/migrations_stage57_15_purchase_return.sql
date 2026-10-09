-- ---------------------------------------------------------------------------
-- Stage 57.15 (user decision 2026-10-06): Purchase Return / return to vendor.
--
-- docs/specs/modules_overview.md and ERROR_CODES.md (PURCHA-0116..0120) have
-- described this since the start; only the financial half (DebitNote) was
-- ever built. A PurchaseReturn is raised against the original GRN, for at
-- most what that GRN received less what earlier returns already took, per
-- SKU, lot and stock bucket (accepted stock, or the rejected/damaged qty QC
-- set aside). It cannot be raised against a GRN whose stock never posted
-- (cancelled). Posting it (engines/purchase_return.go) moves the stock out
-- and raises and posts a DebitNote through the existing PostDebitNote path.
--
-- Additive and idempotent, replayed into every tenant schema that carries
-- the document model (the deployed tenants are not tenant_default). New
-- tenants copy these rows from tenant_default at provisioning.
--
--   * PurchaseReturn doctype + fields + grants (module Procurement, so the
--     role templates' Procurement grant covers it like GRN).
--   * Two number series: PRT for returns, DN for the debit notes a return
--     raises (a hand-made DebitNote still takes its typed note number).
--     documents.id is unique across doctypes, so both prefixes are distinct
--     from every existing series.
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
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('PurchaseReturn', 'Procurement', 'procurement', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- vendor_id/po_id/location/total_amount/debit_note_id are filled by the
    -- server (from the GRN, and on Post), so none of them is mandatory.
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('PurchaseReturn', 'code', 'Return Number', 'Data', TRUE, NULL, 1),
        ('PurchaseReturn', 'grn_id', 'Goods Receipt (GRN)', 'Link', TRUE, 'GRN', 2),
        ('PurchaseReturn', 'vendor_id', 'Vendor', 'Link', FALSE, 'Vendor', 3),
        ('PurchaseReturn', 'po_id', 'Purchase Order', 'Link', FALSE, 'PurchaseOrder', 4),
        ('PurchaseReturn', 'location', 'Location', 'Data', FALSE, NULL, 5),
        ('PurchaseReturn', 'reason', 'Reason for Return', 'Data', TRUE, NULL, 6),
        ('PurchaseReturn', 'return_items', 'Return Lines', 'JSONTable', TRUE,
         '[{"key":"sku","label":"SKU","type":"text","required":true},
           {"key":"batch_no","label":"Lot","type":"text","required":false},
           {"key":"stock_bucket","label":"Stock (Accepted/Rejected/Damaged)","type":"text","required":false},
           {"key":"qty","label":"Qty","type":"number","required":true}]', 7),
        ('PurchaseReturn', 'total_amount', 'Return Value (ex-GST)', 'Number', FALSE, NULL, 8),
        ('PurchaseReturn', 'debit_note_id', 'Debit Note', 'Link', FALSE, 'DebitNote', 9),
        ('PurchaseReturn', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Posted,Cancelled', 10)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'PurchaseReturn', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'PurchaseReturn', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('PRT', 'PRT', '/', 6, 'ANNUAL', TRUE),
               ('DN',  'DN',  '/', 6, 'ANNUAL', TRUE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
