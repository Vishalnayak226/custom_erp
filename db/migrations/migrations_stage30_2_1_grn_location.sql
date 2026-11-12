-- Stage 30.2.1 (docs/UX_MANUAL_AUDIT.md): GRN's stock-posting hook reads
-- payload["location"], but `location` was never declared as a GRN field -
-- only the bespoke GRN Workbench screen happened to send it. A GRN created
-- through the generic record-list form, the API, or bulk import therefore
-- could not supply it at all: the save returned HTTP 200, the receipt counted
-- toward the PO's received quantity (closing it to further receipts via
-- PURCHA-0084), and exactly zero stock was posted. Reproduced live.
--
-- Declaring it mandatory closes that off at engines/doctype.go's
-- ValidateDocument - the one choke point every writer already runs through -
-- and makes the generic form render the field with a required marker instead
-- of omitting it entirely. engines.PrepareGRNReceipt fills it in from the
-- referenced PO's target_warehouse first, so an API/import caller that names
-- a po_id keeps working without change; only a receipt with no resolvable
-- destination at all is now rejected, which is correct - there is nowhere for
-- that stock to land.
--
-- display_order 5 slots it after received_items/status and before Stage
-- 26.5.1's asn_id (9), matching the order the Workbench screen asks for it in.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('GRN', 'location', 'Receiving Location', 'Data', TRUE, NULL, 5)
ON CONFLICT (doctype_name, fieldname) DO UPDATE SET mandatory = TRUE, label = EXCLUDED.label;

-- Backfill every already-provisioned tenant schema; new tenants inherit it
-- from tenant_default at provisioning time (same pattern as
-- db/migrations_stage18_core_module_fix.sql).
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format(
      'INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES (''GRN'', ''location'', ''Receiving Location'', ''Data'', TRUE, NULL, 5) ON CONFLICT (doctype_name, fieldname) DO UPDATE SET mandatory = TRUE, label = EXCLUDED.label',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
