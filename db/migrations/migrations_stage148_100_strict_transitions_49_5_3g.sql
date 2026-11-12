-- 49.5.3 (seventh batch): strict status transitions for 5 more doctypes that
-- failed open (HR and party set-off). Same shape as 148_99_strict_transitions_49_5_3f;
-- sorts after it. Additive: rules ON CONFLICT DO NOTHING, the flag only ever set
-- TRUE, metadata rows only (no client document is touched).
--
--   * AppraisalCycle: Draft -> Active -> Closed, forward only.
--   * ShiftAssignment: Assigned -> Cancelled.
--   * TrainingRecord: Enrolled -> Completed or Failed.
--   * PartyPair: Active <-> Ended by hand.
--   * OnboardingChecklist: no generic move; the status follows its steps and
--     is written by the onboarding engine.
--   * Left out on purpose: SetOffVoucher, StewardshipChange, ImportBatch (approval
--     engines with their own writers, need a per-engine review), PIMTask and
--     PIMWorkflowRun (own state machines), MaintenanceSchedule, Active / Inactive toggles.

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
  ('AppraisalCycle',  'Draft',    'Active',    'No'),
  ('AppraisalCycle',  'Active',   'Closed',    'No'),
  ('ShiftAssignment', 'Assigned', 'Cancelled', 'No'),
  ('TrainingRecord',  'Enrolled', 'Completed', 'No'),
  ('TrainingRecord',  'Enrolled', 'Failed',    'No'),
  ('PartyPair',       'Active',   'Ended',     'No'),
  ('PartyPair',       'Ended',    'Active',    'No')
) AS v(entity, from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;

UPDATE tenant_default.doctype_meta
   SET strict_status_transitions = TRUE
 WHERE name IN ('AppraisalCycle', 'ShiftAssignment', 'TrainingRecord', 'PartyPair', 'OnboardingChecklist');

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
         AND data->>'entity' IN ('AppraisalCycle', 'ShiftAssignment', 'TrainingRecord', 'PartyPair')
      ON CONFLICT (id) DO NOTHING
    $f$, schema_rec.schema_name);
    EXECUTE format($f$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE
       WHERE name IN ('AppraisalCycle', 'ShiftAssignment', 'TrainingRecord', 'PartyPair', 'OnboardingChecklist')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
