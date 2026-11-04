-- ---------------------------------------------------------------------------
-- Stage 148.38 / 148.39 (builder r): goods received before the PO, and PO
-- approval rules beyond amount.
--
-- 148.38: a reason-code category "Received Before PO" (with three starter
-- codes, new rows only), the GRN fields that mark a receipt taken before its
-- PO, and PurchaseOrder.retro_grn_id that ties the retro PO to that receipt.
-- 148.39: PORoutingRule (amount band + vendor group / item category -> role,
-- with an optional escalation) and ApprovalDelegate (who covers an approver,
-- for a date range).
--
-- Metadata and new seed rows only, replayed into every tenant schema. No
-- existing document, posting or master row is changed: with the setting off
-- and no rule on file every receipt and approval behaves as it always has.
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
      UPDATE %1$I.doctype_fields
         SET options = options || ',Received Before PO'
       WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
         AND position('Received Before PO' in COALESCE(options, '')) = 0$sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES
        ('PORoutingRule', 'Procurement', 'procurement', 'Master'),
        ('ApprovalDelegate', 'Procurement', 'procurement', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('PORoutingRule', 'rule_name', 'Name', 'Data', TRUE, NULL, 1),
        ('PORoutingRule', 'min_amount', 'From amount', 'Number', FALSE, NULL, 2),
        ('PORoutingRule', 'max_amount', 'Up to amount', 'Number', FALSE, NULL, 3),
        ('PORoutingRule', 'vendor_group', 'Vendor group', 'Data', FALSE, NULL, 4),
        ('PORoutingRule', 'item_category', 'Item category', 'Data', FALSE, NULL, 5),
        ('PORoutingRule', 'required_role', 'Approver role', 'Data', TRUE, NULL, 6),
        ('PORoutingRule', 'escalate_after_days', 'Escalate after days', 'Number', FALSE, NULL, 7),
        ('PORoutingRule', 'escalate_role', 'Escalate to role', 'Data', FALSE, NULL, 8),
        ('PORoutingRule', 'status', 'Status', 'Select', FALSE, 'Active,Inactive', 9),

        ('ApprovalDelegate', 'approver_user', 'Approver', 'Data', TRUE, NULL, 1),
        ('ApprovalDelegate', 'delegate_user', 'Covered by', 'Data', TRUE, NULL, 2),
        ('ApprovalDelegate', 'from_date', 'From', 'Date', TRUE, NULL, 3),
        ('ApprovalDelegate', 'to_date', 'To', 'Date', TRUE, NULL, 4),
        ('ApprovalDelegate', 'status', 'Status', 'Select', FALSE, 'Active,Inactive', 5),

        ('GRN', 'received_before_po', 'Received before PO', 'Select', FALSE, ',Yes', 120),
        ('GRN', 'before_po_reason', 'Reason code', 'Data', FALSE, NULL, 121),
        ('GRN', 'before_po_due', 'Retro PO due by', 'Date', FALSE, NULL, 122),
        ('GRN', 'retro_po_id', 'Retro PO', 'Data', FALSE, NULL, 123),
        ('PurchaseOrder', 'retro_grn_id', 'Received before PO: receipt', 'Link', FALSE, 'GRN', 120),
        ('PurchaseOrder', 'received_before_po', 'Received before PO', 'Select', FALSE, ',Yes', 121)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('Super Admin', 'PORoutingRule', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'PORoutingRule', TRUE, TRUE, TRUE, FALSE),
        ('Accountant', 'PORoutingRule', TRUE, FALSE, FALSE, FALSE),
        ('Super Admin', 'ApprovalDelegate', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'ApprovalDelegate', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');
    EXECUTE format($sql$
      INSERT INTO %1$I.documents (id, doctype, data, status, created_by)
      SELECT v.id, 'ReasonCode', jsonb_build_object('code', v.id, 'description', v.description, 'category', 'Received Before PO',
             'status', 'Active', 'loss_class', 'None', 'evidence_required', 'No', 'when_to_use', v.when_to_use), 'Active', 'system'
      FROM (VALUES
        ('GBP-URGENT', 'Urgent - PO to follow', 'The goods were needed at once; the PO is raised after.'),
        ('GBP-VENDOR', 'Vendor delivered ahead of the PO', 'The vendor sent the goods before the PO was ready.'),
        ('GBP-EMERGENCY', 'Emergency purchase', 'Bought in an emergency; the PO is raised after.')
      ) AS v(id, description, when_to_use)
      ON CONFLICT (id) DO NOTHING$sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
