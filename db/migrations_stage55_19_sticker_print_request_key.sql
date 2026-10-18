-- ---------------------------------------------------------------------------
-- Stage 55.19 - no duplicate print history on retry.
--
-- A print request may carry a request key (the screen keeps it until the
-- request succeeds). sticker_print_log gains request_key + request_line and a
-- partial unique index, so a retried request with the same key writes each
-- label line's history row once; the labels themselves are still returned so
-- the retry can actually print. Rows without a key (older clients, API
-- callers) behave exactly as before.
--
-- Additive and idempotent, replayed into every tenant schema.
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

    EXECUTE format('ALTER TABLE %I.sticker_print_log ADD COLUMN IF NOT EXISTS request_key VARCHAR(80)', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE %I.sticker_print_log ADD COLUMN IF NOT EXISTS request_line INT', schema_rec.schema_name);
    EXECUTE format('CREATE UNIQUE INDEX IF NOT EXISTS uq_sticker_print_log_request ON %I.sticker_print_log (request_key, request_line) WHERE request_key IS NOT NULL',
      schema_rec.schema_name);
  END LOOP;
END
$mig$;
