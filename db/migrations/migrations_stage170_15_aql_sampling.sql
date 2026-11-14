-- ---------------------------------------------------------------------------
-- Stage 170.15: AQL sampling. An inspection plan may name an AQL; an
-- inspection recorded against it gets the sample size and accept / reject
-- numbers for its lot and a result from the defects found. Plans with no AQL
-- and inspections already on file are untouched.
-- Additive and safe to run twice.
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
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('InspectionPlan',    'aql',             'AQL',          'Select', FALSE, ',0.065,0.10,0.15,0.25,0.40,0.65,1.0,1.5,2.5,4.0,6.5', 80),
        ('QualityInspection', 'aql',             'AQL',          'Data',   FALSE, NULL, 130),
        ('QualityInspection', 'aql_code_letter', 'Code letter',  'Data',   FALSE, NULL, 131),
        ('QualityInspection', 'lot_size',        'Lot size',     'Number', FALSE, NULL, 132),
        ('QualityInspection', 'sample_size',     'Sample size',  'Number', FALSE, NULL, 133),
        ('QualityInspection', 'accept_no',       'Accept at',    'Number', FALSE, NULL, 134),
        ('QualityInspection', 'reject_no',       'Reject at',    'Number', FALSE, NULL, 135),
        ('QualityInspection', 'defects_found',   'Defects found','Number', FALSE, NULL, 136),
        ('QualityInspection', 'aql_result',      'Lot result',   'Data',   FALSE, NULL, 137)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
