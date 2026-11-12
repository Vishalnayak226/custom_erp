-- Stage 24 addendum (18.2 / 24.37, 2026-07-25): closes the two compounding
-- gaps behind "pickers exist but aren't a real constraint, and Store
-- Manager/Cashier can't even use them":
--
-- 1. Store Manager/Cashier were never granted role_permissions read access
--    to Vendor/Item/Customer, so Stage 18.1's attachTypeahead() pickers on
--    PO/RFQ/POS/GRN screens 403 for those roles today (handleGenericDoc's
--    checkPermission denies-by-default with no matching row - see
--    handlers_core_doc_engine.go). Same read-only shape as the existing
--    Store Manager/Location grant (migrations_stage17h).
-- 2. The handful of fields Stage 18.1 actually wired a typeahead onto were
--    still 'Data' (free text), not 'Link' - the generic Link mechanism
--    (engines/doctype.go ValidateDocument's "Link" case, already used by
--    GRN.po_id/VendorInvoice.vendor_id/Bin.location/etc.) was simply never
--    applied to these specific fields. Each one is verified safe to flip:
--    its create/edit form is the bespoke-screen + attachTypeahead() pattern
--    (which stores the picked record's `code`, and every Vendor/Item/
--    Employee record's `id` is set equal to its `code` at creation - see
--    the `id: code, code, ...` pattern throughout public/app.js), not the
--    generic dynamic-modal Link-<select> (which had its own separate value
--    bug, fixed alongside this in public/app.js).
-- Location fields (PurchaseOrder/TransferOrder already had a bespoke
-- ValidateLocationReference check since Stage 17.9) are handled separately
-- by widening that same choke point's field list, not by this migration.

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Store Manager', 'Vendor', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'Item', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'Customer', TRUE, FALSE, FALSE, FALSE),
('Cashier', 'Item', TRUE, FALSE, FALSE, FALSE),
('Cashier', 'Customer', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Backfill: the live dev DB already has a PurchaseOrder ('PO-2026-01') whose
-- free-text vendor field is "Nike Corp" with no matching Vendor master
-- record - exactly the corruption risk 18.2/24.37 describe. Flipping the
-- fieldtype to 'Link' below without first backfilling this would make that
-- pre-existing, otherwise-legitimate PO fail validation on its next edit
-- (Link's existence check only runs on write, so display/read is
-- unaffected either way, but a future edit would newly 422 with no way to
-- fix it since the vendor it names doesn't exist as a record). Standard
-- "normalize orphaned rows before adding the constraint" backfill.
--
-- 26.11.4: `version` is deliberately NOT listed here even though the
-- already-migrated dev DB has had it since migrations_stage24_security.sql
-- ran (months before this addendum file was added on 2026-07-25) - on a
-- FRESH DB, every file applies once in filename-sorted order, and
-- "...addendum_data_integrity.sql" sorts before "..._security.sql" that
-- actually adds the column, so naming it here would fail with "column
-- version does not exist" on first-ever provisioning (caught by an actual
-- from-scratch migration rehearsal). Omitting it is correct either way:
-- migrations_stage24_security.sql's ADD COLUMN ... NOT NULL DEFAULT 1
-- backfills this row's version to 1 regardless of which order the two
-- files ran in.
INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
VALUES ('Nike Corp', 'Vendor', '{"code":"Nike Corp","name":"Nike Corp","status":"Active"}'::jsonb, 'Active', 'admin')
ON CONFLICT (id) DO NOTHING;

UPDATE tenant_default.doctype_fields SET fieldtype = 'Link', options = 'Vendor'
    WHERE doctype_name = 'PurchaseOrder' AND fieldname = 'vendor' AND fieldtype = 'Data';
UPDATE tenant_default.doctype_fields SET fieldtype = 'Link', options = 'Vendor'
    WHERE doctype_name = 'VendorQuote' AND fieldname = 'vendor' AND fieldtype = 'Data';
UPDATE tenant_default.doctype_fields SET fieldtype = 'Link', options = 'Item'
    WHERE doctype_name = 'BOM' AND fieldname = 'parent_item' AND fieldtype = 'Data';
UPDATE tenant_default.doctype_fields SET fieldtype = 'Link', options = 'Employee'
    WHERE doctype_name = 'Asset' AND fieldname = 'custodian' AND fieldtype = 'Data';
