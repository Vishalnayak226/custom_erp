-- Stage 130.16 (74.3.4, BRD 4.11, DEC-17/20): the rest of print profiles.
-- print_template_versions keeps every saved version of a print template
-- (Active / Pending / Superseded / Rejected); PrintTemplate shows its version
-- and any change waiting for approval. A Print Profile can also name a legal
-- entity and a channel. The Legal Entity gains its approved logo. A Sales
-- Invoice gains the buyer's GSTIN, the place of supply and its date.
-- Additive: a new table, new fields.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');
    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.print_template_versions (
        template_id VARCHAR(200) NOT NULL,
        version INT NOT NULL,
        layout TEXT NOT NULL,
        status VARCHAR(20) NOT NULL DEFAULT 'Active',
        saved_by VARCHAR(200),
        saved_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        approved_by VARCHAR(200),
        note TEXT,
        PRIMARY KEY (template_id, version)
      )
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('PrintTemplate', 'version',         'Version',                   'Number', FALSE, NULL, 100),
        ('PrintTemplate', 'pending_version', 'Change awaiting approval',  'Number', FALSE, NULL, 101),
        ('PrintProfile',  'legal_entity',    'Legal entity',              'Link',   FALSE, 'LegalEntity', 100),
        ('PrintProfile',  'channel',         'Channel',                   'Select', FALSE, 'All,POS,Online,B2B,ERP', 101),
        ('LegalEntity',   'logo',            'Logo (image link)',         'Data',   FALSE, NULL, 100),
        ('SalesInvoice',  'invoice_date',    'Invoice date',              'Date',   FALSE, NULL, 100),
        ('SalesInvoice',  'customer_gstin',  'Buyer GSTIN',               'Data',   FALSE, NULL, 101),
        ('SalesInvoice',  'place_of_supply', 'Place of supply',           'Data',   FALSE, NULL, 102)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
