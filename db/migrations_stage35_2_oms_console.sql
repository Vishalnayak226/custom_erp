-- ---------------------------------------------------------------------------
-- Stage 35.2 / 35.3: the OMS Console, and the order-mutation surface it drives.
--
-- Additive and idempotent throughout. Nothing here rewrites an existing row's
-- meaning: the two new SalesOrder/SalesOrderLine fields are optional, and an
-- order written before this migration reads exactly as it did (an unset
-- `priority` is Normal, an unset line `hold_reason` means the line is not held).
--
-- Context for why the console needed schema at all, given the data already
-- existed: it did not, for the *list*. What it needed was indexes - the
-- console's faceted list, its global search and its order-detail assembly are
-- all lookups into `documents.data` by a JSON key, and the only index that
-- covered those was the catch-all GIN on the whole `data` column, which
-- Postgres will not use for a `data->>'key' = value` btree-style comparison.
-- The rest of this file is Stage 35.3's mutation fields.
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- 1. Stage 35.3.4 - order priority / expedite.
--
-- A Select rather than a Check so the vocabulary can grow (Normal / Expedite
-- today; a real 3PL cut-off tier later) without another migration. Optional:
-- blank normalizes to Normal everywhere it is read, which is what keeps every
-- pre-existing order sorting exactly as before.
--
-- The consumer is pick-list generation ordering and the console's own list
-- sort - engines/oms_console.go orders Expedite first, then newest-first.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SalesOrder', 'priority', 'Priority', 'Select', FALSE, 'Normal,Expedite', 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2. Stage 35.3.1 - item-level hold.
--
-- SalesOrderLine already carried independent per-line status; that was the
-- stated reason 26.12.1 split the doctype in two rather than extending POSCart.
-- What it lacked was the *reason* half of a hold, so a line could be stopped
-- but not routed - the exact asymmetry the order-level hold engine avoided by
-- pairing hold_reason with hold_owner.
--
-- 'On Hold' is added to line_status's own vocabulary in the same breath,
-- because a hold reason on a line whose status cannot say "held" would be
-- invisible on every screen that reads status.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SalesOrderLine', 'hold_reason', 'Hold Reason', 'Data', FALSE, NULL, 8),
('SalesOrderLine', 'hold_owner', 'Hold Owner', 'Data', FALSE, NULL, 9),
-- Stage 35.3.5 - order split. Lines carrying different fulfillment_group
-- values are picked, packed and shipped as independent fulfilments. Blank
-- means "the order's single default group", so nothing pre-existing changes.
('SalesOrderLine', 'fulfillment_group', 'Fulfillment Group', 'Data', FALSE, NULL, 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

UPDATE tenant_default.doctype_fields
   SET options = 'Pending,Reserved,On Hold,Dispatched,Cancelled,Returned'
 WHERE doctype_name = 'SalesOrderLine' AND fieldname = 'line_status';

-- ---------------------------------------------------------------------------
-- 3. Stage 35.2.1 - saved views.
--
-- A doctype rather than a new preferences table: the doctype engine already
-- supplies RBAC, soft delete, audit and a generic list view, so a saved view
-- costs one INSERT here instead of a table, a CRUD API and a screen.
--
-- Transaction, not Master, deliberately - Master doctypes surface in the Setup
-- flyout with a New form, and a saved view is created by pressing "Save this
-- view" on the console, never by filling in a form in Setup.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('OMSSavedView', 'OMS', 'oms', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('OMSSavedView', 'code', 'View ID', 'Data', TRUE, NULL, 1),
('OMSSavedView', 'name', 'View Name', 'Data', TRUE, NULL, 2),
('OMSSavedView', 'owner', 'Owner', 'Data', TRUE, NULL, 3),
('OMSSavedView', 'filter', 'Filter', 'Data', FALSE, NULL, 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 'Super Admin', not 'HR/Admin'. On an already-migrated database (dev, and
-- prod once stage40_3 ships) 'HR/Admin' no longer exists, so inserting it here
-- would create a permissions row no session can ever match. On a fresh
-- database this file sorts first and stage40_3's rename simply finds nothing
-- to do for these rows. Correct under both orderings; engines.IsSuperAdmin
-- accepts either name permanently, so no session breaks either way.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin', 'OMSSavedView', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'OMSSavedView', TRUE, TRUE, TRUE, TRUE),
('Cashier', 'OMSSavedView', TRUE, TRUE, TRUE, TRUE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 4. Indexes for the console's three hot paths.
--
-- All partial (WHERE deleted_at IS NULL) and doctype-scoped, so each stays
-- small: `documents` is one table for every doctype in the system, and an
-- unqualified index on data->>'order_id' would carry a row for every GRN,
-- invoice and notification log as well as the order lines it is meant for.
--
-- The GIN index on `data` (migration.sql:141) does NOT serve these. GIN
-- jsonb_ops answers containment (@>), not the `data->>'key' = $1` equality
-- these queries use, so before this every console filter was a sequential scan
-- of the whole documents table.
-- ---------------------------------------------------------------------------

-- The order list's own filters: doctype+status is already covered by
-- idx_documents_doctype_status, so this adds the two JSON keys it faces.
CREATE INDEX IF NOT EXISTS idx_documents_salesorder_channel
    ON tenant_default.documents ((data->>'channel'))
    WHERE doctype = 'SalesOrder' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_salesorder_channel_order_id
    ON tenant_default.documents ((data->>'channel_order_id'))
    WHERE doctype = 'SalesOrder' AND deleted_at IS NULL;

-- Also the idempotency lookup CreateSalesOrder does on every single channel
-- import, and the dangling-mapping guard added in 35.1.3.
CREATE INDEX IF NOT EXISTS idx_documents_salesorder_hold_reason
    ON tenant_default.documents ((data->>'hold_reason'))
    WHERE doctype = 'SalesOrder' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_salesorder_phone
    ON tenant_default.documents ((data->>'customer_phone'))
    WHERE doctype = 'SalesOrder' AND deleted_at IS NULL;

-- The single most-used join in the whole OMS: every line lookup, the location
-- facet, the order-detail assembly and ReleaseOrderHold all go through it.
CREATE INDEX IF NOT EXISTS idx_documents_salesorderline_order_id
    ON tenant_default.documents ((data->>'order_id'))
    WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_salesorderline_sku
    ON tenant_default.documents ((data->>'sku'))
    WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_salesorderline_location
    ON tenant_default.documents ((data->>'location_code'))
    WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL;

-- Global search's AWB arm, and the order-detail shipments section.
CREATE INDEX IF NOT EXISTS idx_documents_booking_tracking
    ON tenant_default.documents ((data->>'tracking_number'))
    WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_booking_order_id
    ON tenant_default.documents ((data->>'order_id'))
    WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_fulfillmenttask_order_id
    ON tenant_default.documents ((data->>'order_id'))
    WHERE doctype = 'FulfillmentTask' AND deleted_at IS NULL;
