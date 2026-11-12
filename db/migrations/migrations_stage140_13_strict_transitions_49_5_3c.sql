-- 49.5.3 (third batch): strict status transitions for 21 more doctypes that
-- failed open (no usable rule, strict_status_transitions FALSE), so a
-- hand-crafted status on the generic document API is refused for them too.
-- Named to sort after the last migration that registers a doctype or its
-- statuses (the runner orders by stage number). Same shape as
-- 130_16_4_strict_money_stock_transitions; additive: rules ON CONFLICT DO
-- NOTHING, the flag only ever set TRUE, and a document already in a status its
-- doctype does not declare keeps the 29.8 escape hatch.
--
--   * Approval core (Draft / Pending Approval / Approved / Rejected, rejection
--     needs a reason): BackdatedPostingRequest, PeriodReopenRequest,
--     GRNExcessApproval, HoldReleaseRequest, QualityInspection, and
--     SalesQuotation, ReprintRequest, ItemTreatmentCorrection, CustomerMerge
--     with their engine-only last step (Converted, Used, Applied, Merged).
--   * Own moves: RecurringSalesContract and ServiceContract. EmployeeLoan
--     (disburse / repayment), POSHeldBill (resume / discard) and POSPriceOverride
--     (approval, checkout) have no generic move: every status after the first
--     is the engine's.
--   * Already had rules, now strict: Batch, SerialNumber, ShippingPackage,
--     GatePass, Wave, WarehouseTask, LoadingTask.

INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
SELECT
  'STR-' || e.entity || '-' || t.from_status || '-' || t.to_status,
  'StatusTransitionRule',
  jsonb_build_object(
    'code',                 'STR-' || e.entity || '-' || t.from_status || '-' || t.to_status,
    'entity',               e.entity,
    'from_status',          t.from_status,
    'to_status',            t.to_status,
    'allowed',              'Yes',
    'requires_reason_code', t.needs_reason,
    'status',               'Active'
  ),
  'Active',
  'system'
FROM (VALUES
  ('BackdatedPostingRequest'), ('PeriodReopenRequest'), ('GRNExcessApproval'), ('HoldReleaseRequest'),
  ('QualityInspection'), ('SalesQuotation'), ('ReprintRequest'), ('ItemTreatmentCorrection'), ('CustomerMerge')
) AS e(entity)
CROSS JOIN (VALUES
  ('Draft',            'Pending Approval', 'No'),
  ('Draft',            'Approved',         'No'),
  ('Pending Approval', 'Draft',            'No'),
  ('Pending Approval', 'Approved',         'No'),
  ('Pending Approval', 'Rejected',         'Yes'),
  ('Rejected',         'Draft',            'No'),
  ('Rejected',         'Pending Approval', 'No'),
  ('Approved',         'Pending Approval', 'No')
) AS t(from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
SELECT
  'STR-' || v.entity || '-' || v.from_status || '-' || v.to_status,
  'StatusTransitionRule',
  jsonb_build_object(
    'code',                 'STR-' || v.entity || '-' || v.from_status || '-' || v.to_status,
    'entity',               v.entity,
    'from_status',          v.from_status,
    'to_status',            v.to_status,
    'allowed',              'Yes',
    'requires_reason_code', v.needs_reason,
    'status',               'Active'
  ),
  'Active',
  'system'
FROM (VALUES
  ('RecurringSalesContract', 'Active',  'Paused',    'No'),
  ('RecurringSalesContract', 'Paused',  'Active',    'No'),
  ('RecurringSalesContract', 'Active',  'Cancelled', 'No'),
  ('RecurringSalesContract', 'Paused',  'Cancelled', 'No'),
  ('ServiceContract',        'Active',  'Expired',   'No'),
  ('ServiceContract',        'Active',  'Cancelled', 'No'),
  ('ServiceContract',        'Expired', 'Active',    'No')
) AS v(entity, from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;

UPDATE tenant_default.doctype_meta
   SET strict_status_transitions = TRUE
 WHERE name IN ('EmployeeLoan', 'RecurringSalesContract', 'POSHeldBill', 'ServiceContract', 'POSPriceOverride',
                'BackdatedPostingRequest', 'PeriodReopenRequest', 'GRNExcessApproval', 'HoldReleaseRequest',
                'QualityInspection', 'SalesQuotation', 'ReprintRequest', 'ItemTreatmentCorrection', 'CustomerMerge',
                'Batch', 'SerialNumber', 'ShippingPackage', 'GatePass', 'Wave', 'WarehouseTask', 'LoadingTask');

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format($f$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT id, doctype, data, status, created_by
        FROM tenant_default.documents
       WHERE doctype = 'StatusTransitionRule' AND created_by = 'system'
         AND data->>'entity' IN ('BackdatedPostingRequest', 'PeriodReopenRequest', 'GRNExcessApproval', 'HoldReleaseRequest',
                                 'QualityInspection', 'SalesQuotation', 'ReprintRequest', 'ItemTreatmentCorrection',
                                 'CustomerMerge', 'RecurringSalesContract', 'ServiceContract')
      ON CONFLICT (id) DO NOTHING
    $f$, schema_rec.schema_name);
    EXECUTE format($f$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE
       WHERE name IN ('EmployeeLoan', 'RecurringSalesContract', 'POSHeldBill', 'ServiceContract', 'POSPriceOverride',
                      'BackdatedPostingRequest', 'PeriodReopenRequest', 'GRNExcessApproval', 'HoldReleaseRequest',
                      'QualityInspection', 'SalesQuotation', 'ReprintRequest', 'ItemTreatmentCorrection', 'CustomerMerge',
                      'Batch', 'SerialNumber', 'ShippingPackage', 'GatePass', 'Wave', 'WarehouseTask', 'LoadingTask')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
