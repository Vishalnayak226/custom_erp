-- ---------------------------------------------------------------------------
-- Stage 146.2 - loyalty earn side (plan: recordings-gap-plan, D8 and D9).
--
--   * LoyaltyScheme (Setup -> CRM -> Loyalty Scheme): a dated scheme with the
--     stores it covers and its own spend-per-point. A sale earns under its own
--     store's scheme on the bill date; with no scheme the tenant-wide setting
--     applies, exactly as before (engines/loyalty_scheme.go).
--   * loyalty_point_ledger.location / .reason: the store and the rule behind
--     every new row (the scheme store; "Bonus: <offer>" for a bonus row).
--   * Offer: type "Bonus Points" with bonus_basis / bonus_points /
--     bonus_spend_step / stores / customer_tag (engines/loyalty_bonus.go). The
--     Select is widened only where it still has the list Gift With Purchase left.
--   * Customer: loyalty_blocked / loyalty_block_reason (+ who and when), the
--     member who neither earns nor redeems (engines/loyalty_block.go).
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'loyalty_point_ledger') THEN
      EXECUTE format('ALTER TABLE %I.loyalty_point_ledger ADD COLUMN IF NOT EXISTS location VARCHAR(100)', schema_rec.schema_name);
      EXECUTE format('ALTER TABLE %I.loyalty_point_ledger ADD COLUMN IF NOT EXISTS reason TEXT', schema_rec.schema_name);
    END IF;

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('LoyaltyScheme', 'CRM', 'crm_loyalty', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('LoyaltyScheme', 'code', 'Scheme ID', 'Data', TRUE, NULL, 1),
        ('LoyaltyScheme', 'name', 'Name', 'Data', TRUE, NULL, 2),
        ('LoyaltyScheme', 'valid_from', 'Valid from', 'Date', TRUE, NULL, 3),
        ('LoyaltyScheme', 'valid_to', 'Valid to', 'Date', FALSE, NULL, 4),
        ('LoyaltyScheme', 'stores', 'Stores (blank = all)', 'Data', FALSE, NULL, 5),
        ('LoyaltyScheme', 'rupees_per_point', 'Spend per point (₹)', 'Number', TRUE, NULL, 6),
        ('LoyaltyScheme', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7),
        ('Offer', 'bonus_basis', 'Bonus per (Bonus Points)', 'Select', FALSE, 'Spend,Unit,Bill', 42),
        ('Offer', 'bonus_points', 'Bonus points', 'Number', FALSE, NULL, 43),
        ('Offer', 'bonus_spend_step', 'Per ₹ spent (Spend)', 'Number', FALSE, NULL, 44),
        ('Offer', 'stores', 'Stores (Bonus Points; blank = all)', 'Data', FALSE, NULL, 45),
        ('Offer', 'customer_tag', 'Customer tag (blank = all)', 'Data', FALSE, NULL, 46),
        ('Customer', 'loyalty_blocked', 'Loyalty blocked', 'Select', FALSE, 'No,Yes', 72),
        ('Customer', 'loyalty_block_reason', 'Block reason', 'Data', FALSE, NULL, 73),
        ('Customer', 'loyalty_blocked_by', 'Blocked by', 'Data', FALSE, NULL, 74),
        ('Customer', 'loyalty_blocked_at', 'Blocked on', 'Data', FALSE, NULL, 75)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = 'Percentage Off,Flat Off,Buy X Get Y,Bundle Price,Gift With Purchase,Bonus Points'
       WHERE doctype_name = 'Offer' AND fieldname = 'offer_type'
         AND options = 'Percentage Off,Flat Off,Buy X Get Y,Bundle Price,Gift With Purchase'
    $sql$, schema_rec.schema_name);

    -- Whoever maintains Offers maintains the schemes.
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, 'LoyaltyScheme', allow_read, allow_create, allow_update, allow_create
        FROM %I.role_permissions WHERE doctype_name = 'Offer'
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('LoyaltyScheme', 'LS', '-', 4, 'NEVER', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
