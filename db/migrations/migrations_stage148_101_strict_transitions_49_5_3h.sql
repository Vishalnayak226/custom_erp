-- 49.5.3 (ninth batch): strict status transitions for the 3 approval record types the
-- earlier batches left out for a per-engine review: SetOffVoucher, StewardshipChange and
-- ImportBatch. Same shape as 148_100_strict_transitions_49_5_3g; sorts after the
-- migrations that create them (set-off 148_22, stewardship 47_13_4, import 47_13_1).
-- Additive: rules ON CONFLICT DO NOTHING, the flag only ever set TRUE, metadata rows
-- only (no client document is touched).
--
-- Reviewed against each engine's writers (all write the status by direct SQL, which the
-- generic-save check never sees; only a plain save is held to this table):
--   * Approval core (Draft / Pending Approval / Approved / Rejected, rejection needs a
--     reason) for all three. The generic save still refuses Approved / Rejected on these
--     approval-gated doctypes; only SubmitForApproval / DecideApproval reach them.
--   * SetOffVoucher: Reversed is written only by the voucher's Reverse action.
--   * ImportBatch: Committed, Reconciled and Expired are written only by the import
--     engine (commit, reconcile, expiry sweep).
--   * StewardshipChange: nothing beyond the approval core.

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
FROM (VALUES ('SetOffVoucher'), ('StewardshipChange'), ('ImportBatch')) AS e(entity)
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

UPDATE tenant_default.doctype_meta
   SET strict_status_transitions = TRUE
 WHERE name IN ('SetOffVoucher', 'StewardshipChange', 'ImportBatch');

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
         AND data->>'entity' IN ('SetOffVoucher', 'StewardshipChange', 'ImportBatch')
      ON CONFLICT (id) DO NOTHING
    $f$, schema_rec.schema_name);
    EXECUTE format($f$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE
       WHERE name IN ('SetOffVoucher', 'StewardshipChange', 'ImportBatch')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
