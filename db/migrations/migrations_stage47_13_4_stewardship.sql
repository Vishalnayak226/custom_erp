-- ---------------------------------------------------------------------------
-- Stage 47.13.4: stewardship of product attributes.
--
-- StewardshipChange: a request to set or remove one attribute's stewardship
-- entry; it goes through the approval engine and is applied when approved.
-- AttributeStewardship: the approved entry (system of record, owner, steward,
-- confidence, effective date, survivorship, markets / channels / languages).
-- ProductAttributeValue gets source, source_ref and confidence.
--
-- Additive and idempotent, replayed into every tenant schema. No existing row
-- changes: with no approved entry every attribute write behaves as before.
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
      VALUES ('StewardshipChange', 'PIM', 'pim', 'Master'),
             ('AttributeStewardship', 'PIM', 'pim', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('StewardshipChange', 'action', 'Action', 'Select', TRUE, 'Set,Remove', 1),
        ('StewardshipChange', 'attribute', 'Attribute', 'Data', TRUE, NULL, 2),
        ('StewardshipChange', 'system_of_record', 'System of record', 'Select', FALSE, ',ERP,Import,Channel,Person', 3),
        ('StewardshipChange', 'sor_ref', 'Channel or person', 'Data', FALSE, NULL, 4),
        ('StewardshipChange', 'data_owner', 'Owner', 'Data', FALSE, NULL, 5),
        ('StewardshipChange', 'data_steward', 'Steward', 'Data', FALSE, NULL, 6),
        ('StewardshipChange', 'confidence', 'Confidence', 'Select', FALSE, ',High,Medium,Low', 7),
        ('StewardshipChange', 'effective_from', 'Effective from', 'Date', FALSE, NULL, 8),
        ('StewardshipChange', 'survivorship', 'Survivorship', 'Select', FALSE, ',Latest write wins,System of record wins', 9),
        ('StewardshipChange', 'markets', 'Markets', 'Data', FALSE, NULL, 10),
        ('StewardshipChange', 'channels', 'Channels', 'Data', FALSE, NULL, 11),
        ('StewardshipChange', 'languages', 'Languages', 'Data', FALSE, NULL, 12),
        ('StewardshipChange', 'applied_at', 'Applied at (system)', 'Data', FALSE, NULL, 13),
        ('StewardshipChange', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 14),
        ('AttributeStewardship', 'attribute', 'Attribute', 'Data', TRUE, NULL, 1),
        ('AttributeStewardship', 'system_of_record', 'System of record', 'Data', FALSE, NULL, 2),
        ('AttributeStewardship', 'sor_ref', 'Channel or person', 'Data', FALSE, NULL, 3),
        ('AttributeStewardship', 'data_owner', 'Owner', 'Data', FALSE, NULL, 4),
        ('AttributeStewardship', 'data_steward', 'Steward', 'Data', FALSE, NULL, 5),
        ('AttributeStewardship', 'confidence', 'Confidence', 'Data', FALSE, NULL, 6),
        ('AttributeStewardship', 'effective_from', 'Effective from', 'Data', FALSE, NULL, 7),
        ('AttributeStewardship', 'survivorship', 'Survivorship', 'Data', FALSE, NULL, 8),
        ('AttributeStewardship', 'markets', 'Markets', 'Data', FALSE, NULL, 9),
        ('AttributeStewardship', 'channels', 'Channels', 'Data', FALSE, NULL, 10),
        ('AttributeStewardship', 'languages', 'Languages', 'Data', FALSE, NULL, 11),
        ('AttributeStewardship', 'change_id', 'Change request', 'Data', FALSE, NULL, 12),
        ('ProductAttributeValue', 'source', 'Source', 'Select', FALSE, ',ERP,Import,Channel,Person', 20),
        ('ProductAttributeValue', 'source_ref', 'Source channel or person', 'Data', FALSE, NULL, 21),
        ('ProductAttributeValue', 'confidence', 'Confidence', 'Select', FALSE, ',High,Medium,Low', 22)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'StewardshipChange', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'StewardshipChange', TRUE, TRUE, TRUE, FALSE),
        ('HR/Admin', 'AttributeStewardship', TRUE, FALSE, FALSE, FALSE),
        ('Store Manager', 'AttributeStewardship', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('StewardshipChange', 0, NULL, 'Store Manager')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('StewardshipChange', 'STC', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
