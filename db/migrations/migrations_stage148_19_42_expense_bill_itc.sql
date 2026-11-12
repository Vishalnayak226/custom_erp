-- ---------------------------------------------------------------------------
-- Stage 148.19 / 148.42 (builder c): bills without a PO, and GST credit
-- eligibility with value bands.
--
-- 148.19 adds no table: an expense bill is a VendorInvoice with bill_kind
-- "Expense", made by the engine. The only schema row is the new posting event
-- (expense_bill) a vendor group can name its expense account for.
--
-- 148.42 adds three optional fields, hidden until the setting
-- finance.itc_eligibility is on: HSNCode.itc and HSNCode.value_bands, and
-- Vendor.itc. All blank on every existing row, and blank means today's
-- behaviour: eligible, and the rate on the HSN row.
--
-- Additive and idempotent, replayed into every tenant schema. No business row
-- is written, only metadata rows.
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
      UPDATE %I.doctype_fields
         SET options = options || ',expense_bill'
       WHERE doctype_name = 'PostingProfile' AND fieldname = 'event'
         AND options IS NOT NULL AND options NOT LIKE '%%expense_bill%%'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('HSNCode', 'itc', 'GST credit', 'Select', FALSE, ',Eligible,Blocked', 6),
        ('HSNCode', 'value_bands', 'Rate by price', 'Data', FALSE, NULL, 7),
        ('Vendor', 'itc', 'GST credit', 'Select', FALSE, ',Eligible,Blocked', 86)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
