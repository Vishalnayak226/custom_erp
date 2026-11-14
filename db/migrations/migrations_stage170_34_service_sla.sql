-- ---------------------------------------------------------------------------
-- Ask 170 item 170.34: service ticket SLA timers and escalation. The due times
-- the worker stamps and the escalation it records are shown as ticket fields.
-- Behind service.sla_enabled (default off). Additive only.
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
        ('ServiceTicket', 'first_response_due', 'Respond by',   'Data', FALSE, NULL, 90),
        ('ServiceTicket', 'resolve_due',        'Resolve by',   'Data', FALSE, NULL, 91),
        ('ServiceTicket', 'sla_escalated_at',   'Escalated at', 'Data', FALSE, NULL, 92),
        ('ServiceTicket', 'escalated_to',       'Escalated to', 'Data', FALSE, NULL, 93)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
