-- ---------------------------------------------------------------------------
-- Stage 127.4 (user, 2026-10-21: "see if accounting and wms and other module
-- needs print profile pbuild that also. see the std practice and bring those
-- aslo"; this is 74.3.4's profile and legal-identity part).
--
-- PrintProfile: how one kind of document prints, for every module - its copy
-- labels (one print run = one set: Original for Recipient / Duplicate for
-- Transporter / Triplicate for Supplier...), paper, default template and a
-- footer line - optionally for one store. Chosen by precedence: this document
-- + this store, this document, all documents + this store, all documents.
-- Nothing is seeded: a client prints exactly as before until an admin adds a
-- profile.
--
-- LegalEntity gains the seller's identity a document carries: trade name,
-- contact, bank and terms - printed from here, not typed into each template.
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
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('PrintProfile', 'Core', 'core', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('PrintProfile', 'code', 'Profile Code', 'Data', TRUE, NULL, 1),
        ('PrintProfile', 'name', 'Profile Name', 'Data', TRUE, NULL, 2),
        ('PrintProfile', 'transaction', 'Document (blank = all documents)', 'Data', FALSE, NULL, 3),
        ('PrintProfile', 'store', 'Only for store', 'Link', FALSE, 'Location', 4),
        ('PrintProfile', 'copy_labels', 'Copy labels (comma-separated; one print = one set)', 'Data', FALSE, NULL, 5),
        ('PrintProfile', 'paper', 'Paper', 'Select', FALSE, ',A4,A5', 6),
        ('PrintProfile', 'template', 'Template', 'Link', FALSE, 'PrintTemplate', 7),
        ('PrintProfile', 'footer_text', 'Footer line', 'Data', FALSE, NULL, 8),
        ('PrintProfile', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'PrintProfile', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'PrintProfile', TRUE, FALSE, FALSE, FALSE),
        ('Cashier', 'PrintProfile', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('PrintProfile', 'PP', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('LegalEntity', 'trade_name', 'Trade name (printed)', 'Data', FALSE, NULL, 21),
        ('LegalEntity', 'contact_phone', 'Phone (printed)', 'Data', FALSE, NULL, 22),
        ('LegalEntity', 'email', 'Email (printed)', 'Data', FALSE, NULL, 23),
        ('LegalEntity', 'bank_name', 'Bank name', 'Data', FALSE, NULL, 24),
        ('LegalEntity', 'bank_account', 'Bank account number', 'Data', FALSE, NULL, 25),
        ('LegalEntity', 'bank_ifsc', 'Bank IFSC', 'Data', FALSE, NULL, 26),
        ('LegalEntity', 'print_terms', 'Terms printed on invoices', 'Data', FALSE, NULL, 27)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
