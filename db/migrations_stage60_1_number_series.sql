-- ---------------------------------------------------------------------------
-- Stage 60.1 - number series: suffix, per-store override, per-transaction
-- lengths the user chose (2026-10-14).
--
-- Additive only:
--   * prefix_configs.suffix          - text after the running number ('' = none)
--   * prefix_configs.store_overrides - {"<store code>": {"prefix": "...", "suffix": "..."}}
--                                      a store's own prefix/suffix for this series;
--                                      blank parts fall back to the series' own.
--   * AST (fixed asset tags), Payslip and PRT (purchase return) rows, numbered
--     with exactly the engine's built-in fallback shape (same prefix, same
--     counter bucket) so only the length changes. Without a row they could
--     not be edited on the screen.
--
-- Lengths: purchase and HR series go to 5 digits, the rest stay at 6. Only a
-- row still at the shipped default of 6 is changed - a tenant that already
-- chose its own length keeps it. The running counters are not touched, so no
-- number already issued can be issued again (a shorter padding produces a
-- different string for every counter value).
-- ---------------------------------------------------------------------------

ALTER TABLE tenant_default.prefix_configs ADD COLUMN IF NOT EXISTS suffix VARCHAR(50) NOT NULL DEFAULT '';
ALTER TABLE tenant_default.prefix_configs ADD COLUMN IF NOT EXISTS store_overrides JSONB NOT NULL DEFAULT '{}'::jsonb;

INSERT INTO tenant_default.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
VALUES
  ('AST',     'AST', '/', 6, 'ANNUAL', TRUE),
  ('Payslip', 'Payslip', '/', 5, 'ANNUAL', TRUE),
  ('PRT',     'PRT', '/', 5, 'ANNUAL', TRUE)
ON CONFLICT (doc_type) DO NOTHING;

UPDATE tenant_default.prefix_configs SET padding_width = 5
 WHERE padding_width = 6
   AND doc_type IN ('PR', 'RFQ', 'QTN', 'PO', 'ASN', 'GRN', 'PRT', 'DN',
                    'LV', 'ATT', 'EXP', 'LOAN', 'GRV', 'Payslip');

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.prefix_configs', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format('ALTER TABLE %I.prefix_configs ADD COLUMN IF NOT EXISTS suffix VARCHAR(50) NOT NULL DEFAULT ''''', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE %I.prefix_configs ADD COLUMN IF NOT EXISTS store_overrides JSONB NOT NULL DEFAULT ''{}''::jsonb', schema_rec.schema_name);
    EXECUTE format($f$
      INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
      VALUES ('AST', 'AST', '/', 6, 'ANNUAL', TRUE), ('Payslip', 'Payslip', '/', 5, 'ANNUAL', TRUE), ('PRT', 'PRT', '/', 5, 'ANNUAL', TRUE)
      ON CONFLICT (doc_type) DO NOTHING
    $f$, schema_rec.schema_name);
    EXECUTE format($f$
      UPDATE %I.prefix_configs SET padding_width = 5
       WHERE padding_width = 6
         AND doc_type IN ('PR', 'RFQ', 'QTN', 'PO', 'ASN', 'GRN', 'PRT', 'DN',
                          'LV', 'ATT', 'EXP', 'LOAN', 'GRV', 'Payslip')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
