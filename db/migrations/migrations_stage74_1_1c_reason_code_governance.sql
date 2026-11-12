-- ---------------------------------------------------------------------------
-- Stage 74.1.1 (BRD 4.9, FR-RSN-01..05, DEC-18), last part of the governed
-- reason-code catalogue. All additive; existing codes, categories and every
-- stored reason stay as they are (DEC-21).
--   - contexts: other categories a code may also be used in (blank = its own
--     category only);
--   - severity (Low, Medium, High) and note_required (a typed note must go
--     with the code);
--   - a starter set for the categories that had none: hold, allocation
--     exception, settlement, quality, WMS exception, transfer discrepancy,
--     cash movement, loyalty adjustment and redemption. They are created by
--     'system' so a clean-slate reset keeps them;
--   - severity on the starter codes that carry a loss.
-- Adds configuration rows in every client (Deploy asks YES).
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
      INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('ReasonCode', 'contexts', 'Also used in', 'Text', FALSE, NULL, 25),
        ('ReasonCode', 'severity', 'Severity', 'Select', FALSE, 'Low,Medium,High', 26),
        ('ReasonCode', 'note_required', 'Needs a note', 'Select', FALSE, 'No,Yes', 27)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');
    EXECUTE format($sql$
      INSERT INTO %1$I.documents (id, doctype, data, status, created_by)
      SELECT v.id, 'ReasonCode', jsonb_build_object('code', v.id, 'description', v.description, 'category', v.category,
             'status', 'Active', 'loss_class', v.loss_class, 'evidence_required', v.evidence, 'severity', v.severity,
             'note_required', 'No', 'when_to_use', v.when_to_use), 'Active', 'system'
      FROM (VALUES
        ('HOLD-CREDIT', 'Credit check', 'Hold', 'None', 'No', 'Low', 'Held until the customer account is checked.'),
        ('HOLD-STOCK', 'Stock check', 'Hold', 'None', 'No', 'Low', 'Held until the stock is counted.'),
        ('HOLD-CUSTOMER', 'Customer asked', 'Hold', 'None', 'No', 'Low', 'The customer asked us to wait.'),
        ('ALLOC-SHORT', 'No stock to allocate', 'Allocation Exception', 'None', 'No', 'Low', 'Stock is not there for the order.'),
        ('ALLOC-DAMAGED', 'Stock damaged', 'Allocation Exception', 'Damage', 'No', 'Medium', 'The piece to allocate is damaged.'),
        ('SET-DIFF', 'Amount differs', 'Settlement', 'None', 'No', 'Medium', 'The payout is not the order amount.'),
        ('SET-FEE', 'Fee not expected', 'Settlement', 'None', 'No', 'Low', 'A charge was taken that was not agreed.'),
        ('SET-DUP', 'Duplicate payment', 'Settlement', 'None', 'No', 'Medium', 'Paid twice for one order.'),
        ('QC-DEFECT', 'Defect found', 'Quality', 'Damage', 'Yes', 'Medium', 'A fault found at the quality check; attach a photo.'),
        ('QC-MISMATCH', 'Not as described', 'Quality', 'None', 'No', 'Low', 'Colour, size or print differs from the listing.'),
        ('WMS-BIN-EMPTY', 'Bin empty', 'WMS Exception', 'None', 'No', 'Low', 'Nothing in the bin the task named.'),
        ('WMS-MISSING', 'Piece missing', 'WMS Exception', 'Shrinkage', 'No', 'Medium', 'Stock cannot be found.'),
        ('WMS-DAMAGED', 'Piece damaged', 'WMS Exception', 'Damage', 'Yes', 'Medium', 'Found damaged on the shelf; attach a photo.'),
        ('TD-DAMAGED', 'Damaged in transit', 'Transfer Discrepancy', 'Damage', 'Yes', 'Medium', 'Arrived damaged; attach a photo.'),
        ('TD-LOST', 'Lost in transit', 'Transfer Discrepancy', 'Shrinkage', 'No', 'High', 'Sent and never arrived.'),
        ('TD-EXCESS', 'More than sent', 'Transfer Discrepancy', 'Gain', 'No', 'Medium', 'More arrived than the transfer shows.'),
        ('TD-RETURN', 'Returned to source', 'Transfer Discrepancy', 'None', 'No', 'Low', 'Wrong goods sent back to the sender.'),
        ('CASH-FLOAT', 'Float top-up', 'Cash Movement', 'None', 'No', 'Low', 'Cash added to the till float.'),
        ('CASH-BANK', 'Cash sent to bank', 'Cash Movement', 'None', 'No', 'Low', 'Takings taken out for the bank.'),
        ('CASH-EXPENSE', 'Petty expense', 'Cash Movement', 'None', 'No', 'Medium', 'Paid out of the till for a small expense.'),
        ('LA-GOODWILL', 'Goodwill', 'Loyalty Adjustment', 'None', 'No', 'Medium', 'Points given to put things right.'),
        ('LA-CORRECTION', 'Correction', 'Loyalty Adjustment', 'None', 'No', 'Medium', 'Points were wrong on an earlier sale.'),
        ('LR-CUSTOMER', 'Customer request', 'Loyalty Redemption', 'None', 'No', 'Low', 'Redeemed at the customer request.')
      ) AS v(id, description, category, loss_class, evidence, severity, when_to_use)
      ON CONFLICT (id) DO NOTHING$sql$, schema_rec.schema_name);

    -- Severity on the starter codes that carry a loss (only where none was set).
    EXECUTE format($sql$
      UPDATE %1$I.documents d
         SET data = d.data || jsonb_build_object('severity', v.severity)
        FROM (VALUES ('IA-SHRINK', 'Medium'), ('IA-DAMAGE', 'Medium'), ('IA-REVALUE', 'High'), ('PR-DAMAGED', 'Medium'),
                     ('GRN-DMG-TRANSIT', 'Medium'), ('OVR-MANAGER', 'Medium')) AS v(id, severity)
       WHERE d.doctype = 'ReasonCode' AND d.id = v.id AND d.created_by = 'system'
         AND COALESCE(d.data->>'severity', '') = ''$sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
