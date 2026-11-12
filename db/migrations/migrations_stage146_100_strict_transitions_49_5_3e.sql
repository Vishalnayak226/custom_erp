-- 49.5.3 (fifth batch): strict status transitions for 9 more stock / cost
-- adjacent doctypes that failed open. Same shape as 146_99_strict_transitions_49_5_3d;
-- additive: rules ON CONFLICT DO NOTHING, the flag only ever set TRUE, metadata
-- rows only (no client document is touched).
--
--   * GateEntry: Open -> Cancelled by hand; GRN Made is the goods receipt's.
--   * POSCostingGap, ChannelSKUException: Open -> Resolved.
--   * POSOfflineQueueGap, POSOfflineSyncVariance: Open -> Reviewed.
--   * FloorAssistRequest: Open -> Acknowledged -> Resolved (or straight to Resolved).
--   * ItemException: Open / Logged -> Resolved (Logged is only ever created).
--   * StoreLabelRequest: Requested -> Cancelled; Printed is the print run's.
--   * Hold: Released is HoldReleaseRequest's (ReleaseHold), no generic move.

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
  ('GateEntry', 'Open', 'Cancelled', 'No'),
  ('POSCostingGap', 'Open', 'Resolved', 'No'),
  ('POSOfflineQueueGap', 'Open', 'Reviewed', 'No'),
  ('POSOfflineSyncVariance', 'Open', 'Reviewed', 'No'),
  ('FloorAssistRequest', 'Open', 'Acknowledged', 'No'),
  ('FloorAssistRequest', 'Open', 'Resolved', 'No'),
  ('FloorAssistRequest', 'Acknowledged', 'Resolved', 'No'),
  ('ItemException', 'Open', 'Resolved', 'No'),
  ('ItemException', 'Logged', 'Resolved', 'No'),
  ('ChannelSKUException', 'Open', 'Resolved', 'No'),
  ('StoreLabelRequest', 'Requested', 'Cancelled', 'No')
) AS v(entity, from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;

UPDATE tenant_default.doctype_meta
   SET strict_status_transitions = TRUE
 WHERE name IN ('GateEntry', 'POSCostingGap', 'POSOfflineQueueGap', 'POSOfflineSyncVariance', 'FloorAssistRequest', 'ItemException',
                'ChannelSKUException', 'StoreLabelRequest', 'Hold');

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
         AND data->>'entity' IN ('GateEntry', 'POSCostingGap', 'POSOfflineQueueGap', 'POSOfflineSyncVariance', 'FloorAssistRequest', 'ItemException', 'ChannelSKUException', 'StoreLabelRequest')
      ON CONFLICT (id) DO NOTHING
    $f$, schema_rec.schema_name);
    EXECUTE format($f$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE
       WHERE name IN ('GateEntry', 'POSCostingGap', 'POSOfflineQueueGap', 'POSOfflineSyncVariance', 'FloorAssistRequest', 'ItemException',
                'ChannelSKUException', 'StoreLabelRequest', 'Hold')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
