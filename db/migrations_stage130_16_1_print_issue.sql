-- Stage 130.16 (74.1.4, BRD 4.8, DEC-17): the issued-output snapshot and the
-- print / reprint log. The print job log (31.1, 127.6) gains the source
-- revision, original / reprint, copies and copy labels, the template, the
-- reason code and approver, the checksum of what was printed and the outcome
-- (Sent to browser / Submitted / Unknown - never "Printed", which nothing
-- confirms). print_snapshots keeps the first issued print of each document
-- revision, which every later print renders. ReprintRequest carries a
-- reprint to its approver when reprint control asks for one, and
-- DocumentReprint is the permission to reprint (a record type with no
-- fields or screen of its own, so it is not listed under Setup).
-- Additive: nullable columns, a new table, new doctypes; every existing
-- row keeps its meaning.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'print_job_log');
    EXECUTE format($sql$
      ALTER TABLE %I.print_job_log
        ADD COLUMN IF NOT EXISTS doc_revision INT,
        ADD COLUMN IF NOT EXISTS is_reprint BOOLEAN NOT NULL DEFAULT FALSE,
        ADD COLUMN IF NOT EXISTS template_id VARCHAR(100),
        ADD COLUMN IF NOT EXISTS copy_labels VARCHAR(300),
        ADD COLUMN IF NOT EXISTS reason_code VARCHAR(40),
        ADD COLUMN IF NOT EXISTS approval_id VARCHAR(100),
        ADD COLUMN IF NOT EXISTS approved_by VARCHAR(200),
        ADD COLUMN IF NOT EXISTS payload_checksum VARCHAR(64),
        ADD COLUMN IF NOT EXISTS requested_count INT,
        ADD COLUMN IF NOT EXISTS confirmed_count INT,
        ADD COLUMN IF NOT EXISTS outcome VARCHAR(30)
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.print_snapshots (
        txn VARCHAR(100) NOT NULL,
        doc_id VARCHAR(200) NOT NULL,
        revision INT NOT NULL,
        amendment_key TEXT NOT NULL DEFAULT '',
        doctype VARCHAR(100),
        payload JSONB NOT NULL,
        checksum VARCHAR(64) NOT NULL,
        frozen_by VARCHAR(200),
        frozen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (txn, doc_id, revision)
      )
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('ReprintRequest', 'Core', 'core', 'Transaction'),
        ('DocumentReprint', 'Core', 'core', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('ReprintRequest', 'number',      'Document',      'Data',   TRUE,  NULL, 1),
        ('ReprintRequest', 'txn',         'Print',         'Data',   TRUE,  NULL, 2),
        ('ReprintRequest', 'doc_id',      'Document id',   'Data',   TRUE,  NULL, 3),
        ('ReprintRequest', 'reason',      'Reason',        'Data',   TRUE,  NULL, 4),
        ('ReprintRequest', 'amount',      'Printed before','Number', FALSE, NULL, 5),
        ('ReprintRequest', 'status',      'Status',        'Select', TRUE,  'Draft,Pending Approval,Approved,Rejected,Used', 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('HR/Admin', 'DocumentReprint', TRUE, TRUE, FALSE, FALSE),
          ('Store Manager', 'DocumentReprint', TRUE, TRUE, FALSE, FALSE),
          ('HR/Admin', 'ReprintRequest', TRUE, TRUE, FALSE, FALSE),
          ('Store Manager', 'ReprintRequest', TRUE, TRUE, FALSE, FALSE),
          ('Cashier', 'ReprintRequest', TRUE, TRUE, FALSE, FALSE),
          ('Accountant', 'ReprintRequest', TRUE, TRUE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
