-- ---------------------------------------------------------------------------
-- Stage 148.3 (decision D4): credit control on the order.
--
-- 1. A customer link (customer_id) beside the typed name on orders and
--    invoices; payment_term and hold_since on the order; a payment_term on
--    the invoice. Customer gets payment_term, credit_stop and one_time.
-- 2. CreditHoldRelease: the request an over-limit order leaves in the approval
--    inbox (written by the engine only; Store Manager approves).
-- 3. The back-fill, this item's data-writing step: customer_id is set on an
--    existing SalesOrder (customer_name) / SalesInvoice (customer) ONLY where
--    the typed name matches exactly ONE non-deleted Customer. A name that
--    matches none or several is left unlinked - never guessed. A record that
--    already has a link is not touched.
-- 4. credit_stop = 'Yes' on every existing Customer that already has a credit
--    limit, so the invoice-post refusal they have today stays; from now on a
--    customer without the flag only gets a warning (D4).
--
-- Additive and safe to re-run; tenant_default first, then every other tenant.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('CreditHoldRelease', 'Sales', 'Transaction', 'sales')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CreditHoldRelease', 'order_id', 'Order', 'Data', TRUE, NULL, 1),
('CreditHoldRelease', 'party', 'Customer', 'Data', FALSE, NULL, 2),
('CreditHoldRelease', 'customer_id', 'Customer ID', 'Data', FALSE, NULL, 3),
('CreditHoldRelease', 'order_value', 'Order value', 'Number', FALSE, NULL, 4),
('CreditHoldRelease', 'exposure', 'Exposure with this order', 'Number', FALSE, NULL, 5),
('CreditHoldRelease', 'credit_limit', 'Credit limit', 'Number', FALSE, NULL, 6),
('CreditHoldRelease', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 7),
('SalesOrder', 'customer_id', 'Customer ID', 'Link', FALSE, 'Customer', 15),
('SalesOrder', 'payment_term', 'Payment Terms', 'Link', FALSE, 'PaymentTerm', 16),
('SalesOrder', 'hold_since', 'On hold since', 'Data', FALSE, NULL, 19),
('SalesInvoice', 'customer_id', 'Customer ID', 'Link', FALSE, 'Customer', 103),
('SalesInvoice', 'payment_term', 'Payment Terms', 'Link', FALSE, 'PaymentTerm', 104),
('Customer', 'payment_term', 'Payment Terms', 'Link', FALSE, 'PaymentTerm', 76),
('Customer', 'credit_stop', 'Stop invoices over the limit', 'Select', FALSE, 'No,Yes', 77),
('Customer', 'one_time', 'One-time customer', 'Select', FALSE, 'No,Yes', 78)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'CreditHoldRelease', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'CreditHoldRelease', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('CreditHoldRelease', 0, NULL, 'Store Manager')
ON CONFLICT (doctype, min_amount) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_meta (name, module, document_type, module_key)
        SELECT name, module, document_type, module_key FROM tenant_default.doctype_meta WHERE name = 'CreditHoldRelease'
        ON CONFLICT (name) DO NOTHING
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
        WHERE doctype_name = 'CreditHoldRelease'
           OR (doctype_name, fieldname) IN (('SalesOrder', 'customer_id'), ('SalesOrder', 'payment_term'), ('SalesOrder', 'hold_since'),
                                            ('SalesInvoice', 'customer_id'), ('SalesInvoice', 'payment_term'),
                                            ('Customer', 'payment_term'), ('Customer', 'credit_stop'), ('Customer', 'one_time'))
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'CreditHoldRelease'
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT doctype, min_amount, max_amount, required_role FROM tenant_default.approval_rules WHERE doctype = 'CreditHoldRelease'
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;

-- The back-fill and the legacy stop flag (steps 3 and 4), every schema that
-- holds documents, tenant_default included.
DO $backfill$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = schema_rec.schema_name AND table_name = 'documents') THEN
      CONTINUE;
    END IF;
    EXECUTE format($sql$
      WITH uniq AS (
        SELECT data->>'name' AS nm, MIN(id) AS cid
          FROM %1$I.documents
         WHERE doctype = 'Customer' AND deleted_at IS NULL AND COALESCE(data->>'name', '') <> ''
         GROUP BY data->>'name'
        HAVING COUNT(*) = 1
      )
      UPDATE %1$I.documents d
         SET data = d.data || jsonb_build_object('customer_id', u.cid)
        FROM uniq u
       WHERE d.doctype = 'SalesOrder' AND d.deleted_at IS NULL
         AND COALESCE(d.data->>'customer_id', '') = ''
         AND d.data->>'customer_name' = u.nm
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      WITH uniq AS (
        SELECT data->>'name' AS nm, MIN(id) AS cid
          FROM %1$I.documents
         WHERE doctype = 'Customer' AND deleted_at IS NULL AND COALESCE(data->>'name', '') <> ''
         GROUP BY data->>'name'
        HAVING COUNT(*) = 1
      )
      UPDATE %1$I.documents d
         SET data = d.data || jsonb_build_object('customer_id', u.cid)
        FROM uniq u
       WHERE d.doctype = 'SalesInvoice' AND d.deleted_at IS NULL
         AND COALESCE(d.data->>'customer_id', '') = ''
         AND d.data->>'customer' = u.nm
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %1$I.documents
         SET data = data || '{"credit_stop":"Yes"}'::jsonb
       WHERE doctype = 'Customer' AND deleted_at IS NULL
         AND COALESCE(data->>'credit_stop', '') = ''
         AND CASE WHEN data->>'credit_limit' ~ '^[0-9]+(\.[0-9]+)?$' THEN (data->>'credit_limit')::numeric > 0 ELSE FALSE END
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$backfill$;
