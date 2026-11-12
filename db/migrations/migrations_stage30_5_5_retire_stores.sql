-- Stage 30.5.5: retire the `Stores` master and fold its fields into `Location`.
--
-- The problem this closes: `Stores` was promoted in the sidebar and documented
-- in USER_GUIDE / USER_SOP as "your shop/warehouse locations", but it had
-- **zero Link references and zero Go references** - nothing in the system
-- could select a Stores record. Meanwhile `Location` (type Store/Warehouse/HO)
-- is what every transaction actually uses. A user following the manual created
-- their shop somewhere nothing could see it, and the four fields only Stores
-- had - address, city, contact_phone, manager - were write-only by
-- construction: there was nowhere else in the product to record a shop's
-- address or phone number at all.
--
-- User's call (2026-08-02), given the three options in micro_checklist 30.5.5:
-- retire Stores and fold its fields into Location.
--
-- ORDER IS LOAD-BEARING IN THIS FILE. Both `documents` and `doctype_fields`
-- declare `REFERENCES doctype_meta(name) ON DELETE CASCADE`, so dropping the
-- Stores doctype row before its documents have been moved would silently
-- cascade-delete every one of them. The steps below are therefore: add the
-- columns, move the data, and only then remove the doctype - with the removal
-- guarded on there being nothing left to lose.

-- ---------------------------------------------------------------------------
-- 1. Location gains the four fields only Stores had.
--
-- Additive and optional (mandatory = FALSE), so every one of the existing
-- Location records stays valid exactly as it is - `ValidateDocument` only
-- enforces mandatory fields, and none of these are. Fieldnames are kept
-- byte-identical to the Stores ones (`address`, `city`, `contact_phone`,
-- `manager`) specifically so step 2 can re-point a document without rewriting
-- a single key inside its `data` blob.
--
-- `manager` is labelled "Manager" rather than Stores' "Store Manager" because
-- a Location may be a Warehouse or the HO, not only a shop. The fieldname is
-- unchanged, so this is a label change only and no stored data moves.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Location', 'address', 'Address', 'Data', FALSE, NULL, 6),
('Location', 'city', 'City', 'Data', FALSE, NULL, 7),
('Location', 'contact_phone', 'Contact Phone', 'Data', FALSE, NULL, 8),
('Location', 'manager', 'Manager', 'Data', FALSE, NULL, 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2. Move every Stores record to Location.
--
-- An UPDATE, not an INSERT + DELETE: `documents.id` is the table's primary key
-- and is therefore unique across *all* doctypes in the schema, which means a
-- Stores row's id cannot already be taken by a Location and no id remapping is
-- possible or needed. Re-pointing `doctype` in place also preserves created_by,
-- created_at and the row's history rather than minting a new record.
--
-- `type` is mandatory on Location, so it is set here; 'Store' is right by
-- definition for anything that was a Stores record. `data || jsonb` sets it
-- without disturbing the keys already present, and COALESCE leaves an existing
-- `type` alone on a re-run.
-- ---------------------------------------------------------------------------
UPDATE tenant_default.documents
SET doctype = 'Location',
    data = data || jsonb_build_object('type', COALESCE(data->>'type', 'Store')),
    updated_at = CURRENT_TIMESTAMP
WHERE doctype = 'Stores';

-- ---------------------------------------------------------------------------
-- 3. Remove the retired doctype - but only once it is genuinely empty.
--
-- The guard is the whole point: if step 2 somehow did not move everything, this
-- DELETE would cascade through `documents` and destroy the remainder. Written
-- so that a partial migration leaves the doctype standing (visible, fixable)
-- instead of taking the data with it. Re-running the file after fixing the data
-- completes the retirement.
--
-- role_permissions and doctype_fields cascade from this row by declaration, so
-- they need no separate statement.
-- ---------------------------------------------------------------------------
DELETE FROM tenant_default.doctype_meta
WHERE name = 'Stores'
  AND NOT EXISTS (SELECT 1 FROM tenant_default.documents WHERE doctype = 'Stores');
