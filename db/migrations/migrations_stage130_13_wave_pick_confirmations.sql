-- Stage 130.13 (80.7 b, user decision 2026-10-22): a mobile picker's line
-- confirmations are kept on the server, not only in the browser - a reload or
-- a second device carries on where the picker was, and who picked what is
-- recorded. One row per wave line (sku | bin | batch). Additive.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.documents', schema_rec.schema_name)) IS NULL;
    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.wave_pick_confirmations (
        wave_id      TEXT NOT NULL,
        line_key     TEXT NOT NULL,
        qty          INT NOT NULL DEFAULT 0,
        confirmed_by TEXT NOT NULL,
        confirmed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        PRIMARY KEY (wave_id, line_key)
      )
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
