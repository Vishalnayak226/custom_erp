-- ---------------------------------------------------------------------------
-- Stage 53.18: apply Location.sellable to EVERY provisioned tenant schema.
--
-- migrations_stage53_1_location_sellable.sql declared the field and backfilled
-- it in `tenant_default` only - but engines.ValidatePOSSellableLocation ships
-- in the BINARY, so it guards every tenant the moment the new build starts.
-- On any other provisioned schema that left:
--
--   * no `sellable` doctype_field, so the flag never rendered on the Location
--     master and an admin had no way to correct it from the UI; and
--   * no `sellable` value on any Location row, so LocationIsSellable fell
--     through to its `type = 'Store'` fallback.
--
-- Stage 17.9 seeded a Location row for every code already in use with a
-- HARDCODED type of 'Warehouse' (see migrations_stage17h_location_masters.sql).
-- So on a tenant whose shops were never re-typed by hand, that fallback
-- answers "not sellable" for every location in the chain and every till
-- refuses every sale - the precise outage 53.1 identified and then guarded
-- for one schema out of N. This migration closes it for the rest.
--
-- Same shape, and the same two halves of the rule, as 53.1:
--
--   sells today (a POSCart or POSSession exists there)  -> Yes
--   typed as a Store                                    -> Yes
--   anything else                                       -> No
--
-- so a till that was working the day before this ran keeps working.
--
-- Loops provisioned schemas in the style of
-- db/migrations_stage30_2_1_grn_location.sql. `tenant_default` is included
-- rather than excluded: both statements are idempotent (ON CONFLICT DO
-- NOTHING, and a backfill that only touches rows whose value is still unset),
-- so re-covering it changes nothing and keeps this file correct on its own.
-- Tenants provisioned AFTER this runs inherit the field by cloning
-- tenant_default at provisioning time (engines.ProvisionTenantSchema).
--
-- The statements below are dollar-quoted ($sql$) rather than single-quoted on
-- purpose: the backfill is dense in string literals, and the quadrupled-quote
-- form that a single-quoted format() string would need is unreadable and was
-- in fact written wrong the first time.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    -- Declare the field, so the flag is visible and correctable on the
    -- Location master. mandatory = FALSE for 53.1's reason: making it
    -- mandatory would reject the next save of every pre-existing Location.
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields
          (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES ('Location', 'sellable', 'Sellable (may a POS till sell from here?)', 'Select', FALSE, 'Yes,No', 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- Give every existing Location an explicit value, so the answer is real
    -- data rather than an invisible default derived in Go.
    EXECUTE format($sql$
      UPDATE %I.documents loc
         SET data = jsonb_set(loc.data, '{sellable}',
               to_jsonb(CASE
                 WHEN loc.data->>'type' = 'Store' THEN 'Yes'
                 WHEN EXISTS (
                   SELECT 1 FROM %I.documents d
                    WHERE d.doctype IN ('POSCart', 'POSSession')
                      AND d.data->>'location' = loc.id
                 ) THEN 'Yes'
                 ELSE 'No'
               END))
       WHERE loc.doctype = 'Location'
         AND (loc.data->>'sellable' IS NULL OR loc.data->>'sellable' = '')
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END $mig$;
