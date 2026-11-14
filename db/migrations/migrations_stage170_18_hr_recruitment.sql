-- Stage 170.18: recruitment. JobOpening (a position, how many places, open / closed), Applicant (stage
-- is the record's status), JobOffer (accepting one makes a draft Employee). HR only: no role but HR and
-- admins is given any of the three. Additive only.

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
        ('JobOpening', 'HR', 'hr', 'Master'),
        ('Applicant', 'HR', 'hr', 'Transaction'),
        ('JobOffer', 'HR', 'hr', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('JobOpening', 'code', 'Opening no', 'Data', TRUE, NULL, 1),
        ('JobOpening', 'position', 'Position', 'Link', TRUE, 'Position', 2),
        ('JobOpening', 'openings', 'Places', 'Number', TRUE, NULL, 3),
        ('JobOpening', 'location', 'Location', 'Data', FALSE, NULL, 4),
        ('JobOpening', 'status', 'Status', 'Select', TRUE, 'Open,On Hold,Closed', 5),
        ('Applicant', 'code', 'Applicant no', 'Data', TRUE, NULL, 1),
        ('Applicant', 'opening', 'Opening', 'Link', TRUE, 'JobOpening', 2),
        ('Applicant', 'applicant_name', 'Name', 'Data', TRUE, NULL, 3),
        ('Applicant', 'email', 'Email', 'Data', FALSE, NULL, 4),
        ('Applicant', 'phone', 'Phone', 'Data', FALSE, NULL, 5),
        ('Applicant', 'notes', 'Notes', 'Data', FALSE, NULL, 6),
        ('Applicant', 'status', 'Stage', 'Select', TRUE, 'Applied,Screening,Interview,Offer,Hired,Rejected', 7),
        ('JobOffer', 'code', 'Offer no', 'Data', TRUE, NULL, 1),
        ('JobOffer', 'applicant', 'Applicant', 'Link', TRUE, 'Applicant', 2),
        ('JobOffer', 'joining_date', 'Joining date', 'Date', FALSE, NULL, 3),
        ('JobOffer', 'offered_ctc', 'Offered pay (yearly)', 'Number', FALSE, NULL, 4),
        ('JobOffer', 'employee_id', 'Employee made', 'Data', FALSE, NULL, 5),
        ('JobOffer', 'status', 'Status', 'Select', TRUE, 'Draft,Sent,Accepted,Declined', 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin', 'JobOpening', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'JobOpening', TRUE, TRUE, TRUE, TRUE),
        ('Super Admin', 'Applicant', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'Applicant', TRUE, TRUE, TRUE, TRUE),
        ('Super Admin', 'JobOffer', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'JobOffer', TRUE, TRUE, TRUE, TRUE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
