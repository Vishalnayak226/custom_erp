-- ---------------------------------------------------------------------------
-- Stage 73.1.7 - transfers keep batch / serial identity and handle
-- discrepancies; D04 transfer approval.
--
-- 1. TransferOrder.status gains the states the engine now writes:
--    Pending Approval (the approval engine), Partially Received (a receipt
--    that leaves units in transit), Returned (everything sent back to the
--    source), Cancelled (before dispatch).
-- 2. ReasonCode.category gains 'Transfer Discrepancy' - every damaged / lost /
--    excess receipt line and every return-to-source names one (append-only,
--    the precedent of 'Quality' and 'WMS Exception').
-- 3. D04: maker-checker by default. TransferOrder joins the approval engine
--    with one slab, 0 and up -> Store Manager, but only where the client has
--    no TransferOrder rule of its own. The per-client value below which a
--    Store Manager may approve directly is the setting
--    inventory.transfer_direct_approval_limit (default 0 = never).
-- 4. A Store Manager approves transfers, so the role can now read, create and
--    update them (no delete). ON CONFLICT DO NOTHING keeps any permission a
--    client has already set for the role.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = 'Draft,Pending Approval,Approved,Packed,Dispatched,Partially Received,Received,Returned,Cancelled'
       WHERE doctype_name = 'TransferOrder' AND fieldname = 'status'
         AND options NOT LIKE '%%Partially Received%%'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = options || ',Transfer Discrepancy'
       WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
         AND options NOT LIKE '%%Transfer Discrepancy%%'
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT 'TransferOrder', 0, NULL, 'Store Manager'
        WHERE NOT EXISTS (SELECT 1 FROM %I.approval_rules WHERE doctype = 'TransferOrder')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        VALUES ('Store Manager', 'TransferOrder', TRUE, TRUE, TRUE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
