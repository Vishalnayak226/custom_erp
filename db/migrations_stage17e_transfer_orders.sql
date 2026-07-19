-- Stage 17.6: Transfer-order lifecycle (Draft -> Approved -> Dispatched -> Received).
ALTER TABLE tenant_default.inventory_availability ADD COLUMN IF NOT EXISTS in_transit INT NOT NULL DEFAULT 0;

-- TransferOrder (db/migration.sql) never declared an items field - additive,
-- same "JSON-encoded string" convention as PurchaseOrder's "items" /
-- GRN's "received_items".
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
VALUES ('TransferOrder', 'items', 'Transfer Items JSON', 'Data', TRUE, NULL, 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
