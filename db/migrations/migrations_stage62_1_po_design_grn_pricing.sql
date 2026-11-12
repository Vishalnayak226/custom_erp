-- ---------------------------------------------------------------------------
-- Stage 62 (user, 2026-10-15): fast-fashion buying.
--
-- engines/grn_pricing.go, engines/grn_print.go and engines/po_design.go hold
-- the behaviour; this file only declares what they store, so the generic
-- record screens show it:
--
-- 1. VendorCreditNote - the mirror of DebitNote: the vendor billed below the
--    PO price, so a credit is raised to them (Dr 5150 / Cr 2100, GST to
--    1500). Same module, permissions and number-series shape as DebitNote;
--    series VCN next to the existing DN.
-- 2. DebitNote gains the GRN it came from, its GST, the vendor's invoice
--    number and its price-difference lines.
-- 3. GRN gains its POs (multi-PO, one vendor), the vendor, the vendor's
--    invoice number / date / amount, the PO vs invoice totals and the
--    difference, and the notes it raised.
-- 4. ProductFamily (the design, Stage 51.5) gains its design attributes and
--    the defaults the PO's Add design fills: HSN, GST rate, MRP, purchase
--    rate, vendor.
--
-- Additive and idempotent (ON CONFLICT DO NOTHING), replayed into every
-- tenant schema; new tenants clone tenant_default's rows. Existing documents
-- are never rewritten.
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
      VALUES ('VendorCreditNote', 'Finance', 'finance', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('VendorCreditNote', 'note_number', 'Note Number', 'Data', TRUE, NULL, 1),
        ('VendorCreditNote', 'vendor_id', 'Vendor', 'Link', TRUE, 'Vendor', 2),
        ('VendorCreditNote', 'reference_po', 'Reference PO', 'Data', FALSE, NULL, 3),
        ('VendorCreditNote', 'grn_id', 'Goods Receipt (GRN)', 'Data', FALSE, NULL, 4),
        ('VendorCreditNote', 'vendor_invoice_no', 'Vendor Invoice No', 'Data', FALSE, NULL, 5),
        ('VendorCreditNote', 'amount', 'Amount (ex-GST)', 'Number', TRUE, NULL, 6),
        ('VendorCreditNote', 'gst_amount', 'GST', 'Number', FALSE, NULL, 7),
        ('VendorCreditNote', 'total_with_gst', 'Total with GST', 'Number', FALSE, NULL, 8),
        ('VendorCreditNote', 'reason', 'Reason', 'Data', TRUE, NULL, 9),
        ('VendorCreditNote', 'status', 'Status', 'Select', TRUE, 'Draft,Posted', 10),
        ('DebitNote', 'grn_id', 'Goods Receipt (GRN)', 'Data', FALSE, NULL, 7),
        ('DebitNote', 'vendor_invoice_no', 'Vendor Invoice No', 'Data', FALSE, NULL, 8),
        ('DebitNote', 'gst_amount', 'GST', 'Number', FALSE, NULL, 9),
        ('DebitNote', 'total_with_gst', 'Total with GST', 'Number', FALSE, NULL, 10),
        ('GRN', 'po_ids', 'Purchase Orders', 'Data', FALSE, NULL, 20),
        ('GRN', 'vendor', 'Vendor', 'Link', FALSE, 'Vendor', 21),
        ('GRN', 'vendor_invoice_no', 'Vendor Invoice No', 'Data', FALSE, NULL, 22),
        ('GRN', 'vendor_invoice_date', 'Vendor Invoice Date', 'Date', FALSE, NULL, 23),
        ('GRN', 'vendor_invoice_amount', 'Vendor Invoice Amount (incl. GST)', 'Number', FALSE, NULL, 24),
        ('GRN', 'po_value', 'Value at PO Price (ex-GST)', 'Number', FALSE, NULL, 25),
        ('GRN', 'invoice_value', 'Value at Invoice Price (ex-GST)', 'Number', FALSE, NULL, 26),
        ('GRN', 'invoice_gst', 'GST at Invoice Price', 'Number', FALSE, NULL, 27),
        ('GRN', 'invoice_total', 'Invoice Value incl. GST', 'Number', FALSE, NULL, 28),
        ('GRN', 'invoice_amount_difference', 'Vendor Invoice minus Lines', 'Number', FALSE, NULL, 29),
        ('GRN', 'debit_note_id', 'Debit Note', 'Data', FALSE, NULL, 30),
        ('GRN', 'vendor_credit_note_id', 'Vendor Credit Note', 'Data', FALSE, NULL, 31),
        ('ProductFamily', 'design_attributes', 'Design Attributes', 'Data', FALSE, NULL, 10),
        ('ProductFamily', 'vendor', 'Vendor', 'Link', FALSE, 'Vendor', 11),
        ('ProductFamily', 'hsn_code', 'HSN Code', 'Data', FALSE, NULL, 12),
        ('ProductFamily', 'gst_rate', 'GST Rate (%%)', 'Number', FALSE, NULL, 13),
        ('ProductFamily', 'mrp', 'MRP', 'Number', FALSE, NULL, 14),
        ('ProductFamily', 'purchase_rate', 'Purchase Rate', 'Number', FALSE, NULL, 15)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'VendorCreditNote', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'VendorCreditNote', TRUE, TRUE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('VCN', 'VCN', '/', 6, 'ANNUAL', TRUE),
               ('DN',  'DN',  '/', 6, 'ANNUAL', TRUE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
