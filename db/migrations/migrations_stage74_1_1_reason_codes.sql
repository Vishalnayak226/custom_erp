-- ---------------------------------------------------------------------------
-- Stage 74.1.1 (BRD 4.9, FR-RSN-01..05, DEC-18): one governed reason-code
-- catalogue, offered wherever an exception is recorded.
--
-- The ReasonCode master already exists (category = the action it is used
-- for, checked by requireActiveReasonCode). Added, all additive:
--   - nine categories for the exceptions still taken as free text or new in
--     Stage 74: goods receipt rejection / damage, purchase return, sales
--     return, price override, reprint, inventory adjustment, material issue,
--     delivery challan;
--   - effective_from / effective_to (a code outside its dates is refused),
--     evidence_required, loss_class (a hint for the posting profile, 74.1.3)
--     and when_to_use;
--   - a starter set of codes per new category, created by 'system' so a
--     clean-slate reset (Stage 91) keeps them.
-- Existing codes, categories and every stored free-text reason stay as they
-- are (DEC-21).
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

    -- New categories appended to the Select options (nothing removed).
    EXECUTE format($sql$
      UPDATE %1$I.doctype_fields
         SET options = options || ',' || $c$Goods Receipt Rejection,Goods Receipt Damage,Purchase Return,Sales Return,Price Override,Reprint,Inventory Adjustment,Material Issue,Delivery Challan$c$
       WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
         AND position('Goods Receipt Rejection' in COALESCE(options, '')) = 0$sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('ReasonCode', 'effective_from', 'Use from', 'Date', FALSE, NULL, 20),
        ('ReasonCode', 'effective_to', 'Use until', 'Date', FALSE, NULL, 21),
        ('ReasonCode', 'evidence_required', 'Needs a file attached', 'Select', FALSE, 'No,Yes', 22),
        ('ReasonCode', 'loss_class', 'Kind of loss or gain', 'Select', FALSE, 'None,Shrinkage,Damage,Gain,Write-off,Revaluation,Expiry', 23),
        ('ReasonCode', 'when_to_use', 'When to use it', 'Text', FALSE, NULL, 24)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');
    EXECUTE format($sql$
      INSERT INTO %1$I.documents (id, doctype, data, status, created_by)
      SELECT v.id, 'ReasonCode', jsonb_build_object('code', v.id, 'description', v.description, 'category', v.category,
             'status', 'Active', 'loss_class', v.loss_class, 'evidence_required', v.evidence, 'when_to_use', v.when_to_use), 'Active', 'system'
      FROM (VALUES
        ('GRN-REJ-QUALITY', 'Quality not as ordered', 'Goods Receipt Rejection', 'None', 'No', 'The goods arrived but fail the quality check.'),
        ('GRN-REJ-WRONG', 'Wrong item, size or colour', 'Goods Receipt Rejection', 'None', 'No', 'Not what the PO ordered.'),
        ('GRN-REJ-EXCESS', 'More than ordered - returned', 'Goods Receipt Rejection', 'None', 'No', 'Goods beyond the PO sent back.'),
        ('GRN-DMG-TRANSIT', 'Damaged in transit', 'Goods Receipt Damage', 'Damage', 'Yes', 'Broken or soiled on arrival; attach a photo.'),
        ('GRN-DMG-PACK', 'Packaging damaged', 'Goods Receipt Damage', 'Damage', 'No', 'The goods are fine but cannot be sold in this packing.'),
        ('PR-QUALITY', 'Quality not as ordered', 'Purchase Return', 'None', 'No', 'Sent back after a quality check.'),
        ('PR-WRONG', 'Wrong item, size or colour', 'Purchase Return', 'None', 'No', 'Not what was ordered.'),
        ('PR-DAMAGED', 'Damaged goods', 'Purchase Return', 'Damage', 'Yes', 'Damaged goods going back to the vendor; attach a photo.'),
        ('PR-EXCESS', 'Excess quantity', 'Purchase Return', 'None', 'No', 'More than was ordered.'),
        ('SR-FIT', 'Size or fit', 'Sales Return', 'None', 'No', 'The customer needs another size.'),
        ('SR-DEFECT', 'Defective', 'Sales Return', 'Damage', 'No', 'A fault found after the sale.'),
        ('SR-CHANGED', 'Changed mind', 'Sales Return', 'None', 'No', 'Returned within the policy without a fault.'),
        ('SR-WRONG', 'Wrong item given', 'Sales Return', 'None', 'No', 'The store handed over the wrong piece.'),
        ('OVR-MATCH', 'Price match', 'Price Override', 'None', 'No', 'Matching a lower advertised price.'),
        ('OVR-SOILED', 'Shop-soiled piece', 'Price Override', 'None', 'No', 'Display or slightly marked piece.'),
        ('OVR-MANAGER', 'Manager discretion', 'Price Override', 'None', 'No', 'A one-off decision by the manager.'),
        ('RPT-LOST', 'Original lost', 'Reprint', 'None', 'No', 'The printed copy was lost.'),
        ('RPT-DAMAGED', 'Print damaged', 'Reprint', 'None', 'No', 'The printed copy is torn or unreadable.'),
        ('RPT-COPY', 'Customer asked for a copy', 'Reprint', 'None', 'No', 'A duplicate for the customer or vendor.'),
        ('IA-FOUND', 'Stock found', 'Inventory Adjustment', 'Gain', 'No', 'Stock on the shelf that the books did not show.'),
        ('IA-SHRINK', 'Shrinkage', 'Inventory Adjustment', 'Shrinkage', 'No', 'Stock missing with no known cause.'),
        ('IA-DAMAGE', 'Damaged - written off', 'Inventory Adjustment', 'Damage', 'Yes', 'Damaged beyond sale; attach a photo.'),
        ('IA-EXPIRED', 'Expired', 'Inventory Adjustment', 'Expiry', 'No', 'Past its expiry date.'),
        ('IA-REVALUE', 'Cost corrected', 'Inventory Adjustment', 'Revaluation', 'Yes', 'Value only; attach the evidence of the right cost.'),
        ('MI-CONSUMABLE', 'Shop consumables', 'Material Issue', 'None', 'No', 'Used up in the shop.'),
        ('MI-SAMPLE', 'Sample', 'Material Issue', 'None', 'No', 'Given as a sample.'),
        ('DC-JOBWORK', 'Job work', 'Delivery Challan', 'None', 'No', 'Sent to a job worker, comes back processed.'),
        ('DC-SAMPLE', 'Sample', 'Delivery Challan', 'None', 'No', 'Sent as a sample.'),
        ('DC-REPAIR', 'Repair', 'Delivery Challan', 'None', 'No', 'Sent for repair, comes back.'),
        ('DC-EXHIBITION', 'Exhibition', 'Delivery Challan', 'None', 'No', 'Sent to an exhibition, comes back.')
      ) AS v(id, description, category, loss_class, evidence, when_to_use)
      ON CONFLICT (id) DO NOTHING$sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
