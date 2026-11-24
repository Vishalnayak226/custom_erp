-- Item 263.4 (ask 263, "Rename all thos QZ as well"): the Printer field that
-- holds the exact name the operating system gives a printer is now
-- os_printer_name, and the print log stores it in a column of that name.
--
-- Additive and safe to run twice:
--   * print_job_log gains os_printer_name. The old qz_printer_name column is
--     never dropped: rows printed before this release keep their value there
--     and the server reads COALESCE(os_printer_name, qz_printer_name).
--   * The Printer form's field definition is renamed to os_printer_name, and
--     its label loses "(exact, from QZ)" only where the shipped wording is
--     still in place - a label an admin changed is kept (same rule as 80.1).
--     Skipped where os_printer_name already exists.
-- Saved Printer records are NOT changed here: a release never changes client
-- data (CONTRIBUTING Article 5). They move to os_printer_name when a client
-- runs the "Printer names" heal in Settings > Data Healer; until then the
-- server reads either key.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF to_regclass(format('%I.print_job_log', schema_rec.schema_name)) IS NOT NULL THEN
      EXECUTE format('ALTER TABLE %I.print_job_log ADD COLUMN IF NOT EXISTS os_printer_name VARCHAR(255)', schema_rec.schema_name);
    END IF;

    IF to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NOT NULL THEN
      EXECUTE format($f$
        UPDATE %I.doctype_fields
           SET fieldname = 'os_printer_name',
               label = CASE WHEN label = 'OS Printer Name (exact, from QZ)' THEN 'OS Printer Name' ELSE label END
         WHERE doctype_name = 'Printer' AND fieldname = 'qz_printer_name'
           AND NOT EXISTS (SELECT 1 FROM %I.doctype_fields
                            WHERE doctype_name = 'Printer' AND fieldname = 'os_printer_name')
      $f$, schema_rec.schema_name, schema_rec.schema_name);
    END IF;
  END LOOP;
END $$;
