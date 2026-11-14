-- ---------------------------------------------------------------------------
-- Stage 170.22: appraisal calibration. The calibrated rating, the reason and who
-- set it sit beside the original overall_rating, which is never overwritten.
-- Additive only: a cycle with no owner and an appraisal never calibrated behave
-- exactly as before.
-- ---------------------------------------------------------------------------

DO $$
DECLARE
  s RECORD;
BEGIN
  FOR s IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default'
        OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = s.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('AppraisalCycle', 'owner',              'Cycle owner (user)', 'Data',   FALSE, NULL, 20),
        ('Appraisal',      'calibrated_rating',  'Calibrated rating',  'Number', FALSE, NULL, 20),
        ('Appraisal',      'calibration_reason', 'Calibration reason', 'Data',   FALSE, NULL, 21),
        ('Appraisal',      'calibrated_by',      'Calibrated by',      'Data',   FALSE, NULL, 22),
        ('Appraisal',      'calibrated_at',      'Calibrated at',      'Data',   FALSE, NULL, 23)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, s.schema_name);
  END LOOP;
END $$;
