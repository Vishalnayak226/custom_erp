-- ---------------------------------------------------------------------------
-- Stage 57: user QA round, 2026-10-04 (docs/product/erp-user-qa-2026-10-04.md)
--
-- Additive changes, all replayed into every provisioned tenant schema,
-- because the deployed tenants are not tenant_default and a fix declared in
-- one schema only is the failure mode Stage 53.2 had to clean up.
--
-- 1. HSNCode - a catalogue of HSN/SAC numbers for the New Item form (57.3).
--    The user asked for the HSN field on New Item to offer the existing codes
--    and to "create the master" when one is missing. Item.hsn_code stays a
--    free-text field storing the plain number - GST, e-invoice and reports
--    read it as an HSN - and this catalogue only feeds the picker on it. See
--    engines/hsn_catalog.go for why the record id is "HSN-" + the number.
--    Backfilled from the distinct HSNs already on Items, so the picker is
--    useful on day one rather than empty; a tenant's existing HSNs are its own
--    data, so this invents no statutory content.
--
-- 2. DebitNote.reference_po becomes a Link to PurchaseOrder. It was a 'Data'
--    field, so the debit note form rendered a bare text box where the user
--    expected a dropdown of their POs ("reference po is not coming in drop
--    down"). Not mandatory, so existing notes stay valid; only rows whose
--    field is still 'Data' are touched, so re-running changes nothing.
--
-- 3-6 are described where they run below: users.must_change_password and the
-- roles registry (57.17), Zone.name (55.7), RFQ.invited_vendors (55.4), and the
-- barcode policy - Item.barcode optional, ItemBarcode register, a daily
-- PIMDatedBarcodeSeq series (57.7). Existing data is never rewritten.
--
-- Everything is idempotent (ON CONFLICT DO NOTHING / guarded UPDATE).
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    -- Only schemas that actually carry the document model.
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    -- 1. HSNCode doctype.
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('HSNCode', 'Core', 'core', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('HSNCode', 'code', 'Catalogue Code', 'Data', TRUE, NULL, 1),
        ('HSNCode', 'hsn', 'HSN / SAC Code', 'Data', TRUE, NULL, 2),
        ('HSNCode', 'description', 'Description', 'Data', FALSE, NULL, 3),
        ('HSNCode', 'gst_rate', 'Default GST Rate (%%)', 'Number', FALSE, NULL, 4),
        ('HSNCode', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- Same shape as Item's own grants: the role that maintains Items maintains
    -- the codes they carry; the store sees them.
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'HSNCode', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'HSNCode', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- Backfill from existing Items: spaces/dots stripped exactly as
    -- engines.NormalizeHSN does, only well-formed 2-8 digit codes, and the
    -- highest rate seen for a code as its default. Guarded on the 'system'
    -- user the rows are attributed to, since created_by is a foreign key and
    -- a tenant without it must be skipped rather than abort the migration.
    EXECUTE format($sql$
      INSERT INTO %1$I.documents (id, doctype, data, status, created_by)
      SELECT DISTINCT ON (hsn)
             'HSN-' || hsn, 'HSNCode',
             jsonb_build_object('code', 'HSN-' || hsn, 'hsn', hsn, 'description', '', 'status', 'Active')
               || CASE WHEN rate > 0 THEN jsonb_build_object('gst_rate', rate) ELSE '{}'::jsonb END,
             'Active', 'system'
      FROM (
        SELECT regexp_replace(data->>'hsn_code', '[[:space:].]', '', 'g') AS hsn,
               CASE WHEN data->>'gst_rate' ~ '^[0-9]+(\.[0-9]+)?$' THEN (data->>'gst_rate')::numeric END AS rate
        FROM %1$I.documents
        WHERE doctype = 'Item' AND deleted_at IS NULL
      ) s
      WHERE hsn ~ '^([0-9]{4}|[0-9]{6}|[0-9]{8})$'
        AND EXISTS (SELECT 1 FROM %1$I.users WHERE id = 'system')
      ORDER BY hsn, rate DESC NULLS LAST
      ON CONFLICT (id) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- 3. Users and roles (57.17). users.must_change_password is set when an
    --    administrator chose the password (a new account, an admin-issued
    --    reset) and cleared when the user sets their own; apiMiddleware lets
    --    such a session do nothing else. DEFAULT FALSE, so every existing
    --    account - and every password already set - is untouched.
    --    roles is the registry that lets a role exist before anyone holds it.
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'users') THEN
      EXECUTE format('ALTER TABLE %I.users ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT FALSE',
                     schema_rec.schema_name);
      EXECUTE format($sql$
        CREATE TABLE IF NOT EXISTS %I.roles (
          name        VARCHAR(100) PRIMARY KEY,
          description TEXT NOT NULL DEFAULT '',
          created_by  VARCHAR(100),
          created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
      $sql$, schema_rec.schema_name);
    END IF;

    -- 4. Zone gains an optional name (55.7). A Bin's zone must equal an
    --    existing Zone's code, and a master's code is auto-numbered
    --    ("Zone/HQ/2026/000001"), so the user was left typing a code they
    --    could not know ("unable to create bin as no zone"). The Bin form now
    --    picks the zone - with inline create - and this is what it shows.
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT 'Zone', 'name', 'Zone Name', 'Data', FALSE, NULL, 0
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'Zone')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    -- 5. RFQ.invited_vendors (55.4): "how to assign vendors to an RFQ?" -
    --    there was no way to. A JSON array of Vendor codes, kept by the RFQ
    --    screen; optional, so every existing RFQ stays valid.
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT 'RFQ', 'invited_vendors', 'Invited Vendors', 'Data', FALSE, NULL, 6
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'RFQ')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    -- 6. Barcode policy (57.7, engines/barcode_policy.go). Barcode stops
    --    being a field someone must type on every Item: it is issued by the
    --    tenant's policy (at SKU creation by default). Existing barcodes are
    --    untouched. ItemBarcode registers receipt-date barcodes so a scan of
    --    one resolves to its SKU; PIMDatedBarcodeSeq numbers them per day
    --    (an ANNUAL series bucketed by the date key).
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET mandatory = FALSE
      WHERE doctype_name = 'Item' AND fieldname = 'barcode' AND mandatory = TRUE
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('ItemBarcode', 'Inventory', 'inventory', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('ItemBarcode', 'code', 'Barcode', 'Data', TRUE, NULL, 1),
        ('ItemBarcode', 'item', 'Item', 'Link', TRUE, 'Item', 2),
        ('ItemBarcode', 'item_code', 'Item Code', 'Data', FALSE, NULL, 3),
        ('ItemBarcode', 'kind', 'Kind', 'Select', TRUE, 'Dated,Additional', 4),
        ('ItemBarcode', 'received_on', 'Received On', 'Date', FALSE, NULL, 5),
        ('ItemBarcode', 'grn', 'Goods Receipt', 'Data', FALSE, NULL, 6),
        ('ItemBarcode', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'ItemBarcode', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'ItemBarcode', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store)
        VALUES ('PIMDatedBarcodeSeq', '', '', 4, 'ANNUAL', TRUE, FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    -- 2. Debit note's Reference PO as a real PO picker.
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET fieldtype = 'Link', options = 'PurchaseOrder'
      WHERE doctype_name = 'DebitNote' AND fieldname = 'reference_po' AND fieldtype = 'Data'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
