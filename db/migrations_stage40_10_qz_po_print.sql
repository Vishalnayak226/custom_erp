-- Stage 40.10: silent QZ printing for a Purchase Order.
--
-- handlers_qz_print.go's job_type switch gains a 'Purchase Order' case
-- (engines.BuildPurchaseOrderQZPayload); this migration is the one schema
-- change that goes with it - adding 'Purchase Order' to the Printer.print_role
-- Select list (seeded by migrations_stage31_1_qz_print.sql) so an operator can
-- actually set a printer's "Default For" to it. Appended, not replaced, and
-- guarded so re-running this migration cannot append it twice - same shape as
-- migrations_stage34_3_undercut_alert.sql's NotificationTemplate.event append.
UPDATE tenant_default.doctype_fields
   SET options = options || ',Purchase Order'
 WHERE doctype_name = 'Printer'
   AND fieldname = 'print_role'
   AND options NOT LIKE '%Purchase Order%';

-- Existing tenants were provisioned by copying tenant_default at some point in
-- the past, so a change made only there never reaches them (the Stage 30.2.2 /
-- 31.1 failure mode). Same DO-block catch-up shape as migrations_stage31_1's
-- own tenant-schema loop.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format($f$
      UPDATE %I.doctype_fields
         SET options = options || ',Purchase Order'
       WHERE doctype_name = 'Printer'
         AND fieldname = 'print_role'
         AND options NOT LIKE '%%Purchase Order%%'
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
