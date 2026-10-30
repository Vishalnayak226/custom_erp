-- Stage 49.5.3's migration, named to run after Stage 130.16.3: the runner
-- orders by stage number, and DeliveryChallan / ChallanWriteOff (130.16.3),
-- MaterialIssue (130.15), IRN* (75.2), LoyaltyPointAdjustment (73.2.11),
-- InventoryAdjustment (88.5) and others are registered by later-numbered
-- migrations, so under its own number a fresh database had no row to make
-- strict. Keep it after the last migration that registers one of its doctypes.
--
-- 49.5.3: strict status transitions for the money / stock / tax doctypes
-- that still failed open (no StatusTransitionRule, strict_status_transitions
-- FALSE), so a hand-crafted status on the generic document API is refused
-- the way it already is for SalesInvoice, GRN, PurchaseOrder and friends
-- (migrations_stage29_8_status_transition_map.sql).
--
-- The matrix is the one 29.8 seeded for the approval-flow doctypes: Draft,
-- Pending Approval, Approved and Rejected move between themselves; a
-- rejection needs a reason; the statuses the document's own action sets
-- (Posted, Reversed, Issued, Dispatched, Applied...) have no generic rule,
-- because only the engine that moves the stock or money may set them.
-- A document already in a status its doctype does not declare keeps the
-- 29.8 escape hatch. Additive: rules are inserted ON CONFLICT DO NOTHING,
-- and the strict flag is only ever set TRUE here.
--
-- engines/status_transition_stage4953_test.go proves every declared status
-- of each doctype is covered: a generic rule in or out, or engine-only.

-- 1. The approval core, for every approval-flow posting doctype.
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
  ('JournalVoucher'), ('InventoryAdjustment'), ('PurchaseReturn'), ('DeliveryChallan'),
  ('MaterialIssue'), ('IntercompanyTransaction'), ('ChallanWriteOff'), ('LoyaltyPointAdjustment'),
  ('IRNCancellation'), ('IRNManualImport')
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
  -- a voucher kept as a recurring template, and back to a draft
  ('JournalVoucher',   'Draft',              'Recurring Template', 'No'),
  ('JournalVoucher',   'Recurring Template', 'Draft',              'No'),
  -- a return or challan cancelled before it posts / dispatches, with a reason
  -- (Purchase Returns offers Cancel on a Draft and a Rejected return; the
  -- reason the return was raised with satisfies the reason check)
  ('PurchaseReturn',   'Draft',              'Cancelled',          'Yes'),
  ('PurchaseReturn',   'Rejected',           'Cancelled',          'Yes'),
  ('PurchaseReturn',   'Approved',           'Cancelled',          'Yes'),
  ('DeliveryChallan',  'Draft',              'Cancelled',          'Yes'),
  ('DeliveryChallan',  'Approved',           'Cancelled',          'Yes'),
  -- posted from a draft, as CreditNote / DebitNote already are (29.8)
  ('VendorCreditNote', 'Draft',              'Posted',             'No'),
  ('Payslip',          'Draft',              'Posted',             'No')
) AS v(entity, from_status, to_status, needs_reason)
ON CONFLICT (id) DO NOTHING;

-- 3. Strict for the doctypes above, plus LandedCostVoucher (Draft -> Applied
-- is its apply action only; a plain save may not claim it).
UPDATE tenant_default.doctype_meta
   SET strict_status_transitions = TRUE
 WHERE name IN ('JournalVoucher', 'InventoryAdjustment', 'PurchaseReturn', 'DeliveryChallan',
                'MaterialIssue', 'IntercompanyTransaction', 'ChallanWriteOff', 'LoyaltyPointAdjustment',
                'IRNCancellation', 'IRNManualImport', 'VendorCreditNote', 'Payslip', 'LandedCostVoucher');

-- 4. Every client schema: the same rules copied from tenant_default, the
-- same flags (same loop shape as 29.8).
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
         AND data->>'entity' IN ('JournalVoucher', 'InventoryAdjustment', 'PurchaseReturn', 'DeliveryChallan',
                                 'MaterialIssue', 'IntercompanyTransaction', 'ChallanWriteOff', 'LoyaltyPointAdjustment',
                                 'IRNCancellation', 'IRNManualImport', 'VendorCreditNote', 'Payslip')
      ON CONFLICT (id) DO NOTHING
    $f$, schema_rec.schema_name);
    EXECUTE format($f$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE
       WHERE name IN ('JournalVoucher', 'InventoryAdjustment', 'PurchaseReturn', 'DeliveryChallan',
                      'MaterialIssue', 'IntercompanyTransaction', 'ChallanWriteOff', 'LoyaltyPointAdjustment',
                      'IRNCancellation', 'IRNManualImport', 'VendorCreditNote', 'Payslip', 'LandedCostVoucher')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
