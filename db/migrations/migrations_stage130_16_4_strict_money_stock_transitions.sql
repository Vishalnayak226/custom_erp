-- Stage 49.5.3's second migration, named to run after Stage 130.16.4: the
-- runner orders by stage number, and SubcontractOrder's statuses (Challan
-- Pending, Partly Received) are set by 130_16_4_job_work, which sorts before
-- this file ("strict" > "job" at the same number). Keep it after the last
-- migration that registers or re-declares one of its doctypes' statuses.
--
-- 49.5.3 (rest): strict status transitions for the money / stock-adjacent
-- doctypes the first pass (130_16_3_strict_posting_transitions) left failing
-- open - no StatusTransitionRule, strict_status_transitions FALSE - so a
-- hand-crafted status on the generic document API is refused here too.
--
-- Each doctype's real statuses were read in its engine first:
--
--   * Approval core (Draft / Pending Approval / Approved / Rejected, the 29.8
--     matrix, a rejection needs a reason) for LoyaltyRedemptionRequest,
--     PriceListVersion, PostingProfile, Budget and PrepaidExpenseSchedule.
--     Redeemed (loyalty_redemption_security.go), Superseded (approving a newer
--     version, 135.12) and Completed (the amortisation worker,
--     deferred_prepaid.go) are engine-only.
--   * PostingProfile's Inactive: a profile is retired from Draft, Approved or
--     Rejected and re-opened to Draft - never straight back to Approved.
--   * CapturedCharge: Captured -> Cancelled (a charge captured in error,
--     before billing); Billed is set by invoice generation only.
--   * Voucher: Active, Inactive and Expired move between themselves (Add or
--     edit vouchers); Exhausted is set by redemption only, so a used-up
--     single-use voucher cannot be saved back to Active.
--   * SubcontractOrder: Draft -> Cancelled; Sent, Challan Pending, Partly
--     Received and Received are set by Send / Receive (job_work.go) only.
--   * No generic move at all - every status after the first is the engine's:
--     PhysicalInventory (Counting -> Reconciling / Closed / Cancelled by its
--     own routes), BundleAssembly (written Completed), ExchangeCredit (Open ->
--     Used at checkout), DeferredRevenueSchedule (Active -> Completed by the
--     worker), OMSBill (Issued -> Billed; the generic API refuses it anyway)
--     and LogisticsBooking (courier, manifest, handover and tracking).
--
-- Additive: rules are inserted ON CONFLICT DO NOTHING and the strict flag is
-- only ever set TRUE. A document already in a status its doctype does not
-- declare keeps the 29.8 escape hatch. engines/status_transition_stage4953b_test.go
-- proves every declared status of each doctype is covered.

-- 1. The approval core.
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
  ('LoyaltyRedemptionRequest'), ('PriceListVersion'), ('PostingProfile'), ('Budget'), ('PrepaidExpenseSchedule')
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

-- 2. Each doctype's own moves.
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
  ('PostingProfile',   'Draft',     'Inactive',  'No'),
  ('PostingProfile',   'Approved',  'Inactive',  'No'),
  ('PostingProfile',   'Rejected',  'Inactive',  'No'),
  ('PostingProfile',   'Inactive',  'Draft',     'No'),
  ('CapturedCharge',   'Captured',  'Cancelled', 'No'),
  ('Voucher',          'Active',    'Inactive',  'No'),
  ('Voucher',          'Inactive',  'Active',    'No'),
  ('Voucher',          'Active',    'Expired',   'No'),
  ('Voucher',          'Inactive',  'Expired',   'No'),
  ('Voucher',          'Expired',   'Active',    'No'),
  ('Voucher',          'Expired',   'Inactive',  'No'),
  ('SubcontractOrder', 'Draft',     'Cancelled', 'No')
) AS v(entity, from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;

-- 3. Strict for all of them.
UPDATE tenant_default.doctype_meta
   SET strict_status_transitions = TRUE
 WHERE name IN ('LoyaltyRedemptionRequest', 'PriceListVersion', 'PostingProfile', 'Budget', 'PrepaidExpenseSchedule',
                'CapturedCharge', 'Voucher', 'SubcontractOrder', 'PhysicalInventory', 'BundleAssembly', 'ExchangeCredit',
                'DeferredRevenueSchedule', 'OMSBill', 'LogisticsBooking');

-- 4. Every client schema: the same rules copied from tenant_default, the
-- same flags (same loop shape as 29.8 and 130_16_3).
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
         AND data->>'entity' IN ('LoyaltyRedemptionRequest', 'PriceListVersion', 'PostingProfile', 'Budget',
                                 'PrepaidExpenseSchedule', 'CapturedCharge', 'Voucher', 'SubcontractOrder')
      ON CONFLICT (id) DO NOTHING
    $f$, schema_rec.schema_name);
    EXECUTE format($f$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE
       WHERE name IN ('LoyaltyRedemptionRequest', 'PriceListVersion', 'PostingProfile', 'Budget', 'PrepaidExpenseSchedule',
                      'CapturedCharge', 'Voucher', 'SubcontractOrder', 'PhysicalInventory', 'BundleAssembly', 'ExchangeCredit',
                      'DeferredRevenueSchedule', 'OMSBill', 'LogisticsBooking')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
