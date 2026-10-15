-- ---------------------------------------------------------------------------
-- Stage 63 (user, 2026-10-15): "Print template should be customisable by the
-- user - drag and drop, placement for each template, mapped product-wise;
-- PO, Vendor PO, Transfer Out, Transfer In, GRN ... my brand name always
-- there, and which store I am generating from."
--
-- PrintTemplate holds one designed layout (public/trade-docs.js renders it,
-- the Print Template Studio edits it):
--   transaction      PurchaseOrder | VendorPO | GRN | TransferOut | TransferIn
--                    | DebitNote | VendorCreditNote
--   vendor / item_families / item_categories
--                    optional mapping - the template is offered only for that
--                    vendor / those products, and the most specific wins
--                    (engines/print_templates.go PrintTemplatesFor)
--   is_default       the one used when nothing more specific matches
--   layout           page size, orientation, margins and the placed elements
--                    (JSON). Its own max_length: a layout with a logo is well
--                    past the 10,000-character default field cap; the
--                    whole-request 2 MB cap still bounds it.
--
-- Additive and idempotent, replayed into every tenant schema.
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
      VALUES ('PrintTemplate', 'Core', 'core', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('PrintTemplate', 'code', 'Template Code', 'Data', TRUE, NULL, 1),
        ('PrintTemplate', 'name', 'Template Name', 'Data', TRUE, NULL, 2),
        ('PrintTemplate', 'transaction', 'Transaction', 'Select', TRUE, 'PurchaseOrder,VendorPO,GRN,TransferOut,TransferIn,DebitNote,VendorCreditNote', 3),
        ('PrintTemplate', 'is_default', 'Default for this transaction', 'Select', FALSE, 'No,Yes', 4),
        ('PrintTemplate', 'vendor', 'Only for vendor', 'Link', FALSE, 'Vendor', 5),
        ('PrintTemplate', 'item_families', 'Only for designs / families (comma-separated)', 'Data', FALSE, NULL, 6),
        ('PrintTemplate', 'item_categories', 'Only for categories (comma-separated)', 'Data', FALSE, NULL, 7),
        ('PrintTemplate', 'layout', 'Layout (designed in the Print Template Studio)', 'Data', FALSE, NULL, 8),
        ('PrintTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET max_length = 1500000
      WHERE doctype_name = 'PrintTemplate' AND fieldname = 'layout' AND max_length IS NULL
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'PrintTemplate', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'PrintTemplate', TRUE, FALSE, FALSE, FALSE),
        ('Cashier', 'PrintTemplate', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('PrintTemplate', 'PT', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
