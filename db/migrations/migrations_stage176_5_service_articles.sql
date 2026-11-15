-- ---------------------------------------------------------------------------
-- Ask 176 item 176.5: service knowledge base. A ServiceArticle is a how-to or
-- fix by category; the record list's search reads its text. A service ticket
-- names the article that solved it (kb_article), and only a Published article
-- may be named (engines/service_knowledge_base.go).
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
        ('ServiceArticle', 'Service', 'service', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('ServiceArticle', 'code',     'Code',     'Data',   TRUE,  NULL, 1),
        ('ServiceArticle', 'name',     'Title',    'Data',   TRUE,  NULL, 2),
        ('ServiceArticle', 'category', 'Category', 'Data',   TRUE,  NULL, 3),
        ('ServiceArticle', 'keywords', 'Keywords', 'Data',   FALSE, NULL, 4),
        ('ServiceArticle', 'body',     'Article',  'Text',   TRUE,  NULL, 5),
        ('ServiceArticle', 'status',   'Status',   'Select', TRUE,  'Draft,Published,Archived', 6),
        ('ServiceTicket',  'kb_article', 'Help article', 'Link', FALSE, 'ServiceArticle', 26)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'ServiceArticle', TRUE, TRUE,  TRUE,  FALSE),
        ('Store Manager', 'ServiceArticle', TRUE, TRUE,  TRUE,  FALSE),
        ('HR/Admin',      'ServiceArticle', TRUE, TRUE,  TRUE,  FALSE),
        ('Cashier',       'ServiceArticle', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
