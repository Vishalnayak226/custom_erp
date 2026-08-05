-- Stage 26.4.10: Supplier portal - submission + QC-approval workflow for
-- supplier-provided product content.
--
-- Product decision taken 2026-08-05: a LIMITED-ROLE LOGIN, not a separate
-- portal application. A second app would mean a second auth system, a second
-- session/token story, a second deployment and a second set of security
-- rules to keep in step with this one - for a workflow that is, in substance,
-- "an outside party fills in a form that an internal reviewer approves". The
-- ERP already has all of that: roles, RBAC per doctype, the maker-checker
-- approval engine, and the generic doctype screens. So a supplier is simply a
-- user whose role can reach exactly one doctype.
--
-- What keeps that safe is the row-level scoping added alongside this
-- migration in handleGenericDoc: a 'Supplier' session can only ever see and
-- write rows whose supplier_code matches the Vendor their user account is
-- tied to (users.supplier_code below). Doctype-level RBAC alone would let
-- every supplier read every other supplier's submissions, which is the one
-- thing this shape must not do.

-- 1. The link from a login to the Vendor it speaks for. Nullable: every
-- existing user has no supplier, which is exactly right - only accounts
-- deliberately created as supplier logins ever get one.
ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS supplier_code VARCHAR(100);

-- 2. The submission itself. A Transaction doctype rather than a Master
-- because it is approval-gated and has a lifecycle, exactly like
-- ProductContent (db/migration.sql) - which is also what it becomes on
-- approval.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('SupplierSubmission', 'PIM', 'pim', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- Field set deliberately mirrors ProductContent's, because an approved
-- submission is copied straight onto a ProductContent row
-- (engines/pim_supplier_portal.go). Keeping the names identical is what lets
-- that copy be a plain field-for-field mapping instead of a translation
-- table that would drift the first time either side gained a field.
--
-- media_url is the one field with no ProductContent counterpart: suppliers
-- have no access to the DAM (Stage 15.2 media library), so they submit a URL
-- and the reviewer decides whether to pull it into the library. Deliberately
-- NOT auto-fetched on approval - a server that downloads arbitrary
-- supplier-supplied URLs is an SSRF surface, and this stage does not need it.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SupplierSubmission', 'code', 'Code', 'Data', TRUE, NULL, 1),
('SupplierSubmission', 'supplier_code', 'Supplier (Vendor)', 'Link', TRUE, 'Vendor', 2),
('SupplierSubmission', 'product_id', 'Product (Item)', 'Link', TRUE, 'Item', 3),
('SupplierSubmission', 'language', 'Language', 'Data', TRUE, NULL, 4),
('SupplierSubmission', 'title', 'Title', 'Data', TRUE, NULL, 5),
('SupplierSubmission', 'short_desc', 'Short Description', 'Data', FALSE, NULL, 6),
('SupplierSubmission', 'long_desc', 'Long Description', 'Data', FALSE, NULL, 7),
('SupplierSubmission', 'seo_title', 'SEO Title', 'Data', FALSE, NULL, 8),
('SupplierSubmission', 'tags', 'Tags (comma-separated)', 'Data', FALSE, NULL, 9),
('SupplierSubmission', 'media_url', 'Image URL (for review)', 'Data', FALSE, NULL, 10),
('SupplierSubmission', 'supplier_notes', 'Notes from Supplier', 'Data', FALSE, NULL, 11),
('SupplierSubmission', 'qc_notes', 'QC Notes (internal)', 'Data', FALSE, NULL, 12),
('SupplierSubmission', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 13)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 3. QC approval reuses the existing maker-checker engine rather than a
-- bespoke review flow (CLAUDE.md: "the maker-checker approval engine for any
-- new approval-gated flow"). The supplier is the maker; HR/Admin is the
-- checker. Rejection comments are already mandatory there (APPROV-0159), so
-- "why was my submission rejected" is answered without new storage.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('SupplierSubmission', 0, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;

-- 4. RBAC. The Supplier role's entire world is one doctype plus read access
-- to Item (it has to name the product it is writing about). No delete: a
-- submitted-then-withdrawn record is part of the audit trail of what a
-- supplier claimed, so it is rejected, never erased.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Supplier', 'SupplierSubmission', TRUE, TRUE, TRUE, FALSE),
('Supplier', 'Item', TRUE, FALSE, FALSE, FALSE),
('HR/Admin', 'SupplierSubmission', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'SupplierSubmission', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 5. Existing tenants were provisioned by copying tenant_default, so
-- everything above has to be replayed into each live tenant schema - the
-- failure mode Stage 30.2.2 cleaned up. Same DO-block catch-up shape as
-- Stage 31.1's, guarded on the tenant actually having the tables involved.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE %I.users ADD COLUMN IF NOT EXISTS supplier_code VARCHAR(100)', schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('SupplierSubmission', 'PIM', 'pim', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT * FROM (VALUES
        ('SupplierSubmission', 'code', 'Code', 'Data', TRUE, NULL::text, 1),
        ('SupplierSubmission', 'supplier_code', 'Supplier (Vendor)', 'Link', TRUE, 'Vendor', 2),
        ('SupplierSubmission', 'product_id', 'Product (Item)', 'Link', TRUE, 'Item', 3),
        ('SupplierSubmission', 'language', 'Language', 'Data', TRUE, NULL, 4),
        ('SupplierSubmission', 'title', 'Title', 'Data', TRUE, NULL, 5),
        ('SupplierSubmission', 'short_desc', 'Short Description', 'Data', FALSE, NULL, 6),
        ('SupplierSubmission', 'long_desc', 'Long Description', 'Data', FALSE, NULL, 7),
        ('SupplierSubmission', 'seo_title', 'SEO Title', 'Data', FALSE, NULL, 8),
        ('SupplierSubmission', 'tags', 'Tags (comma-separated)', 'Data', FALSE, NULL, 9),
        ('SupplierSubmission', 'media_url', 'Image URL (for review)', 'Data', FALSE, NULL, 10),
        ('SupplierSubmission', 'supplier_notes', 'Notes from Supplier', 'Data', FALSE, NULL, 11),
        ('SupplierSubmission', 'qc_notes', 'QC Notes (internal)', 'Data', FALSE, NULL, 12),
        ('SupplierSubmission', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 13)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'SupplierSubmission')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
      VALUES ('SupplierSubmission', 0, NULL, 'HR/Admin')
      ON CONFLICT (doctype, min_amount) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('Supplier', 'SupplierSubmission', TRUE, TRUE, TRUE, FALSE),
        ('Supplier', 'Item', TRUE, FALSE, FALSE, FALSE),
        ('HR/Admin', 'SupplierSubmission', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'SupplierSubmission', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
