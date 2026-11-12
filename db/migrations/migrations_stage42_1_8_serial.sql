-- ---------------------------------------------------------------------------
-- Stage 42.1.8 - the SerialNumber register.
--
-- 42.D3 resolved 2026-08-17: batch AND serial, not batch-only. This closes
-- the serial half of the traceability foundation db/migrations_stage42_1_
-- traceability.sql opened for batch. Item.tracking_mode already carries
-- 'Serial' and 'Batch and Serial' as valid values (that migration, 42.1.1)
-- and ItemTracking.TracksSerial() already answers correctly - both were
-- built ahead of this decision on purpose, so nothing already shipped is
-- reworked here.
--
-- The one design decision worth recording, because it is the one place this
-- deliberately does NOT mirror Batch: a SerialNumber is not given a further
-- breakdown table the way bin_stock_batch breaks down bin_stock. A batch
-- needs one because many units share one lot row (bin_stock_batch.qty).
-- A serial number IS one unit - there is nothing to sum. So the SerialNumber
-- document itself carries current_bin/status directly, updated in place as
-- the unit moves, the same shape a FulfillmentTask uses for its own
-- lifecycle rather than the bin_stock_batch shape. See
-- engines/serial_tracking.go's header for the rest of this reasoning.
--
-- Everything here is additive, the same posture as the batch migration:
-- a tenant that never sets tracking_mode = Serial or Batch and Serial is
-- byte-identical to what it is today.
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- The SerialNumber master.
--
-- module_key 'master_data', same taxonomy slot Batch uses - a warehouse with
-- serialised stock needs this register visible with no moduleGate hiding it,
-- exactly as Batch's own note explains.
--
-- `item` and `batch_no` are Data, not Link, for the identical reason Batch's
-- `item` is: the generic Link check (META-0198) resolves against
-- documents.id, and neither an Item's code nor a Batch's batch_no is its
-- document id in this tree. Existence is checked against data->>'code' /
-- (item, batch_no) in engines/master_data_validation.go instead.
--
-- `current_bin` and `location_code` start blank at registration (a unit is
-- registered the moment it is received, before it has been put away) and are
-- filled in by PutawaySerial - the serial analogue of RecordBatchPutaway,
-- and the same separation of "receive the identity" from "place the stock"
-- batch already draws.
--
-- `reserved_for` is new ground Batch does not have: the voucher (pick task /
-- sales order) a unit is currently allocated to, stamped on the
-- InStock/Returned -> Allocated transition and checked on Allocated ->
-- Shipped. This is what makes "verify at pack" a real check for serial
-- rather than a bare status flip - the plan's wording for 42.1.8 asks for
-- verification batch's own pick/pack path never had to provide, because a
-- lot is fungible across an order and a serialised unit is not.
--
-- status: InStock/Allocated/Shipped/Returned/Scrapped, matching the plan's
-- own field spec (docs/specs/wms_parity_plan.md:195-198) verbatim.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('SerialNumber', 'Master Data', 'master_data', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SerialNumber', 'serial_no', 'Serial Number', 'Data', TRUE, NULL, 1),
('SerialNumber', 'item', 'Item Code (SKU)', 'Data', TRUE, NULL, 2),
('SerialNumber', 'batch_no', 'Batch / Lot No (optional)', 'Data', FALSE, NULL, 3),
('SerialNumber', 'current_bin', 'Current Bin', 'Data', FALSE, NULL, 4),
('SerialNumber', 'location_code', 'Location', 'Data', FALSE, NULL, 5),
('SerialNumber', 'vendor', 'Supplier', 'Data', FALSE, NULL, 6),
('SerialNumber', 'owner', 'Owner (3PL, optional)', 'Data', FALSE, NULL, 7),
('SerialNumber', 'reserved_for', 'Reserved For (voucher)', 'Data', FALSE, NULL, 8),
('SerialNumber', 'notes', 'Notes', 'Data', FALSE, NULL, 9),
('SerialNumber', 'status', 'Status', 'Select', TRUE,
 'InStock,Allocated,Shipped,Returned,Scrapped', 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Same three-role shape as Batch's: Store Manager creates/updates but does
-- not delete (a SerialNumber is chain-of-custody evidence, retired via
-- status = Scrapped, never removed); HR/Admin keeps delete for the
-- mistyped-row case; Cashier is read-only.
INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'SerialNumber', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'SerialNumber', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'SerialNumber', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- The lookup every receive/putaway/allocate/ship call makes: (item, serial_no)
-- -> the one document. Same non-unique index Batch's own (item, batch_no)
-- index is (uniqueness is enforced in validateSerialMasterRules, where it
-- can produce a real message instead of a constraint-violation stack trace).
CREATE INDEX IF NOT EXISTS idx_documents_serial_item_no
    ON tenant_default.documents ((data->>'item'), (data->>'serial_no'))
    WHERE doctype = 'SerialNumber';

-- "Where is this unit now" - the serial-inquiry report's access path.
CREATE INDEX IF NOT EXISTS idx_documents_serial_bin
    ON tenant_default.documents ((data->>'current_bin'))
    WHERE doctype = 'SerialNumber';

-- ---------------------------------------------------------------------------
-- The stock ledger learns about serial numbers, the same additive
-- field-registration-only shape 42.1.3 used for batch_no: the column is a
-- JSON key inside documents.data, so this row only teaches the generic doc
-- viewer and the report drill-down to render it. The writing is Go-side
-- (engines/serial_tracking.go).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('StockLedgerEntry', 'serial_no', 'Serial Number', 'Data', FALSE, NULL, 15)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- SerialPutaway/SerialAllocate/SerialShip/SerialReturn/SerialScrap are real
-- voucher types written by engines/serial_tracking.go; widening the Select
-- keeps the generic doc form's dropdown honest. Appended with a NOT LIKE
-- guard rather than matched against the exact prior value the way the batch
-- migration's widening is: this file's embed-glob sort position falls
-- *before* migrations_stage42_1_traceability.sql (digits sort before letters,
-- so "..._1_8_serial" < "..._1_traceability" byte-wise), and on a brand-new
-- database both are still pending, so this one can legitimately run first.
-- Appending onto whatever is already there, guarded on SerialPutaway not yet
-- being present, gives the correct result either order it runs in - unlike
-- an exact-match WHERE, which would silently no-op if it ran before the
-- batch migration had widened the list yet.
UPDATE tenant_default.doctype_fields
SET options = options || ',SerialPutaway,SerialAllocate,SerialShip,SerialReturn,SerialScrap'
WHERE doctype_name = 'StockLedgerEntry' AND fieldname = 'voucher_type'
  AND options NOT LIKE '%SerialPutaway%';

-- ---------------------------------------------------------------------------
-- The lifecycle a SerialNumber can move through.
--
-- Same StatusTransitionRule shape 42.1.6 used for Batch, and the same
-- opt-in-strict posture (doctype_meta.strict_status_transitions left FALSE
-- for SerialNumber): these rules are seeded and enforceable for the day
-- strict is switched on, but that is a later decision, not this one.
--
-- InStock -> Allocated -> Shipped is the normal receive-to-dispatch path.
-- Allocated -> InStock is a cancelled pick (the unit goes back on the
-- shelf). Shipped -> Returned is an RMA; Returned -> InStock is the restock
-- once inspected. Scrapped is reachable from every live state and is
-- terminal - a scrapped unit's identity is retired, a new physical unit
-- (even one that later carries the same serial printed on a replacement
-- label) is received and registered fresh. Every edge INTO Scrapped, and the
-- Shipped -> Returned edge, requires a reason: "who scrapped this unit and
-- why" and "why did this order come back" are exactly the questions a
-- traceability register exists to answer.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
SELECT
  'STR-SerialNumber-' || v.from_status || '-' || v.to_status,
  'StatusTransitionRule',
  jsonb_build_object(
    'code',                 'STR-SerialNumber-' || v.from_status || '-' || v.to_status,
    'entity',               'SerialNumber',
    'from_status',          v.from_status,
    'to_status',            v.to_status,
    'allowed',              'Yes',
    'requires_reason_code', v.needs_reason,
    'status',               'Active'
  ),
  'Active',
  'system'
FROM (VALUES
  ('InStock',   'Allocated', 'No'),
  ('Allocated', 'InStock',   'No'),
  ('Allocated', 'Shipped',   'No'),
  ('Shipped',   'Returned',  'Yes'),
  ('Returned',  'InStock',   'No'),
  ('InStock',   'Scrapped',  'Yes'),
  ('Allocated', 'Scrapped',  'Yes'),
  ('Returned',  'Scrapped',  'Yes')
) AS v(from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;
