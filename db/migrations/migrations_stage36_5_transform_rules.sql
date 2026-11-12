-- ---------------------------------------------------------------------------
-- Stage 36.5: declarative data transformation rules.
--
-- The gap this closes: engines/connector.go's BuildChannelPayload already
-- maps a source field to a target field per ChannelFieldMap row, but that
-- mapping is a pure rename - the value crosses unchanged. There was no way
-- to say "uppercase this", "prefix the SKU with our brand code" or "convert
-- DD/MM/YYYY to ISO" without writing Go. PIMTransformRule is an ordered list
-- of steps drawn from a CLOSED vocabulary the engine implements
-- (engines/pim_transform.go, pimTransformFunctions) - the same shape Stage
-- 36.2.3's workflow condition vocabulary already established for exactly the
-- same reason: a rule is authored by a category manager in a form, and a
-- form that accepts an expression or code is a remote-execution surface.
--
-- One doctype (Master), plus one additive field on the pre-existing
-- ChannelFieldMap doctype so an export mapping can optionally reference a
-- rule. Both changes are additive: a mapping with no transform_rule behaves
-- exactly as it does today (untouched passthrough), and a tenant that never
-- opens the new screen is byte-identical to what it is today.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PIMTransformRule', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.5.1 - PIMTransformRule.
--
-- steps is a JSONTable, the same treatment PIMWorkflowDefinition.stages got
-- in Stage 36.2.3: only ever read as part of its parent rule, never queried
-- across rules, so a second table would only add a join nothing needs.
-- Each step names one function from the closed vocabulary plus up to two
-- literal operands (find_replace_literal is the one function that uses
-- both; every other function uses at most operand1). Unknown functions and
-- missing required operands are refused at author time by
-- ValidatePIMTransformRuleDocument (engines/pim_transform.go), the same
-- "the form cannot offer what the engine cannot run" guarantee 36.2.3 built.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMTransformRule', 'code', 'Rule Code', 'Data', TRUE, NULL, 1),
('PIMTransformRule', 'name', 'Rule Name', 'Data', TRUE, NULL, 2),
('PIMTransformRule', 'description', 'Description', 'Data', FALSE, NULL, 3),
('PIMTransformRule', 'steps', 'Steps', 'JSONTable', TRUE,
 '[{"key":"sequence","label":"Sequence","type":"number","required":true},
   {"key":"function","label":"Function","type":"text","required":true},
   {"key":"operand1","label":"Operand 1","type":"text"},
   {"key":"operand2","label":"Operand 2","type":"text"}]', 4),
('PIMTransformRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.5.3 - the one additive field that lets an existing ChannelFieldMap row
-- opt into a rule. display_order 6 is the next free slot after the five
-- fields ChannelFieldMap has carried since db/migration.sql; blank/unset
-- means BuildChannelPayload's mapping loop behaves exactly as it does today.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ChannelFieldMap', 'transform_rule', 'Transform Rule', 'Link', FALSE, 'PIMTransformRule', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Permissions - same split as PIMWorkflowDefinition (36.2): Super Admin gets
-- full control, Store Manager can author and use rules but not delete one
-- out from under a mapping or import template that still references it.
-- 'Super Admin', not 'HR/Admin' - stage40_3 renamed that role away on an
-- already-migrated database, so inserting it here would create a row no
-- session can ever match (same reasoning as migrations_stage36_2_pim_tasks.sql).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'PIMTransformRule', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PIMTransformRule', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO UPDATE SET
    allow_read   = EXCLUDED.allow_read,
    allow_create = EXCLUDED.allow_create,
    allow_update = EXCLUDED.allow_update,
    allow_delete = EXCLUDED.allow_delete;

-- ---------------------------------------------------------------------------
-- A rule code is quoted by name from an import template column mapping and
-- a ChannelFieldMap row, so it must resolve to exactly one row - the same
-- partial unique index treatment every other PIM master code gets.
-- ---------------------------------------------------------------------------
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_transform_rule_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'PIMTransformRule' AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- PIM-stage migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = 'PIMTransformRule'
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module,
        module_key = EXCLUDED.module_key,
        document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
       WHERE doctype_name IN ('PIMTransformRule', 'ChannelFieldMap')
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label,
        fieldtype = EXCLUDED.fieldtype,
        mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options,
        display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'PIMTransformRule'
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read,
        allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update,
        allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_transform_rule_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'PIMTransformRule' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
