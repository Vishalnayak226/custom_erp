-- ---------------------------------------------------------------------------
-- Item 47.13.6 (ask 185): declaration packs.
--
-- DeclarationPack turns a shipped pack of declaration fields
-- (engines/pim_declaration_packs.json - the packaged-goods list is an example)
-- on for one category and its children, on one channel or every channel, from
-- an effective date to an optional last day. It names the qualified reviewer
-- and the evidence they checked, and goes through the approval engine. Only an
-- Approved record in date counts (engines/pim_declaration_packs.go): the
-- category's items are not ready to publish until each required field has a
-- value, and approving one adds the pack's attributes the client lacks.
--
-- Approval rule Super Admin where the client has none for it. Raised under
-- Setup › PIM, beside Image Exceptions; strict status moves as 55.11.
--
-- Additive and idempotent, replayed into every tenant schema. No client
-- document changes; with no record every category behaves as before.
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
      VALUES ('DeclarationPack', 'PIM', 'Master', 'pim')
      ON CONFLICT (name) DO NOTHING
    $sql$, s.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('DeclarationPack', 'pack',           'Pack',             'Select', TRUE,  'packaged-goods-in', 1),
        ('DeclarationPack', 'category',       'Category',         'Link',   TRUE,  'Category', 2),
        ('DeclarationPack', 'channel',        'Channel',          'Link',   FALSE, 'Channel', 3),
        ('DeclarationPack', 'effective_from', 'Effective from',   'Date',   TRUE,  NULL, 4),
        ('DeclarationPack', 'effective_to',   'Last day',         'Date',   FALSE, NULL, 5),
        ('DeclarationPack', 'reviewer',       'Reviewed by',      'Data',   TRUE,  NULL, 6),
        ('DeclarationPack', 'evidence',       'Evidence',         'Text',   TRUE,  NULL, 7),
        ('DeclarationPack', 'status',         'Status',           'Select', TRUE,  'Draft,Pending Approval,Approved,Rejected', 8)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, s.schema_name);

    IF to_regclass(format('%I.role_permissions', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        VALUES ('Store Manager', 'DeclarationPack', TRUE, TRUE, TRUE, FALSE),
               ('Super Admin', 'DeclarationPack', TRUE, TRUE, TRUE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, s.schema_name);
    END IF;

    IF to_regclass(format('%I.approval_rules', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT 'DeclarationPack', 0, NULL, 'Super Admin'
         WHERE NOT EXISTS (SELECT 1 FROM %I.approval_rules WHERE doctype = 'DeclarationPack')
      $sql$, s.schema_name, s.schema_name);
    END IF;

    IF to_regclass(format('%I.prefix_configs', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('DeclarationPack', 'DPK', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, s.schema_name);
    END IF;

    CONTINUE WHEN to_regclass(format('%I.documents', s.schema_name)) IS NULL;

    EXECUTE format($sql$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT
        'STR-DeclarationPack-' || t.from_status || '-' || t.to_status,
        'StatusTransitionRule',
        jsonb_build_object(
          'code',                 'STR-DeclarationPack-' || t.from_status || '-' || t.to_status,
          'entity',               'DeclarationPack',
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
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE WHERE name = 'DeclarationPack'
    $sql$, s.schema_name);
  END LOOP;
END
$mig$;
