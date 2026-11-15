-- ---------------------------------------------------------------------------
-- Ask 176 item 176.4: field-service visits. A ServiceVisit is one engineer's
-- visit on a service ticket: when it is booked, check-in and check-out (the
-- server stamps the time when the status moves, unless one is typed), the
-- parts used and the name of the customer who signed it off
-- (engines/service_field_visit.go).
-- Additive and idempotent, replayed into every tenant schema.
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
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('ServiceVisit', 'Service', 'service', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('ServiceVisit', 'code',                  'Visit number',  'Data',     TRUE,  NULL, 1),
        ('ServiceVisit', 'ticket',                'Ticket',        'Link',     TRUE,  'ServiceTicket', 2),
        ('ServiceVisit', 'engineer',              'Engineer',      'Link',     TRUE,  'Employee', 3),
        ('ServiceVisit', 'scheduled_on',          'Visit date',    'Date',     TRUE,  NULL, 4),
        ('ServiceVisit', 'scheduled_time',        'Time',          'Data',     FALSE, NULL, 5),
        ('ServiceVisit', 'checked_in_at',         'Checked in',    'Data',     FALSE, NULL, 6),
        ('ServiceVisit', 'checked_out_at',        'Checked out',   'Data',     FALSE, NULL, 7),
        ('ServiceVisit', 'work_done',             'Work done',     'Text',     FALSE, NULL, 8),
        ('ServiceVisit', 'parts_used',            'Parts used',    'Text',     FALSE, NULL, 9),
        ('ServiceVisit', 'customer_signoff_name', 'Signed off by', 'Data',     FALSE, NULL, 10),
        ('ServiceVisit', 'status',                'Status',        'Select',   TRUE,  'Scheduled,Checked in,Done,Cancelled', 11)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'ServiceVisit', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'ServiceVisit', TRUE, TRUE, TRUE, FALSE),
        ('HR/Admin',      'ServiceVisit', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
