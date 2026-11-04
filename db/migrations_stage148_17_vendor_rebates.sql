-- ---------------------------------------------------------------------------
-- Stage 148.17: volume rebates from vendors (setting procurement.vendor_rebates,
-- off by default; nothing below is visible until it is on).
--
-- New asset account 1820 Vendor Rebates Receivable in every tenant's chart (as
-- 148.1 added 5195 and 148.15 added 1810), ON CONFLICT DO NOTHING. It writes one
-- new gl_accounts row per tenant, so the release counts it as a data change.
--
-- PriceListVersion (the vendor agreement) gets the rebate target fields; the
-- VendorRebateClaim doctype tracks one vendor and one period. The claim is
-- read-only to every role (the engine routes write it and post the money),
-- goes through the approval engine (Accountant) and is strict about status
-- moves (the 49.5.3 pattern): no generic status change is allowed, the engine
-- moves it. Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type)
        VALUES ('1820', 'Vendor Rebates Receivable', 'Asset')
        ON CONFLICT DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('PriceListVersion', 'rebate_basis', 'Rebate on', 'Select', FALSE, ',Gross,Net', 60),
        ('PriceListVersion', 'rebate_tier1_target', 'Tier 1 target', 'Number', FALSE, NULL, 61),
        ('PriceListVersion', 'rebate_tier1_pct', 'Tier 1 rebate %%', 'Number', FALSE, NULL, 62),
        ('PriceListVersion', 'rebate_tier2_target', 'Tier 2 target', 'Number', FALSE, NULL, 63),
        ('PriceListVersion', 'rebate_tier2_pct', 'Tier 2 rebate %%', 'Number', FALSE, NULL, 64)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type, strict_status_transitions)
      VALUES ('VendorRebateClaim', 'Procurement', 'procurement', 'Master', TRUE)
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('VendorRebateClaim', 'vendor_id', 'Vendor', 'Link', TRUE, 'Vendor', 1),
        ('VendorRebateClaim', 'period_from', 'From', 'Date', TRUE, NULL, 2),
        ('VendorRebateClaim', 'period_to', 'To', 'Date', TRUE, NULL, 3),
        ('VendorRebateClaim', 'basis', 'Rebate on', 'Data', FALSE, NULL, 4),
        ('VendorRebateClaim', 'qualifying_amount', 'Qualifying', 'Number', FALSE, NULL, 5),
        ('VendorRebateClaim', 'tier', 'Tier', 'Number', FALSE, NULL, 6),
        ('VendorRebateClaim', 'rebate_pct', 'Rebate %%', 'Number', FALSE, NULL, 7),
        ('VendorRebateClaim', 'rebate_amount', 'Rebate', 'Number', FALSE, NULL, 8),
        ('VendorRebateClaim', 'debit_note_id', 'Debit note', 'Data', FALSE, NULL, 9),
        ('VendorRebateClaim', 'status', 'Status', 'Select', TRUE, 'Calculated,Pending Approval,Approved,Rejected,Settled', 10)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE WHERE name = 'VendorRebateClaim'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'VendorRebateClaim', TRUE, FALSE, FALSE, FALSE),
        ('Accountant', 'VendorRebateClaim', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('VendorRebateClaim', 0, NULL, 'Accountant')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
