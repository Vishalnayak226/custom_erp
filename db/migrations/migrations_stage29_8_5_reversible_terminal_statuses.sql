-- Stage 29.8.5: make Leave and VendorQuote's terminal statuses reversible.
--
-- 29.8 seeded the transition matrices and flagged two judgement calls rather
-- than guessing at them: `Leave` treated both `Approved` and `Rejected` as
-- terminal, so an approved leave could never be revoked and a rejected one
-- could never be reopened; `VendorQuote` did the same with `Selected`, so a
-- chosen vendor that then fell through left the quote frozen. The user
-- decided (2026-08-01): allow both, each requiring a reason code.
--
-- This needs no schema change and no new mechanism - it is exactly the "one
-- admin-editable StatusTransitionRule row" the 29.8.5 note predicted, which
-- is why it is seeded as ordinary rule documents here rather than coded
-- anywhere. Both doctypes keep their existing option sets:
--
--   Leave:       Applied, Approved, Rejected
--   VendorQuote: Submitted, Selected, Rejected
--
-- so "revoke" is expressed as a move back to the doctype's own open state
-- (Applied / Submitted) or across to the other decision, not as a new
-- `Cancelled` option. Adding a status option would change every existing
-- record's legal option set and the screens that render it; reusing the
-- declared set keeps this to what was actually asked for.
--
-- requires_reason_code is 'Yes' on every row: reversing a decision that has
-- already been communicated to an employee or a vendor is precisely the case
-- where the audit trail has to say why. engines/status_transition.go enforces
-- it (looking for `reason_code` or `reason` on the payload) and rejects a
-- reversal without one.

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
    'requires_reason_code', 'Yes',
    'status',               'Active'
  ),
  'Active',
  'system'
FROM (VALUES
  -- Leave: revoke an approval (employee cancels planned leave, or HR reverses
  -- it), send it back for a fresh decision, or reopen a rejection on appeal.
  ('Leave',       'Approved',  'Rejected'),
  ('Leave',       'Approved',  'Applied'),
  ('Leave',       'Rejected',  'Applied'),

  -- VendorQuote: unselect a quote when the chosen vendor falls through, or
  -- bring a rejected quote back into consideration without re-running the RFQ.
  ('VendorQuote', 'Selected',  'Submitted'),
  ('VendorQuote', 'Selected',  'Rejected'),
  ('VendorQuote', 'Rejected',  'Submitted')
) AS v(entity, from_status, to_status)
ON CONFLICT (id) DO NOTHING;

-- Backfill every already-provisioned tenant schema. New tenants inherit these
-- through ProvisionTenantSchema, which clones tenant_default's documents.
-- Copies tenant_default's rows rather than repeating the VALUES list, same as
-- migrations_stage29_8_status_transition_map.sql does, so the two can't drift.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.documents', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT id, doctype, data, status, created_by
        FROM tenant_default.documents
       WHERE doctype = 'StatusTransitionRule' AND created_by = 'system'
         AND data->>'entity' IN ('Leave', 'VendorQuote')
      ON CONFLICT (id) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
