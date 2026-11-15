-- ---------------------------------------------------------------------------
-- Stage 47.13.5 (Left part, ask 179): a supplier or item merge goes through the
-- maker-checker approval engine, like CustomerMerge (Stage 101).
--
-- MasterMerge: a merge request (which master, the record kept, the duplicate,
-- why). engines/master_merge.go RequestMasterMerge records it Draft and sends it
-- for approval; the approval hook carries it out (MergeMasters) and marks it
-- Merged with the merge record's id. With no approval rule for MasterMerge the
-- merge is done at once, as before this file.
--
-- An HR/Admin approval rule is added only where the client has none for
-- MasterMerge. Strict status moves as the 49.5.3 batches: the approval core,
-- Merged written only by the engine. Users read it; nobody creates or edits it
-- through the generic record screens.
--
-- Additive and idempotent, replayed into every tenant schema. No client
-- document changes.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  s RECORD;
BEGIN
  FOR s IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.doctype_meta', s.schema_name)) IS NULL;

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, document_type, module_key)
      VALUES ('MasterMerge', 'Core', 'Transaction', 'core')
      ON CONFLICT (name) DO NOTHING
    $sql$, s.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('MasterMerge', 'master_doctype', 'Record type',   'Select', TRUE,  'Vendor,Item', 1),
        ('MasterMerge', 'keep_id',        'Keep',          'Data',   TRUE,  NULL, 2),
        ('MasterMerge', 'merge_id',       'Merge into it', 'Data',   TRUE,  NULL, 3),
        ('MasterMerge', 'reason',         'Why',           'Data',   FALSE, NULL, 4),
        ('MasterMerge', 'merge_record',   'Merge record',  'Data',   FALSE, NULL, 5),
        ('MasterMerge', 'merge_error',    'Merge error',   'Data',   FALSE, NULL, 6),
        ('MasterMerge', 'status',         'Status',        'Select', TRUE,  'Draft,Pending Approval,Approved,Merged,Rejected', 7)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, s.schema_name);

    IF to_regclass(format('%I.role_permissions', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        VALUES ('HR/Admin', 'MasterMerge', TRUE, FALSE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, s.schema_name);
    END IF;

    IF to_regclass(format('%I.approval_rules', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT 'MasterMerge', 0, NULL, 'HR/Admin'
         WHERE NOT EXISTS (SELECT 1 FROM %I.approval_rules WHERE doctype = 'MasterMerge')
      $sql$, s.schema_name, s.schema_name);
    END IF;

    CONTINUE WHEN to_regclass(format('%I.documents', s.schema_name)) IS NULL;

    EXECUTE format($sql$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT
        'STR-MasterMerge-' || t.from_status || '-' || t.to_status,
        'StatusTransitionRule',
        jsonb_build_object(
          'code',                 'STR-MasterMerge-' || t.from_status || '-' || t.to_status,
          'entity',               'MasterMerge',
          'from_status',          t.from_status,
          'to_status',            t.to_status,
          'allowed',              'Yes',
          'requires_reason_code', t.needs_reason,
          'status',               'Active'
        ),
        'Active',
        'system'
      FROM (VALUES
        ('Draft',            'Pending Approval', 'No'),
        ('Draft',            'Approved',         'No'),
        ('Pending Approval', 'Draft',            'No'),
        ('Pending Approval', 'Approved',         'No'),
        ('Pending Approval', 'Rejected',         'Yes'),
        ('Rejected',         'Draft',            'No'),
        ('Rejected',         'Pending Approval', 'No'),
        ('Approved',         'Pending Approval', 'No')
      ) AS t(from_status, to_status, needs_reason)
      ON CONFLICT (id) DO NOTHING
    $sql$, s.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE WHERE name = 'MasterMerge'
    $sql$, s.schema_name);
  END LOOP;
END
$mig$;
