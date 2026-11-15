-- ---------------------------------------------------------------------------
-- Stage 55.11 (ask 179): an exception to a channel's minimum-image rule.
--
-- MediaRuleException: for one item on one channel, a lower number of product
-- images the channel's "Min Images" validation rule accepts, with the reason
-- and an optional last day. It goes through the approval engine; only an
-- Approved exception that is still in date counts (engines/pim_publish.go,
-- evaluateChannelValidationRules). Any other channel rule is unaffected.
--
-- Approval rule Super Admin (as ProductContent) where the client has none for
-- it. Store Managers raise it (Setup › PIM, beside Channel Validation Rule);
-- strict status moves as the 49.5.3 batches.
--
-- Additive and idempotent, replayed into every tenant schema. No client
-- document changes; with no exception every channel behaves as before.
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
      VALUES ('MediaRuleException', 'PIM', 'Master', 'pim')
      ON CONFLICT (name) DO NOTHING
    $sql$, s.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('MediaRuleException', 'item_code',   'Item',                 'Link',   TRUE,  'Item', 1),
        ('MediaRuleException', 'channel',     'Channel',              'Link',   TRUE,  'Channel', 2),
        ('MediaRuleException', 'min_images',  'Images accepted',      'Number', TRUE,  NULL, 3),
        ('MediaRuleException', 'reason',      'Why',                  'Text',   TRUE,  NULL, 4),
        ('MediaRuleException', 'valid_until', 'Last day',             'Date',   FALSE, NULL, 5),
        ('MediaRuleException', 'status',      'Status',               'Select', TRUE,  'Draft,Pending Approval,Approved,Rejected', 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, s.schema_name);

    IF to_regclass(format('%I.role_permissions', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        VALUES ('Store Manager', 'MediaRuleException', TRUE, TRUE, TRUE, FALSE),
               ('Super Admin', 'MediaRuleException', TRUE, TRUE, TRUE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, s.schema_name);
    END IF;

    IF to_regclass(format('%I.approval_rules', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT 'MediaRuleException', 0, NULL, 'Super Admin'
         WHERE NOT EXISTS (SELECT 1 FROM %I.approval_rules WHERE doctype = 'MediaRuleException')
      $sql$, s.schema_name, s.schema_name);
    END IF;

    IF to_regclass(format('%I.prefix_configs', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('MediaRuleException', 'MRE', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, s.schema_name);
    END IF;

    CONTINUE WHEN to_regclass(format('%I.documents', s.schema_name)) IS NULL;

    EXECUTE format($sql$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT
        'STR-MediaRuleException-' || t.from_status || '-' || t.to_status,
        'StatusTransitionRule',
        jsonb_build_object(
          'code',                 'STR-MediaRuleException-' || t.from_status || '-' || t.to_status,
          'entity',               'MediaRuleException',
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
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE WHERE name = 'MediaRuleException'
    $sql$, s.schema_name);
  END LOOP;
END
$mig$;
