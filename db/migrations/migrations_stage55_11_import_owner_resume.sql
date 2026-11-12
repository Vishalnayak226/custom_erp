-- ---------------------------------------------------------------------------
-- Stage 55.11 (73.4.4 items, user go-ahead 2026-10-17):
--
--   * PIMImportSchedule.owner - the person a schedule's imports run as (their
--     user id on the ImportJob, their role for field-write restrictions).
--     Blank falls back to the schedule's creator; a schedule never runs as
--     "system" again (engines/pim_import_schedule.go PIMImportScheduleRunAs).
--   * ImportJob gains read-only fields for resuming from failed rows
--     (engines/import_resume.go): open failed rows, rows resolved since, and
--     the job a resume came from. The cells themselves live in the job's
--     JSON, like error_csv already does.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT v.* FROM (VALUES
        ('PIMImportSchedule', 'owner', 'Owner (imports run as this user)', 'Data', FALSE, NULL, 12),
        ('ImportJob', 'resolved_rows', 'Failed rows imported since', 'Number', FALSE, NULL, 20),
        ('ImportJob', 'resumed_from', 'Resumed from job', 'Data', FALSE, NULL, 21)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = v.doctype_name)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
