-- Stage 26.10.1 (Reports/BI Sprint): stock ledger wiring. StockLedgerEntry/
-- WriteStockLedgerEntry (engines/inventory.go, registered since Phase 3 -
-- db/migrations_phase3.sql) existed but had zero callers - dead code. This
-- migration only extends the existing doctype_fields row set additively
-- (idempotency_key, from/to location_id, from/to status, user_id, device_id)
-- so the generic doc viewer/report drill-down can render the new columns;
-- the actual wiring into GRN/checkout/transfer/putaway/condition-change/
-- cycle-count posting is Go-side (engines/inventory.go, wms.go,
-- wms_putaway_ext.go, wms_receiving.go, transfer_orders.go, pos_checkout.go).

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('StockLedgerEntry', 'idempotency_key', 'Idempotency Key', 'Data', FALSE, NULL, 7),
('StockLedgerEntry', 'from_location_id', 'From Location/Bin', 'Data', FALSE, NULL, 8),
('StockLedgerEntry', 'to_location_id', 'To Location/Bin', 'Data', FALSE, NULL, 9),
('StockLedgerEntry', 'from_status', 'From Condition', 'Data', FALSE, NULL, 10),
('StockLedgerEntry', 'to_status', 'To Condition', 'Data', FALSE, NULL, 11),
('StockLedgerEntry', 'user_id', 'User', 'Data', FALSE, NULL, 12),
('StockLedgerEntry', 'device_id', 'Device', 'Data', FALSE, NULL, 13)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- voucher_type's original Select options (Phase 3) only covered
-- GRN/POSInvoice/StockTransfer - widen to every voucher_type this stage's
-- wiring actually writes, plus the manufacturing/generic fallback tags
-- PostInventoryLedger's back-compat wrapper uses for callers with no real
-- voucher to pass (engines/inventory.go's PostInventoryLedger comment).
UPDATE tenant_default.doctype_fields
SET options = 'GRN,POSInvoice,StockTransfer,TransferOrder,Putaway,BinReplenishment,ConditionChange,CycleCount,StockAdjustment,ProductionOrder'
WHERE doctype_name = 'StockLedgerEntry' AND fieldname = 'voucher_type' AND options = 'GRN,POSInvoice,StockTransfer';
