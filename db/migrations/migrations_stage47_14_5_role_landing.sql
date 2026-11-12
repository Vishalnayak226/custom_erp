-- Stage 47.14.5: a role's default landing screen (a view key such as 'pos').
-- One table, no business rows touched. A role with no row lands as it does
-- today, or on its role template's landing.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                    WHERE table_schema = schema_rec.schema_name AND table_name = 'users') THEN
      CONTINUE;
    END IF;
    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.role_landing (
        role       VARCHAR(100) PRIMARY KEY,
        view       VARCHAR(60)  NOT NULL,
        updated_by VARCHAR(100),
        updated_at TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
