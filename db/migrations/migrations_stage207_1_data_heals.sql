-- ---------------------------------------------------------------------------
-- Stage 207.1: Data Healer. A DataHeal record says a heal (a one-time fix a
-- client runs after a release, engines/data_healer.go) is done in this
-- client: which heal, when, by whom, how many records it filled. It is
-- written only by Run heal, never by hand. Metadata only, replayed into every
-- tenant schema; no existing row is touched - the heal itself runs later,
-- from Settings > Data Healer, when the client's admin presses Run heal.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL;
    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('DataHeal', 'Master Data', 'master_data', 'Master')
      ON CONFLICT (name) DO NOTHING$f$, schema_rec.schema_name);
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('DataHeal', 'heal_id', 'Heal',          'Data',   FALSE, NULL,   1),
        ('DataHeal', 'title',   'Title',         'Data',   FALSE, NULL,   2),
        ('DataHeal', 'status',  'Status',        'Select', TRUE,  'Done', 3),
        ('DataHeal', 'done_at', 'Done at',       'Data',   FALSE, NULL,   4),
        ('DataHeal', 'done_by', 'Done by',       'Data',   FALSE, NULL,   5),
        ('DataHeal', 'fixed',   'Records fixed', 'Number', FALSE, NULL,   6),
        ('DataHeal', 'typed',   'Typed by hand', 'Number', FALSE, NULL,   7)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$f$, schema_rec.schema_name);
    CONTINUE WHEN to_regclass(format('%I.role_permissions', schema_rec.schema_name)) IS NULL;
    EXECUTE format($f$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'DataHeal', TRUE, FALSE, TRUE,  FALSE),
        ('HR/Admin',      'DataHeal', TRUE, FALSE, TRUE,  FALSE),
        ('Store Manager', 'DataHeal', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING$f$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
