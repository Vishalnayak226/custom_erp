-- ---------------------------------------------------------------------------
-- Stage 73.2.2 - comparable quotes (decision D06).
--
-- A VendorQuote prices each RFQ line (unit price in the vendor's UOM, its
-- conversion to the RFQ's unit, tax, lead time) and carries freight, payment
-- terms and validity; quoted_price stays as the landed total (lines + tax +
-- freight) so every older reader keeps sorting on the right figure. A newer
-- quote from the same vendor marks the earlier one Superseded. Written by
-- engines/rfq_sourcing.go. Additive and idempotent, replayed into every
-- tenant schema.
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
      SELECT 'VendorQuote', v.f, v.l, v.t, FALSE, NULL, v.n
        FROM (VALUES
          ('lines', 'Priced lines', 'JSONTable', 7),
          ('subtotal', 'Lines total (ex tax)', 'Number', 8),
          ('tax_total', 'Tax', 'Number', 9),
          ('freight_amount', 'Freight', 'Number', 10),
          ('landed_total', 'Landed total', 'Number', 11),
          ('payment_terms', 'Payment terms', 'Data', 12),
          ('valid_until', 'Valid until', 'Date', 13)
        ) AS v(f, l, t, n)
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = 'VendorQuote')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET options = options || ',Superseded'
       WHERE doctype_name = 'VendorQuote' AND fieldname = 'status' AND options NOT LIKE '%%Superseded%%'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
