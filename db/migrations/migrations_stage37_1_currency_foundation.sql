-- Stage 37.1.1: currency and effective-dated exchange-rate masters.
--
-- These are normal Finance masters, so manual entry, generic CSV import,
-- audit history and role permissions all come from existing platform paths.
-- Financial documents remain single-currency until 37.1.2; adding the masters
-- first is intentionally non-behavioural for every existing transaction.

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('Currency', 'Finance', 'finance', 'Master'),
('ExchangeRate', 'Finance', 'finance', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Currency', 'code', 'ISO Currency Code', 'Data', TRUE, NULL, 1),
('Currency', 'name', 'Currency Name', 'Data', TRUE, NULL, 2),
('Currency', 'symbol', 'Symbol', 'Data', FALSE, NULL, 3),
('Currency', 'decimal_places', 'Decimal Places', 'Number', TRUE, NULL, 4),
('Currency', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),

('ExchangeRate', 'code', 'Rate Code', 'Data', TRUE, NULL, 1),
('ExchangeRate', 'from_currency', 'From Currency', 'Link', TRUE, 'Currency', 2),
('ExchangeRate', 'to_currency', 'To Currency', 'Link', TRUE, 'Currency', 3),
('ExchangeRate', 'rate', 'Exchange Rate', 'Number', TRUE, NULL, 4),
('ExchangeRate', 'rate_type', 'Rate Type', 'Select', TRUE, 'Spot,Average,Closing', 5),
('ExchangeRate', 'effective_from', 'Effective From', 'Date', TRUE, NULL, 6),
('ExchangeRate', 'effective_to', 'Effective To (blank = open-ended)', 'Date', FALSE, NULL, 7),
('ExchangeRate', 'source', 'Source', 'Select', TRUE, 'Manual,Imported', 8),
('ExchangeRate', 'source_reference', 'Source Reference', 'Data', FALSE, NULL, 9),
('ExchangeRate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Currency', TRUE, TRUE, TRUE, TRUE),
('Super Admin', 'Currency', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Currency', TRUE, FALSE, FALSE, FALSE),
('HR/Admin', 'ExchangeRate', TRUE, TRUE, TRUE, TRUE),
('Super Admin', 'ExchangeRate', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ExchangeRate', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO UPDATE SET
    allow_read = EXCLUDED.allow_read,
    allow_create = EXCLUDED.allow_create,
    allow_update = EXCLUDED.allow_update,
    allow_delete = EXCLUDED.allow_delete;

-- INR is the system's historical implicit functional currency. Seeding only
-- that truth avoids guessing which foreign currencies a tenant trades in.
INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
('INR', 'Currency',
 '{"id":"INR","code":"INR","name":"Indian Rupee","symbol":"₹","decimal_places":2,"status":"Active"}',
 'Active', 'system')
ON CONFLICT (id) DO NOTHING;

-- The generic documents table's primary key is the document id, not the
-- business code inside JSON. These two expression indexes close that gap for
-- currencies/rates so API and CSV callers cannot create ambiguous resolvers.
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_currency_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'Currency' AND deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_exchange_rate_window_unique
    ON tenant_default.documents
       ((data->>'from_currency'), (data->>'to_currency'),
        (COALESCE(data->>'rate_type', 'Spot')), (data->>'effective_from'))
    WHERE doctype = 'ExchangeRate' AND deleted_at IS NULL AND status = 'Active';

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
        FROM tenant_default.doctype_meta WHERE name IN ('Currency', 'ExchangeRate')
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
       WHERE doctype_name IN ('Currency', 'ExchangeRate')
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
        FROM tenant_default.role_permissions
       WHERE doctype_name IN ('Currency', 'ExchangeRate')
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read,
        allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update,
        allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT id, doctype, data, status, created_by
        FROM tenant_default.documents WHERE id = 'INR' AND doctype = 'Currency'
      ON CONFLICT (id) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_currency_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'Currency' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_exchange_rate_window_unique
        ON %I.documents
           ((data->>'from_currency'), (data->>'to_currency'),
            (COALESCE(data->>'rate_type', 'Spot')), (data->>'effective_from'))
       WHERE doctype = 'ExchangeRate' AND deleted_at IS NULL AND status = 'Active'
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
