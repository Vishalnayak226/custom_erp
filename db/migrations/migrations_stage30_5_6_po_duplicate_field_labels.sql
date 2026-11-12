-- ---------------------------------------------------------------------------
-- Stage 30.5.6 (the remaining half): PurchaseOrder's two "PO Number" columns.
--
-- 30.6 made both `po_number` and `code` server-issued and read-only, which
-- removed the duplicate *input* boxes. What was left is that both still carry
-- the identical label "PO Number", so a record list shows two columns with the
-- same heading and the same value. The `vendor`/`vendor_id` half is handled in
-- code (engines/document_mirror_fields.go) because the form has to stop asking
-- for it; this half only needs the label to stop lying.
--
-- `code` is relabelled rather than dropped: it is the field
-- db/migrations_phase3.sql registered, it is populated on every historical PO,
-- and engines/procurement.go still writes it on RFQ conversion.
-- ---------------------------------------------------------------------------
UPDATE tenant_default.doctype_fields
   SET label = 'PO Number (Internal)'
 WHERE doctype_name = 'PurchaseOrder' AND fieldname = 'code';

UPDATE tenant_default.doctype_fields
   SET label = 'Vendor Code (auto)'
 WHERE doctype_name = 'PurchaseOrder' AND fieldname = 'vendor_id';

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format($f$
      UPDATE %I.doctype_fields t SET label = d.label
        FROM tenant_default.doctype_fields d
       WHERE d.doctype_name = t.doctype_name AND d.fieldname = t.fieldname
         AND d.doctype_name = 'PurchaseOrder' AND d.fieldname IN ('code', 'vendor_id')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
