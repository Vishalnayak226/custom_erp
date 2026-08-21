-- ---------------------------------------------------------------------------
-- Stage 42.2.5 - Zone master, promoting Bin.zone out of unstructured free
-- text into a real, governed catalogue (temperature class, hazmat
-- allowance, putaway/pick sequence - inputs 42.2.7's directed putaway and
-- 42.2.8's allocation strategy both need).
--
-- Deviates from the plan's literal "Bin.zone becomes a Link" - deliberately,
-- for the same reason Batch.item/SerialNumber.item/UOMConversion.item/
-- WarehouseTask.item/from_bin/to_bin are all Data, not Link, already in this
-- Stage: a generic Link field's value must be the target's documents.id, but
-- what a clerk types (and every existing Bin document already holds) is the
-- human zone code ("A", "COLD-1"), not a generated id. A real Link would
-- force this migration to rewrite data->>'zone' on every existing Bin
-- document from that code to a new "ZONE-..." id - a destructive rewrite of
-- live tenant data for a purely cosmetic type change, and every consumer
-- that displays data->>'zone' today (pick lists, slotting suggestions)
-- would start showing an opaque id instead of the code a warehouse actually
-- uses. This resolves open decision 6 (42.D6) the same way it was resolved
-- for the other four: Bin.zone stays Data, validated against Zone.code by
-- validateBinMasterRules (engines/master_data_validation.go) exactly the way
-- validateBatchMasterRules already validates Batch.item against
-- Item.data->>'code'. No existing Bin document is touched by this migration
-- at all - the auto-created Zone row per distinct value is purely additive,
-- and a Bin whose zone doesn't (yet) match any Zone.code is not rejected
-- retroactively, only going forward on the next save.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('Zone', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Zone', 'code', 'Zone Code', 'Data', TRUE, NULL, 1),
('Zone', 'location', 'Location (optional)', 'Link', FALSE, 'Location', 2),
('Zone', 'zone_type', 'Zone Type', 'Select', FALSE, 'Storage,Staging,Dock,PickFace,Reserve,QC,Other', 3),
('Zone', 'temperature_class', 'Temperature Class', 'Select', FALSE, 'Ambient,Chilled,Frozen,Controlled', 4),
('Zone', 'hazmat_allowed', 'Hazmat Allowed', 'Select', FALSE, 'Yes,No', 5),
('Zone', 'putaway_sequence', 'Putaway Sequence (lower = preferred first)', 'Number', FALSE, NULL, 6),
('Zone', 'pick_sequence', 'Pick Sequence (lower = walked first)', 'Number', FALSE, NULL, 7),
('Zone', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'Zone', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'Zone', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'Zone', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Auto-create one Active Zone per distinct existing Bin.zone value, blank/
-- null excluded - "keep existing free-text values working" from the plan,
-- satisfied without moving a single Bin document. hazmat_allowed defaults to
-- 'Yes' (unrestricted) rather than 'No' deliberately: an auto-created Zone
-- must not silently start blocking hazmat putaway into a zone that has
-- always accepted it, since nothing about the pre-existing free text ever
-- expressed a restriction.
INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
SELECT
  'ZONE-' || md5(z.zone_value) ,
  'Zone',
  jsonb_build_object(
    'code', z.zone_value,
    'hazmat_allowed', 'Yes',
    'status', 'Active'
  ),
  'Active',
  'system'
FROM (
  SELECT DISTINCT data->>'zone' AS zone_value
  FROM tenant_default.documents
  WHERE doctype = 'Bin' AND COALESCE(data->>'zone', '') != ''
) z
WHERE NOT EXISTS (
  SELECT 1 FROM tenant_default.documents d2
  WHERE d2.doctype = 'Zone' AND d2.data->>'code' = z.zone_value
);
