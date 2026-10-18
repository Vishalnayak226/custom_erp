-- ---------------------------------------------------------------------------
-- Stage 55.13 - quality and maintenance a person can run end to end.
--
-- QualityInspection was production-only (production_order_id, Pass/Fail,
-- remarks). It now names its source - Production (a ProductionOrder),
-- Incoming (a GRN) or Outgoing (a SalesOrder) - the item, lot, location and
-- quantity inspected, the inspection plan, the readings taken against that
-- plan, and the disposition a quality person decided (who, why, and the stock
-- hold or non-conformance report it raised). production_order_id stays the
-- field the production QC gate reads; it is simply no longer required for an
-- incoming or outgoing inspection. InspectionPlan says which stage it applies
-- to.
--
-- NonConformanceReport gains the item and lot, an accountable owner and due
-- date for the corrective action, and the evidence it was closed on.
-- MaintenanceSchedule gains a task checklist; MaintenanceOrder carries that
-- checklist plus the parts and hours used, and who started and completed it.
--
-- All written by engines/quality_inspection.go through /api/v1/quality/*.
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
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT v.d, v.f, v.l, v.t, FALSE, v.o, v.n
        FROM (VALUES
          ('QualityInspection', 'source_type', 'Inspecting', 'Select', 'Production,Incoming,Outgoing', 10),
          ('QualityInspection', 'source_id', 'Source document', 'Data', NULL, 11),
          ('QualityInspection', 'item', 'Item', 'Data', NULL, 12),
          ('QualityInspection', 'batch_no', 'Batch / lot', 'Data', NULL, 13),
          ('QualityInspection', 'location', 'Location', 'Data', NULL, 14),
          ('QualityInspection', 'qty_inspected', 'Quantity inspected', 'Number', NULL, 15),
          ('QualityInspection', 'inspection_plan', 'Inspection plan', 'Link', 'InspectionPlan', 16),
          ('QualityInspection', 'readings', 'Readings', 'Data', NULL, 17),
          ('QualityInspection', 'disposition', 'Disposition', 'Select', 'Accept,Hold stock,Rework,Use as-is (concession)', 18),
          ('QualityInspection', 'disposition_note', 'Disposition note', 'Data', NULL, 19),
          ('QualityInspection', 'disposed_by', 'Disposed by', 'Data', NULL, 20),
          ('QualityInspection', 'hold_id', 'Stock hold', 'Data', NULL, 21),
          ('QualityInspection', 'ncr_id', 'Non-conformance report', 'Data', NULL, 22),
          ('QualityInspection', 'inspected_by', 'Inspected by', 'Data', NULL, 23),
          ('InspectionPlan', 'applies_to', 'Applies to', 'Select', 'Incoming,Production,Outgoing', 6),
          ('NonConformanceReport', 'item', 'Item', 'Data', NULL, 10),
          ('NonConformanceReport', 'batch_no', 'Batch / lot', 'Data', NULL, 11),
          ('NonConformanceReport', 'owner', 'Action owner', 'Data', NULL, 12),
          ('NonConformanceReport', 'due_date', 'Action due', 'Date', NULL, 13),
          ('NonConformanceReport', 'closure_evidence', 'Closed on evidence', 'Data', NULL, 14),
          ('NonConformanceReport', 'closed_by', 'Closed by', 'Data', NULL, 15),
          ('MaintenanceSchedule', 'checklist', 'Task checklist (one task per line)', 'Text', NULL, 10),
          ('MaintenanceOrder', 'checklist', 'Task checklist', 'Data', NULL, 10),
          ('MaintenanceOrder', 'parts_used', 'Parts used', 'Data', NULL, 11),
          ('MaintenanceOrder', 'labour_hours', 'Hours', 'Number', NULL, 12),
          ('MaintenanceOrder', 'started_by', 'Started by', 'Data', NULL, 13),
          ('MaintenanceOrder', 'completed_by', 'Completed by', 'Data', NULL, 14),
          ('MaintenanceOrder', 'completed_at', 'Completed at', 'Data', NULL, 15)
        ) AS v(d, f, l, t, o, n)
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = v.d)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    -- An incoming or outgoing inspection has no production order.
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET mandatory = FALSE
       WHERE doctype_name = 'QualityInspection' AND fieldname = 'production_order_id' AND mandatory
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
