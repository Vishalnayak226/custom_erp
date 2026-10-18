-- ---------------------------------------------------------------------------
-- Stage 73.2.18 - the planner approves supply proposals.
--
-- SupplyProposal keeps a reorder or MRP suggestion as a record the planner
-- decides on: Proposed -> Converted (into a Draft PurchaseOrder, TransferOrder
-- or ProductionOrder, named in target_doctype / target_id) or Rejected (with a
-- reason). Written only by engines/supply_proposals.go through
-- /api/v1/planning/proposals/*, so no role creates one by hand. The documents
-- a proposal became carry source_proposal_ids back.
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

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('SupplyProposal', 'Inventory', 'inventory', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('SupplyProposal', 'source', 'Source', 'Select', TRUE, 'Reorder,MRP,MRP component', 1),
        ('SupplyProposal', 'sku', 'Item', 'Data', TRUE, NULL, 2),
        ('SupplyProposal', 'location', 'Location', 'Data', TRUE, NULL, 3),
        ('SupplyProposal', 'suggested_qty', 'Suggested quantity', 'Number', TRUE, NULL, 4),
        ('SupplyProposal', 'approved_qty', 'Approved quantity', 'Number', FALSE, NULL, 5),
        ('SupplyProposal', 'action', 'Supply by', 'Select', FALSE, 'Purchase,Transfer,Production', 6),
        ('SupplyProposal', 'target_doctype', 'Became', 'Data', FALSE, NULL, 7),
        ('SupplyProposal', 'target_id', 'Document', 'Data', FALSE, NULL, 8),
        ('SupplyProposal', 'reason', 'Note / reason', 'Data', FALSE, NULL, 9),
        ('SupplyProposal', 'status', 'Status', 'Select', TRUE, 'Proposed,Converted,Rejected', 10)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT v.d, 'source_proposal_ids', 'From supply proposal(s)', 'Data', FALSE, NULL, 95
        FROM (VALUES ('PurchaseOrder'), ('TransferOrder'), ('ProductionOrder')) AS v(d)
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = v.d)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        VALUES ('HR/Admin', 'SupplyProposal', TRUE, FALSE, TRUE, FALSE),
               ('Store Manager', 'SupplyProposal', TRUE, FALSE, TRUE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
