-- ---------------------------------------------------------------------------
-- Stage 37.9: Quality & maintenance - inspection plans, CoA, NCR/CAPA,
-- preventive maintenance.
--
-- Pre-build audit found all four completely absent. GRN receiving's QC
-- (Stage 26.5.2) is a pure quantity split (accepted/rejected/damaged) with
-- no structured per-item test/attribute list, so InspectionPlan/CoA are new
-- ground rather than an extension of that path - PostGRNReceiptWithQC
-- itself is deliberately left untouched (a real, riskier change to already
-- complex WMS receiving code, out of this stage's scope). ReasonCode is
-- reused as-is for NCR root-cause classification (a new "Quality" category
-- value, the same append-only pattern every prior stage's own reason-code
-- use already follows) rather than a second classification system.
-- CoA rejection reuses the real, existing TransitionBinStockCondition/
-- bin_stock_batch quarantine mechanism (Stage 42.1) - not a new hold flag.
-- Five doctypes, each with dedicated engine functions enforcing their own
-- explicit transition guards (the IntercompanyTransaction/LandedCostVoucher/
-- ServiceTicket shape this session, not Stage 29.8's generic-API-scoped
-- StatusTransitionRule map).
-- ---------------------------------------------------------------------------

-- ⚠️ A real gap found via this stage's own live-HTTP verification, affecting
-- this stage AND the previously-committed Stage 37.8: neither 'quality' nor
-- 'service' was ever registered as a real module_key (public.modules +
-- tenant_default.module_entitlements), the same "wms"/"oms" registration
-- Stage 27 (migrations_stage27_product_packaging.sql) had to add for THOSE
-- modules before their doctypes' generic-API create path would stop
-- refusing every write with SAAS-0191 "Module disabled for tenant"
-- (handlers_core_doc_engine.go's per-request engines.IsModuleEnabled check).
-- ServiceTicket/CertificateOfAnalysis/NonConformanceReport happened to work
-- in this session's own live checks because they go through DEDICATED
-- handlers (no moduleGate wrapper) - but ServiceContract, InspectionPlan
-- and MaintenanceSchedule, all meant to be created via the generic document
-- API (the Project/CostCenter precedent), were silently unusable until this
-- fix. Registered here, not as a separate migration, since it is the same
-- root cause for both this stage's own doctypes and 37.8's.
INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('quality', 'Quality & Maintenance', 'Inspection plans, certificates of analysis, non-conformance/CAPA, preventive maintenance', FALSE, TRUE),
('service', 'Service Management', 'Service tickets, technician assignment, AMC service contracts', FALSE, TRUE)
ON CONFLICT (module_key) DO NOTHING;

INSERT INTO tenant_default.module_entitlements (module_key, enabled, granted_by, note) VALUES
('quality', TRUE, 'system', NULL),
('service', TRUE, 'system', NULL)
ON CONFLICT (module_key) DO NOTHING;

-- ReasonCode.category gains 'Quality' - append-only, the same precedent
-- migrations_stage35_8_settlement_reconciliation.sql set for 'Settlement'
-- (itself following migrations_stage26_5_wms_enterprise.sql's 'Cycle Count
-- Variance'). ReasonCode.category is a closed-vocabulary Select field, NOT
-- free text - InvestigateNonConformanceReport's requireActiveReasonCode
-- gate (the same choke point Return/Cancellation/Hold/Settlement already
-- use) would refuse every "Quality" reason code without this.
UPDATE tenant_default.doctype_fields
SET options = options || ',Quality'
WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
  AND options NOT LIKE '%Quality%';

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('InspectionPlan', 'Quality', 'quality', 'Master'),
('CertificateOfAnalysis', 'Quality', 'quality', 'Transaction'),
('NonConformanceReport', 'Quality', 'quality', 'Transaction'),
('MaintenanceSchedule', 'Quality', 'quality', 'Master'),
('MaintenanceOrder', 'Quality', 'quality', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- 37.9.1: a reusable, per-item test/attribute definition. test_parameters is
-- a JSONTable, the same "only ever read as part of its parent document"
-- convention every other line-item JSONTable in this codebase already uses.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('InspectionPlan', 'code', 'Plan Code', 'Data', TRUE, NULL, 1),
('InspectionPlan', 'item', 'Item', 'Data', TRUE, NULL, 2),
('InspectionPlan', 'sample_size', 'Sample Size', 'Number', TRUE, NULL, 3),
('InspectionPlan', 'test_parameters', 'Test Parameters', 'JSONTable', TRUE,
 '[{"key":"parameter_name","label":"Parameter","type":"text","required":true},
   {"key":"spec","label":"Specification","type":"text","required":true}]', 4),
('InspectionPlan', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 37.9.2: CertificateOfAnalysis. batch_no is a plain Data field (Batch is
-- not itself a Link-validated doctype target elsewhere in this codebase -
-- traceability.go's BatchInfo lives in `documents` but nothing Links to it
-- by convention), matching how every other batch-identifying field in this
-- system already stores it. inspection_plan is optional - a CoA can record
-- ad-hoc results with no predefined plan.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CertificateOfAnalysis', 'code', 'CoA Number', 'Data', TRUE, NULL, 1),
('CertificateOfAnalysis', 'batch_no', 'Batch/Lot Number', 'Data', TRUE, NULL, 2),
('CertificateOfAnalysis', 'item', 'Item', 'Data', TRUE, NULL, 3),
('CertificateOfAnalysis', 'inspection_plan', 'Inspection Plan (optional)', 'Link', FALSE, 'InspectionPlan', 4),
('CertificateOfAnalysis', 'test_results', 'Test Results', 'JSONTable', TRUE,
 '[{"key":"parameter_name","label":"Parameter","type":"text","required":true},
   {"key":"actual_value","label":"Actual Value","type":"text","required":true},
   {"key":"pass_fail","label":"Pass/Fail","type":"select","options":"Pass,Fail","required":true}]', 5),
('CertificateOfAnalysis', 'overall_result', 'Overall Result (system-managed)', 'Select', FALSE, 'Pass,Fail', 6),
('CertificateOfAnalysis', 'status', 'Status', 'Select', TRUE, 'Draft,Released,Rejected', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 37.9.3: NonConformanceReport. root_cause_reason_code is a ReasonCode
-- reference in the new "Quality" category (validated by
-- requireActiveReasonCode, the existing choke point) rather than a second
-- classification system.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('NonConformanceReport', 'code', 'NCR Number', 'Data', TRUE, NULL, 1),
('NonConformanceReport', 'description', 'Description', 'Data', TRUE, NULL, 2),
('NonConformanceReport', 'source_doctype', 'Source Doctype (optional)', 'Data', FALSE, NULL, 3),
('NonConformanceReport', 'source_id', 'Source Document (optional)', 'Data', FALSE, NULL, 4),
('NonConformanceReport', 'root_cause_reason_code', 'Root Cause', 'Link', FALSE, 'ReasonCode', 5),
('NonConformanceReport', 'corrective_action', 'Corrective Action', 'Data', FALSE, NULL, 6),
('NonConformanceReport', 'status', 'Status', 'Select', TRUE, 'Draft,Investigating,CorrectiveActionPlanned,Closed', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 37.9.4: MaintenanceSchedule (the recurring definition, the
-- RecurringSalesContract shape) + MaintenanceOrder (what gets spawned each
-- cycle, the real trackable work item - the recurring-billing pattern was
-- chosen over the dunning-scan pattern because preventive maintenance needs
-- an actionable, closeable record, not just a notification).
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('MaintenanceSchedule', 'code', 'Schedule Code', 'Data', TRUE, NULL, 1),
('MaintenanceSchedule', 'asset', 'Asset', 'Link', TRUE, 'Asset', 2),
('MaintenanceSchedule', 'description', 'Description', 'Data', TRUE, NULL, 3),
('MaintenanceSchedule', 'interval_days', 'Interval (days)', 'Number', TRUE, NULL, 4),
('MaintenanceSchedule', 'next_due_date', 'Next Due Date', 'Date', TRUE, NULL, 5),
('MaintenanceSchedule', 'status', 'Status', 'Select', TRUE, 'Active,Paused', 6),
('MaintenanceOrder', 'code', 'Order Number', 'Data', TRUE, NULL, 1),
('MaintenanceOrder', 'asset', 'Asset', 'Link', TRUE, 'Asset', 2),
('MaintenanceOrder', 'description', 'Description', 'Data', TRUE, NULL, 3),
('MaintenanceOrder', 'maintenance_schedule_id', 'Maintenance Schedule (optional)', 'Link', FALSE, 'MaintenanceSchedule', 4),
('MaintenanceOrder', 'scheduled_date', 'Scheduled Date', 'Date', FALSE, NULL, 5),
('MaintenanceOrder', 'completion_notes', 'Completion Notes', 'Data', FALSE, NULL, 6),
('MaintenanceOrder', 'status', 'Status', 'Select', TRUE, 'Draft,Scheduled,InProgress,Completed,Cancelled', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'InspectionPlan', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'InspectionPlan', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'CertificateOfAnalysis', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'CertificateOfAnalysis', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'NonConformanceReport', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'NonConformanceReport', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'MaintenanceSchedule', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'MaintenanceSchedule', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'MaintenanceOrder', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'MaintenanceOrder', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- Stage 35-37 migration in this file family uses.
-- ---------------------------------------------------------------------------
-- module_entitlements needs every tenant schema including tenant_default
-- re-touched (ON CONFLICT DO UPDATE, not DO NOTHING) - a schema that already
-- has a 'quality'/'service' row with enabled=FALSE from some other path
-- should not silently stay disabled after this migration says the default
-- is on; the Stage 27 precedent makes the identical choice.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format(
      'INSERT INTO %I.module_entitlements (module_key, enabled, granted_by, granted_at) VALUES '
      '(''quality'', TRUE, ''system'', CURRENT_TIMESTAMP), (''service'', TRUE, ''system'', CURRENT_TIMESTAMP) '
      'ON CONFLICT (module_key) DO UPDATE SET enabled = TRUE',
      schema_rec.schema_name
    );
  END LOOP;
END $$;

DO $$
DECLARE
  schema_rec RECORD;
  meta_doctypes TEXT[] := ARRAY['InspectionPlan', 'CertificateOfAnalysis', 'NonConformanceReport', 'MaintenanceSchedule', 'MaintenanceOrder'];
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format(
      'UPDATE %I.doctype_fields SET options = options || '',Quality'' '
      'WHERE doctype_name = ''ReasonCode'' AND fieldname = ''category'' AND options NOT LIKE ''%%Quality%%''',
      schema_rec.schema_name
    );

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = ANY($1)
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module, module_key = EXCLUDED.module_key, document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name) USING meta_doctypes;

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = ANY($1)
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label, fieldtype = EXCLUDED.fieldtype, mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options, display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name) USING meta_doctypes;

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = ANY($1)
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read, allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update, allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name) USING meta_doctypes;
  END LOOP;
END $$;
