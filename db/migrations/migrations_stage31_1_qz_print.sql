-- Stage 31.1: QZ Tray silent printing.
--
-- Context: every print path in this app was window.print() into a hidden
-- @media print area (public/app.js printPOSReceipt / renderPrintSheet). That
-- pops the browser print dialog every time and cannot choose a printer, so a
-- packing bench with a thermal label printer AND an A4 invoice printer has to
-- pick the right one by hand on every single order. QZ Tray is the local
-- WebSocket bridge that lets a browser address a named OS printer directly.
--
-- No new table for the printer mapping: the Printer Master already exists
-- (db/migration.sql, module_key 'stickers') with code/name/location/status,
-- and engines/stickers.go already validates against it (DEVICE-0298). This
-- extends that same doctype rather than introducing a parallel registry.
--
--   qz_printer_name  - the exact OS printer name as QZ reports it. Distinct
--                      from `name`, which is the human label used in the UI;
--                      the OS string is often something like
--                      "ZDesigner ZD220-203dpi ZPL" and must match verbatim.
--   print_role       - which document class this printer is the default for.
--                      Drives one-click printing: the UI resolves role ->
--                      printer without asking the operator.
--   printer_language - how payloads must be encoded for it. Thermal units
--                      taking raw command streams (ZPL/TSPL/ESC-POS) get a
--                      raw byte payload; everything else gets PDF or HTML.
--   label_width_mm / label_height_mm / dpi - needed to rasterise a PDF label
--                      to the right physical size on a 4x6 thermal roll.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Printer', 'qz_printer_name', 'OS Printer Name (exact, from QZ)', 'Data', FALSE, NULL, 5),
('Printer', 'print_role', 'Default For', 'Select', FALSE, 'Shipping Label,Invoice,Sticker,Receipt,General', 6),
('Printer', 'printer_language', 'Printer Language', 'Select', FALSE, 'PDF,ZPL,TSPL,ESC-POS,HTML', 7),
('Printer', 'label_width_mm', 'Label Width (mm)', 'Number', FALSE, NULL, 8),
('Printer', 'label_height_mm', 'Label Height (mm)', 'Number', FALSE, NULL, 9),
('Printer', 'dpi', 'Printer DPI', 'Number', FALSE, NULL, 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Print audit trail. sticker_print_log already covers barcode stickers only
-- and carries sticker-specific columns (sku/barcode/reprint_reason); this is
-- the general one every QZ job writes to, so a disputed marketplace label
-- reprint can be traced to an operator and a moment. Deliberately additive -
-- sticker_print_log is left exactly as it is and keeps its own screen.
CREATE TABLE IF NOT EXISTS tenant_default.print_job_log (
    id SERIAL PRIMARY KEY,
    job_type VARCHAR(50) NOT NULL,          -- 'Shipping Label' | 'Invoice' | 'Sticker' | 'Receipt'
    document_ref VARCHAR(200),              -- booking id / order id / cart number
    printer_code VARCHAR(100),
    qz_printer_name VARCHAR(255),
    print_format VARCHAR(20),               -- 'PDF' | 'ZPL' | 'TSPL' | 'ESC-POS' | 'HTML'
    copies INT NOT NULL DEFAULT 1,
    printed_by VARCHAR(200),
    status VARCHAR(20) NOT NULL DEFAULT 'Submitted',  -- 'Submitted' | 'Failed'
    error_detail TEXT,
    printed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_print_job_log_printed_at
  ON tenant_default.print_job_log (printed_at DESC);

-- Existing tenants were provisioned by copying tenant_default, so a change
-- made only there never reaches them - the exact failure mode Stage 30.2.2
-- was written to clean up (five tables missing on live). Same DO-block
-- catch-up shape as that migration.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.print_job_log (id SERIAL PRIMARY KEY, job_type VARCHAR(50) NOT NULL, document_ref VARCHAR(200), printer_code VARCHAR(100), qz_printer_name VARCHAR(255), print_format VARCHAR(20), copies INT NOT NULL DEFAULT 1, printed_by VARCHAR(200), status VARCHAR(20) NOT NULL DEFAULT ''Submitted'', error_detail TEXT, printed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)', schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_print_job_log_printed_at ON %I.print_job_log (printed_at DESC)', schema_rec.schema_name);

    -- doctype_fields is a per-tenant table (see db/migration.sql), so the new
    -- Printer fields have to be inserted into each tenant's own copy too.
    -- Guarded on the tenant actually having the Printer doctype, since the
    -- FK to doctype_meta(name) would otherwise fail the whole migration.
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT * FROM (VALUES
        ('Printer', 'qz_printer_name', 'OS Printer Name (exact, from QZ)', 'Data', FALSE, NULL::text, 5),
        ('Printer', 'print_role', 'Default For', 'Select', FALSE, 'Shipping Label,Invoice,Sticker,Receipt,General', 6),
        ('Printer', 'printer_language', 'Printer Language', 'Select', FALSE, 'PDF,ZPL,TSPL,ESC-POS,HTML', 7),
        ('Printer', 'label_width_mm', 'Label Width (mm)', 'Number', FALSE, NULL::text, 8),
        ('Printer', 'label_height_mm', 'Label Height (mm)', 'Number', FALSE, NULL::text, 9),
        ('Printer', 'dpi', 'Printer DPI', 'Number', FALSE, NULL::text, 10)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'Printer')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END $$;
