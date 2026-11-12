-- ---------------------------------------------------------------------------
-- Stage 42.2.9 - Exception-code catalogue, extending the existing ReasonCode
-- master (26.12.9) rather than a parallel doctype - same "append, don't
-- replace, the category options" precedent migrations_stage26_5_wms_enterprise.sql
-- already set for 'Cycle Count Variance'.
--
-- `process_step`/`follow_on_action` are additive optional fields, blank for
-- every ReasonCode row created before this migration (Cancellation/Hold/
-- Return/Allocation Exception/Other keep behaving exactly as they do today -
-- requireActiveReasonCode, engines/orders.go, never reads either field).
-- They only mean something for a row tagged category='WMS Exception', which
-- is what TransitionWarehouseTaskStatus (engines/warehouse_task.go) now
-- requires when moving a WarehouseTask into Exception status, reusing that
-- exact choke point rather than adding a second reason-code gate for tasks.
-- ---------------------------------------------------------------------------
UPDATE tenant_default.doctype_fields
SET options = options || ',WMS Exception'
WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
  AND options NOT LIKE '%WMS Exception%';

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ReasonCode', 'process_step', 'Process Step (WMS Exception only)', 'Select', FALSE,
 'Receiving,Putaway,Pick,Pack,Count,Replenish,Move', 5),
('ReasonCode', 'follow_on_action', 'Follow-On Action (WMS Exception only)', 'Select', FALSE,
 'Reallocate,Hold,Create Count Task,Notify,None', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- WarehouseTask.reason_code - same "additive JSON key, registered for
-- generic-table visibility only" precedent AssignTasksToWave's wave_id
-- already set (migrations_stage26_5_wms_enterprise.sql). Written by
-- TransitionWarehouseTaskStatus (engines/warehouse_task.go), not typed
-- directly - this row only makes it visible on the generic record view.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('WarehouseTask', 'reason_code', 'Exception Reason Code (optional)', 'Data', FALSE, NULL, 17)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
