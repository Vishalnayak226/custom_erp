-- 49.5.3 (sixth batch): strict status transitions for 10 more money / stock
-- doctypes that failed open. Same shape as 146_100_strict_transitions_49_5_3e;
-- named to sort after every other stage 146 / 148 migration. Additive: rules
-- ON CONFLICT DO NOTHING, the flag only ever set TRUE, metadata rows only (no
-- client document is touched).
--
--   * Approval core (Draft / Pending Approval / Approved / Rejected, rejection
--     needs a reason): CreditHoldRelease, GLAccountChange, VendorChange.
--   * Customer: Active <-> Inactive by hand; Merged is the merge's.
--   * VendorAdvance, TradeChargeEntry: every status after the first is the
--     engine's (they are also made on their own screens only).
--   * MaterialReturn, POSCashMovement, ConsumptionEntry, StorageBalanceSnapshot:
--     one status, set on creation by the posting; no generic move.
--   * Left out on purpose: PasswordResetRequest and SupplierSubmission (security
--     / outside-party approval cores), LPN, SortSlot, HR, Active / Inactive
--     toggles.

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
FROM (VALUES ('CreditHoldRelease'), ('GLAccountChange'), ('VendorChange')) AS e(entity)
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
  ('Customer', 'Active',   'Inactive', 'No'),
  ('Customer', 'Inactive', 'Active',   'No')
) AS v(entity, from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;

UPDATE tenant_default.doctype_meta
   SET strict_status_transitions = TRUE
 WHERE name IN ('CreditHoldRelease', 'GLAccountChange', 'VendorChange', 'Customer', 'VendorAdvance', 'TradeChargeEntry',
                'MaterialReturn', 'POSCashMovement', 'ConsumptionEntry', 'StorageBalanceSnapshot');

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
         AND data->>'entity' IN ('CreditHoldRelease', 'GLAccountChange', 'VendorChange', 'Customer')
      ON CONFLICT (id) DO NOTHING
    $f$, schema_rec.schema_name);
    EXECUTE format($f$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE
       WHERE name IN ('CreditHoldRelease', 'GLAccountChange', 'VendorChange', 'Customer', 'VendorAdvance', 'TradeChargeEntry',
                'MaterialReturn', 'POSCashMovement', 'ConsumptionEntry', 'StorageBalanceSnapshot')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
