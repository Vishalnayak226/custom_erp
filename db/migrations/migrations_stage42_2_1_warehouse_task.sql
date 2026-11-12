-- ---------------------------------------------------------------------------
-- Stage 42.2.1 - the WarehouseTask doctype: one object every floor action
-- emits into, closing the second of Stage 42's two foundational holes (the
-- first, lot/serial traceability, was 42.1). Before this file, PutawayToBin,
-- ExecuteBinReplenishment, ScanPickItem, CrossDockPutaway and
-- PostCycleCountAdjustment were five parallel, unrelated Go functions with no
-- shared queue, priority, assignment or ageing - which is why 26.5.13 could
-- only instrument three of them, and why there has never been a warehouse
-- cockpit (42.2.10). 42.2.2 retrofits those five actions to emit/close a
-- task additively; this migration only creates the object itself.
--
-- module_key 'inventory' matches FulfillmentTask/LPN/BinReplenishmentRule -
-- the precedent every Stage-26.5-and-later WMS transactional doctype in this
-- tree already follows, rather than the 'wms' module_key (that one only
-- gates ROUTES, per routes.go's own moduleGate("wms", ...) convention, not
-- doctype_meta rows).
--
-- `item`/`batch_no` are Data holding the SKU code / lot number, the same
-- reason Batch.item and UOMConversion.item are Data rather than Link: the
-- generic Link check resolves against documents.id, and neither an Item's
-- nor a Batch's document id is the code/number a person actually types or
-- scans. `from_bin`/`to_bin` are Data bin codes for the identical reason
-- Bin is referenced everywhere else in this tree.
--
-- `priority` is a plain Number, higher = more urgent (0 is the default, an
-- unprioritised task) - deliberately not a Select, because 42.2.4's
-- TaskDispatchStrategy master needs to compare and sort on it numerically,
-- and a fixed Low/Medium/High/Urgent enum would have to be re-mapped to
-- numbers there anyway.
--
-- The status lifecycle mirrors Batch's own StatusTransitionRule shape below:
-- Pending (created, unassigned) -> Assigned -> In Progress -> Completed, with
-- Cancelled reachable from any non-terminal state and Exception the one
-- machine/human-flagged detour 42.2.9's exception catalogue will attach
-- follow-on actions to. strict_status_transitions left FALSE, matching
-- 29.8's documented opt-in-strict posture - these rules are seeded and
-- enforceable, not yet bet on being complete.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('WarehouseTask', 'Inventory', 'inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('WarehouseTask', 'task_type', 'Task Type', 'Select', TRUE,
 'Putaway,Pick,Replenish,Count,Move,VAS,Load,Unload', 1),
('WarehouseTask', 'status', 'Status', 'Select', TRUE,
 'Pending,Assigned,In Progress,Completed,Cancelled,Exception', 2),
('WarehouseTask', 'priority', 'Priority (higher = more urgent)', 'Number', FALSE, NULL, 3),
('WarehouseTask', 'location_code', 'Location', 'Data', TRUE, NULL, 4),
('WarehouseTask', 'from_bin', 'From Bin (optional)', 'Data', FALSE, NULL, 5),
('WarehouseTask', 'to_bin', 'To Bin (optional)', 'Data', FALSE, NULL, 6),
('WarehouseTask', 'item', 'Item Code (SKU, optional)', 'Data', FALSE, NULL, 7),
('WarehouseTask', 'batch_no', 'Batch / Lot No (optional)', 'Data', FALSE, NULL, 8),
('WarehouseTask', 'qty', 'Quantity (optional)', 'Number', FALSE, NULL, 9),
('WarehouseTask', 'uom', 'UOM (optional)', 'Data', FALSE, NULL, 10),
('WarehouseTask', 'assigned_to', 'Assigned To (optional)', 'Data', FALSE, NULL, 11),
('WarehouseTask', 'queue', 'Queue (optional)', 'Data', FALSE, NULL, 12),
('WarehouseTask', 'wave_id', 'Wave ID (optional)', 'Data', FALSE, NULL, 13),
('WarehouseTask', 'source_doc_type', 'Source Document Type (optional)', 'Data', FALSE, NULL, 14),
('WarehouseTask', 'source_doc_id', 'Source Document ID (optional)', 'Data', FALSE, NULL, 15),
('WarehouseTask', 'notes', 'Notes (optional)', 'Data', FALSE, NULL, 16)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Same split every WMS floor-ops doctype in this tree uses: HR/Admin full
-- control, Store Manager and Cashier can work tasks (create/update, e.g.
-- self-assign or complete one) but not delete an operational record.
INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'WarehouseTask', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'WarehouseTask', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'WarehouseTask', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
SELECT
  'STR-WarehouseTask-' || v.from_status || '-' || v.to_status,
  'StatusTransitionRule',
  jsonb_build_object(
    'code',                 'STR-WarehouseTask-' || v.from_status || '-' || v.to_status,
    'entity',               'WarehouseTask',
    'from_status',          v.from_status,
    'to_status',            v.to_status,
    'allowed',              'Yes',
    'requires_reason_code', v.needs_reason,
    'status',               'Active'
  ),
  'Active',
  'system'
FROM (VALUES
  ('Pending',     'Assigned',    'No'),
  ('Assigned',    'In Progress', 'No'),
  ('Assigned',    'Pending',     'No'),
  ('In Progress', 'Completed',   'No'),
  ('In Progress', 'Exception',   'No'),
  ('Exception',   'Assigned',    'No'),
  ('Exception',   'Cancelled',   'Yes'),
  ('Pending',     'Cancelled',   'Yes'),
  ('Assigned',    'Cancelled',   'Yes'),
  ('In Progress', 'Cancelled',   'Yes')
) AS v(from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;
