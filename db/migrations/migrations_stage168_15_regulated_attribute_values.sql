-- ---------------------------------------------------------------------------
-- Stage 168.15 (G01): a change to a regulated attribute's value waits for
-- approval.
--
-- ProductAttributeDef gets "regulated" (blank = No). AttributeValueChange is
-- the request: item, attribute, before / after, filed when an existing item's
-- value for a regulated attribute is changed (setting
-- pim.hold_regulated_attribute_changes, off by default) and applied once when
-- a checker approves - the same maker-checker engine as VendorChange.
--
-- Additive and idempotent, replayed into every tenant schema (tenant_default
-- included). No existing row is touched; with the setting off nothing waits.
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
      VALUES ('AttributeValueChange', 'PIM', 'pim', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('ProductAttributeDef', 'regulated', 'Regulated', 'Select', FALSE, ',No,Yes', 12),
        ('AttributeValueChange', 'item', 'Item', 'Data', TRUE, NULL, 1),
        ('AttributeValueChange', 'attribute', 'Attribute', 'Data', TRUE, NULL, 2),
        ('AttributeValueChange', 'locale', 'Locale', 'Data', FALSE, NULL, 3),
        ('AttributeValueChange', 'channel', 'Channel', 'Data', FALSE, NULL, 4),
        ('AttributeValueChange', 'old_value', 'Before', 'Data', FALSE, NULL, 5),
        ('AttributeValueChange', 'new_value', 'After', 'Data', FALSE, NULL, 6),
        ('AttributeValueChange', 'summary', 'What changes', 'Data', FALSE, NULL, 7),
        ('AttributeValueChange', 'requested_by', 'Requested by', 'Data', FALSE, NULL, 8),
        ('AttributeValueChange', 'applied_at', 'Applied at (system)', 'Data', FALSE, NULL, 9),
        ('AttributeValueChange', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 10)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'AttributeValueChange', TRUE, FALSE, FALSE, FALSE),
        ('Store Manager', 'AttributeValueChange', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('AttributeValueChange', 0, NULL, 'Store Manager')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('AttributeValueChange', 'AVC', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    -- Strict approval statuses, the same rules as VendorChange.
    EXECUTE format($sql$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT 'STR-AttributeValueChange-' || t.from_status || '-' || t.to_status, 'StatusTransitionRule',
             jsonb_build_object('code', 'STR-AttributeValueChange-' || t.from_status || '-' || t.to_status,
                                'entity', 'AttributeValueChange', 'from_status', t.from_status, 'to_status', t.to_status,
                                'allowed', 'Yes', 'requires_reason_code', t.needs_reason, 'status', 'Active'),
             'Active', 'system'
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
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE WHERE name = 'AttributeValueChange'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
