-- Stage 139.5 / 139.6f: UserShortcut - one person's favourites (the star on a
-- screen or report, shown first in the Ctrl+K box) and saved views (a named
-- search on a record list). Kept on the server so they follow the person to
-- any computer; recents and drafts stay in the browser (public/shortcuts.js).
--
-- The personal-view shape ReportFilterPreset / ReportColumnProfile already
-- use: a plain Master record type through the standard /api/v1/doc API with a
-- mandatory "owner" username, read back filtered by owner. Every role may
-- keep its own (granted to each role the client already has). Additive.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('UserShortcut', 'Core', 'core', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('UserShortcut', 'kind',   'Kind',             'Select', TRUE,  'Favourite,List view', 1),
        ('UserShortcut', 'name',   'Name',             'Data',   TRUE,  NULL, 2),
        ('UserShortcut', 'target', 'Opens',            'Data',   TRUE,  NULL, 3),
        ('UserShortcut', 'params', 'Search (JSON)',    'Data',   FALSE, NULL, 4),
        ('UserShortcut', 'owner',  'Owner Username',   'Data',   TRUE,  NULL, 5),
        ('UserShortcut', 'status', 'Status',           'Select', TRUE,  'Active,Inactive', 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        SELECT DISTINCT role, 'UserShortcut', TRUE, TRUE, TRUE, TRUE FROM %I.role_permissions
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
