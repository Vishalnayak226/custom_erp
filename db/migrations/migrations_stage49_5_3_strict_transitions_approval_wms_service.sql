-- 49.5.3 (eighth batch): strict status transitions for the 8 doctypes the earlier
-- batches left out on purpose. Approved by the owner on 2026-11-12 ("all
-- recommended"): make them strict, with the standard approval rules and a
-- reason on rejection. Same shape as 148_99_strict_transitions_49_5_3f.
-- Additive: rules ON CONFLICT DO NOTHING, the flag only ever set TRUE, metadata
-- rows only (no client document is touched).
--
--   * Approval core (Draft / Pending Approval / Approved / Rejected, rejection
--     needs a reason): PasswordResetRequest, SupplierSubmission, Appraisal,
--     Grievance. The generic save still refuses Approved / Rejected on these
--     approval-gated doctypes; only SubmitForApproval / DecideApproval reach them.
--     PasswordResetRequest's Failed is written only by the reset itself.
--   * LPN: no engine writes its status, so the moves are by hand -
--     Open <-> Closed, Closed -> Shipped. A shipped LPN never moves again.
--   * Appointment: Scheduled -> Cancelled / NoShow and CheckedIn -> Cancelled by
--     hand; CheckedIn, InProgress and Completed are the yard / receiving engines'.
--   * ServiceTicket: every move after Draft is the ticket endpoints'
--     (assign / start / resolve / close / cancel).
--   * SortSlot: every move is the sortation engine's (assign, confirm, clear);
--     Cleared is declared but nothing writes it.

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
FROM (VALUES ('PasswordResetRequest'), ('SupplierSubmission'), ('Appraisal'), ('Grievance')) AS e(entity)
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
  ('LPN',         'Open',      'Closed',    'No'),
  ('LPN',         'Closed',    'Open',      'No'),
  ('LPN',         'Closed',    'Shipped',   'No'),
  ('Appointment', 'Scheduled', 'Cancelled', 'No'),
  ('Appointment', 'Scheduled', 'NoShow',    'No'),
  ('Appointment', 'CheckedIn', 'Cancelled', 'No')
) AS v(entity, from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;

UPDATE tenant_default.doctype_meta
   SET strict_status_transitions = TRUE
 WHERE name IN ('PasswordResetRequest', 'SupplierSubmission', 'Appraisal', 'Grievance',
                'LPN', 'SortSlot', 'ServiceTicket', 'Appointment');

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
         AND data->>'entity' IN ('PasswordResetRequest', 'SupplierSubmission', 'Appraisal', 'Grievance', 'LPN', 'Appointment')
      ON CONFLICT (id) DO NOTHING
    $f$, schema_rec.schema_name);
    EXECUTE format($f$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE
       WHERE name IN ('PasswordResetRequest', 'SupplierSubmission', 'Appraisal', 'Grievance',
                      'LPN', 'SortSlot', 'ServiceTicket', 'Appointment')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
