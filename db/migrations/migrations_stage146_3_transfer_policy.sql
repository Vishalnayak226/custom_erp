-- Stage 146.3.5 / 146.3.12 / 146.3.13 - transfer purpose and fill policy, allocation run, wave.
-- Additive and re-runnable. A blank purpose reads as Replenishment and a blank policy follows the
-- store, then the purpose; none of it matters until inventory.transfer_fill_policy is turned on.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('TransferOrder', 'purpose', 'Purpose (blank = Replenishment)', 'Select', FALSE, 'Replenishment,New store,Launch allocation', 6),
('TransferOrder', 'fill_policy', 'Fill Policy (blank = store / purpose)', 'Select', FALSE, 'Exact,Ship what is there,Complete', 7),
('TransferOrder', 'min_fill_pct', 'Minimum Fill %', 'Number', FALSE, NULL, 8),
('TransferOrder', 'allocation_run_id', 'Allocation Run', 'Data', FALSE, NULL, 9),
('TransferOrder', 'wave_id', 'Wave', 'Data', FALSE, NULL, 10),
('Location', 'transfer_fill_policy', 'Transfer Fill Policy (blank = by purpose)', 'Select', FALSE, 'Exact,Ship what is there,Complete', 44),
('Location', 'transfer_min_fill_pct', 'Transfer Minimum Fill %', 'Number', FALSE, NULL, 45)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
