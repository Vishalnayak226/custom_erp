-- ---------------------------------------------------------------------------
-- Stage 42.5 - Inventory control depth (42.5.1-42.5.4, 42.5.6-42.5.8).
-- 42.5.5 (multi-owner stock segregation) is deliberately NOT in this file -
-- gated on open decision 42.D2 (is 3PL a real target?), still open.
-- Additive-only, same pattern as every prior Stage 42 migration.
-- ---------------------------------------------------------------------------

-- 42.5.1: PhysicalInventory - a governed full/annual count. Handler-only
-- (engines/wms_physical_inventory.go), same "no direct generic-write for the
-- state-transition half" pattern as POSSession/CycleCountLine - allow_create/
-- allow_update stay FALSE so a count can't be started or its status hand-
-- edited around StartPhysicalInventory/ReconcilePhysicalInventory/
-- ClosePhysicalInventory.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PhysicalInventory', 'Inventory', 'inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PhysicalInventory', 'code', 'Physical Inventory', 'Data', TRUE, NULL, 1),
('PhysicalInventory', 'location', 'Location', 'Data', TRUE, NULL, 2),
('PhysicalInventory', 'zone', 'Zone (optional - blank freezes the whole location)', 'Data', FALSE, NULL, 3),
('PhysicalInventory', 'status', 'Status', 'Select', TRUE, 'Counting,Reconciling,Closed,Cancelled', 4),
('PhysicalInventory', 'line_count', 'Line Count (system-set)', 'Number', FALSE, NULL, 5),
('PhysicalInventory', 'started_at', 'Started At (system-set)', 'Data', FALSE, NULL, 6),
('PhysicalInventory', 'closed_at', 'Closed At (system-set)', 'Data', FALSE, NULL, 7),
('PhysicalInventory', 'notes', 'Notes (optional)', 'Data', FALSE, NULL, 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PhysicalInventory', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'PhysicalInventory', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 42.5.1/42.5.2: CycleCountLine gains two optional, additive fields.
-- physical_inventory links a line back to the PhysicalInventory header that
-- generated it (blank for every ordinary ABC-plan cycle-count line, exactly
-- as before this Stage). cycle_class records which CycleClass tier (42.5.2)
-- classified the SKU when the line was generated, for audit/reporting -
-- blank for a line generated before this Stage, or for a tenant that never
-- defines a CycleClass and stays on the 26.5.9 default A/B/C split.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CycleCountLine', 'physical_inventory', 'Physical Inventory (optional, set by a full count)', 'Data', FALSE, NULL, 11),
('CycleCountLine', 'cycle_class', 'Cycle Class (optional, set by classification)', 'Data', FALSE, NULL, 12)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 42.5.2: CycleClass master - configurable count-frequency tiers. A tenant
-- that never creates one keeps 26.5.9's fixed 20/30/50 A/B/C Pareto split as
-- the default (see engines.GetCycleCountPlan) - this table makes that split
-- overridable, not mandatory to configure. pareto_cutoff_pct is the
-- cumulative top-% boundary (evaluated in ascending sequence order) a SKU's
-- velocity rank must fall within to land in this class; the last class by
-- sequence catches everything above the prior cutoffs regardless of its own
-- pareto_cutoff_pct, the same "remainder is always classified" guarantee
-- GetABCCycleCountPlan already gives Tier C.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('CycleClass', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CycleClass', 'code', 'Class Code', 'Data', TRUE, NULL, 1),
('CycleClass', 'name', 'Name', 'Data', TRUE, NULL, 2),
('CycleClass', 'sequence', 'Sequence (evaluation order, lower first)', 'Number', TRUE, NULL, 3),
('CycleClass', 'pareto_cutoff_pct', 'Cumulative Velocity Cutoff % (top X% by sequence)', 'Number', TRUE, NULL, 4),
('CycleClass', 'interval_days', 'Recount Interval (days)', 'Number', TRUE, NULL, 5),
('CycleClass', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'CycleClass', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'CycleClass', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 42.5.3: BinReplenishmentRule gains an optional trigger_type - purely
-- informational/filtering (which of GetBinReplenishmentSuggestions'/
-- GetDemandDrivenReplenishmentSuggestions' scans a rule is meant for),
-- default/blank reads as 'Min/Max' so every existing rule is unaffected.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('BinReplenishmentRule', 'trigger_type', 'Trigger Type', 'Select', FALSE, 'Min/Max,Demand-Driven,Dynamic', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 42.5.6: Facility hierarchy - Location.parent (self-link) + level. Blank
-- parent/level on every existing Location (this Stage does not backfill a
-- guess at hierarchy for data that predates it) reads as an unplaced
-- top-level facility - GetFacilityRollup/GetChildLocations treat that
-- exactly like a Location with no children, not an error.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Location', 'parent', 'Parent Facility (optional)', 'Link', FALSE, 'Location', 11),
('Location', 'level', 'Facility Level (optional)', 'Select', FALSE, 'Company,Division,DC,Warehouse', 12)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
