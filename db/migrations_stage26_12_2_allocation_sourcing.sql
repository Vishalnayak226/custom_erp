-- Stage 26.12.2: Allocation/Sourcing Engine - adds a `pincode` field to the
-- existing Location master (Stage 17.9) so the new "Nearest Pincode"
-- AllocationRule strategy (engines/sourcing.go's ResolveAllocationPlan) has
-- real per-location data to compare against a SalesOrder's shipping address
-- pincode - additive, optional, same shape as every other Location field.
-- No other schema change needed: AllocationRule/ReasonCode/StatusTransitionRule
-- (26.12.9) and the inventory_availability buckets (26.12.6) already exist.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Location', 'pincode', 'Pincode', 'Data', FALSE, NULL, 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
