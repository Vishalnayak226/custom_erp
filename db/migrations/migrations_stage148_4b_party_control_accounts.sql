-- ---------------------------------------------------------------------------
-- Stage 148.4b (ask 176): receivables and payables by party group. Two more
-- posting-profile events - receivable_control (in place of 1300 Accounts
-- Receivable) and payable_control (in place of 2100 GRN Suspense / payable) -
-- matched by the profile's customer group / vendor group. The remap itself is
-- code (engines/posting_groups.go); with no such profile nothing changes.
--
-- Metadata only: the two events are appended to the PostingProfile event
-- Select options, once. Named 148_4b so it sorts after 148_4_5, which set the
-- option list. Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default'
        OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = options || ',receivable_control,payable_control'
       WHERE doctype_name = 'PostingProfile' AND fieldname = 'event'
         AND options IS NOT NULL AND options NOT LIKE '%%receivable_control%%'
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
