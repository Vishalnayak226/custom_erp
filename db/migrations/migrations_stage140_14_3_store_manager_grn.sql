-- Stage 140.14.3: Purchase Return lists GRNs (GET /api/v1/doc/GRN) and a Store
-- Manager had no read on GRN, so the screen answered 403. Read only; a row an
-- administrator already set is left alone. Writes one role_permissions row per
-- schema (a metadata table, not client data). Safe to re-run.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
VALUES ('Store Manager', 'GRN', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.role_permissions', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format($f$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('Store Manager', 'GRN', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
