-- ---------------------------------------------------------------------------
-- Stage 52.9 (user decision 2026-10-08): record the batch/lot on each sticker
-- print run.
--
-- Since 52.8 a single lot of a GRN line can be printed on its own, but
-- sticker_print_log had no lot column, so two runs for the same SKU on the
-- same GRN were indistinguishable in the print history. resolveAndLogSticker
-- (engines/stickers.go) already receives the lot - it goes onto the label -
-- and now writes it here too.
--
-- Additive and idempotent: one nullable column on every tenant schema that
-- has the table. Existing rows and the manual SKU-scan flow (no lot) read
-- back NULL. New tenants clone tenant_default's table (engines/saas.go), so
-- they get the column from there.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'sticker_print_log');

    EXECUTE format('ALTER TABLE %I.sticker_print_log ADD COLUMN IF NOT EXISTS batch_no VARCHAR(100)',
      schema_rec.schema_name);
  END LOOP;
END
$mig$;
