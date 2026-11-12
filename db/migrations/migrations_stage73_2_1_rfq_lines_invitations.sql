-- ---------------------------------------------------------------------------
-- Stage 73.2.1 - RFQ item lines and invited vendors with a response status.
--
-- RFQ.lines: the items asked for (an Item or free text, UOM, quantity).
-- RFQ.vendor_invitations: each vendor with Invited / Responded / Declined, a
-- due date and the quote that answered it (the pre-existing invited_vendors
-- list of codes is kept in step). RFQ.location / response_due_date: where the
-- goods go and when quotes are due. Written by engines/rfq_sourcing.go.
-- Field registration only. Additive and idempotent, replayed into every
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
      SELECT 'RFQ', v.f, v.l, v.t, FALSE, v.o, v.n
        FROM (VALUES
          ('lines', 'Items asked for', 'JSONTable', NULL, 6),
          ('vendor_invitations', 'Invited vendors', 'JSONTable', NULL, 7),
          ('location', 'Deliver to', 'Link', 'Location', 8),
          ('response_due_date', 'Quotes due by', 'Date', NULL, 9)
        ) AS v(f, l, t, o, n)
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = 'RFQ')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
