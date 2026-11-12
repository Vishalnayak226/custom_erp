-- ---------------------------------------------------------------------------
-- Stage 55.15 - expense claims carry the bill / receipt number.
--
-- There is no attachment mechanism in this codebase yet (Stage 74's source
-- re-check lists it), so a claim cannot hold the receipt image itself. What it
-- can hold is the number printed on the bill, so an approver and AP can ask
-- for the paper and match it, and a second claim for the same bill is easy to
-- spot. Optional, additive, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ExpenseClaim', 'receipt_number', 'Bill / Receipt Number', 'Data', FALSE, NULL, 12)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        VALUES ('ExpenseClaim', 'receipt_number', 'Bill / Receipt Number', 'Data', FALSE, NULL, 12)
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
