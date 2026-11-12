-- Stage 30.5.3: replace hand-typed JSON with line editors on the five Master
-- doctypes whose only editor is the generic record form.
--
-- 19 fields in this system ask for JSON in a text box. Most belong to
-- doctypes that already have a bespoke screen with a proper add-line editor
-- (GRN Workbench, Stock Transfer, ASN, POS, Purchase Order). Five do not -
-- BOM, Routing, AppraisalCycle, ReportColumnProfile and ReportFilterPreset
-- are reachable straight from the Setup menu with nothing but the generic
-- form behind them, so creating a BOM literally meant typing
-- [{"sku":"...","qty":2}] by hand into a Data field, with no validation of
-- any kind: a malformed string saved happily and only failed much later,
-- inside the MRP explosion.
--
-- Two new fieldtypes carry this, rather than a doctype/field list hardcoded
-- in app.js:
--
--   JSONTable - a JSON array of objects. `options` holds the column spec.
--   JSONMap   - a JSON object of key/value pairs. `options` is unused.
--
-- Storage does not change at all: the column stays text and still holds the
-- same serialised JSON, so every Go consumer (explodeBOMComponents,
-- fetchRoutingOperations, byProducts, applyReportCatalogSavedFilter) reads
-- exactly what it read before. This is a rendering and validation change, not
-- a schema rework - which is also why it needs no data backfill.
--
-- Column spec grammar (JSON array in doctype_fields.options):
--   key      - the JSON property name written into each row object
--   label    - column heading in the editor
--   type     - text | number | link
--   link     - target doctype when type is link (renders a typeahead)
--   required - the line editor refuses to serialise a row missing this
--
-- Every key below was read off the Go struct that consumes it, not off the
-- old label text, so the editor cannot emit a shape the engine will not
-- understand.

-- 1. BOM.components -> [{sku, qty, sub_bom?, scrap_percent?}]
--    (engines/manufacturing.go's bomComponent)
UPDATE tenant_default.doctype_fields
   SET fieldtype = 'JSONTable',
       label     = 'Components',
       options   = '[{"key":"sku","label":"Component SKU","type":"link","link":"Item","required":true},
                     {"key":"qty","label":"Qty per Unit","type":"number","required":true},
                     {"key":"sub_bom","label":"Sub-BOM","type":"link","link":"BOM"},
                     {"key":"scrap_percent","label":"Scrap %","type":"number"}]'
 WHERE doctype_name = 'BOM' AND fieldname = 'components';

-- 2. BOM.by_products -> [{sku, qty_per_unit}]
--    (engines/manufacturing_mrp.go's byProductLine)
UPDATE tenant_default.doctype_fields
   SET fieldtype = 'JSONTable',
       label     = 'By-Products',
       options   = '[{"key":"sku","label":"By-Product SKU","type":"link","link":"Item","required":true},
                     {"key":"qty_per_unit","label":"Qty per Unit","type":"number","required":true}]'
 WHERE doctype_name = 'BOM' AND fieldname = 'by_products';

-- 3. Routing.operations -> [{seq, operation_name, work_center_id, setup_time_mins, run_time_mins_per_unit}]
--    (engines/manufacturing_mrp.go's routingOperation)
UPDATE tenant_default.doctype_fields
   SET fieldtype = 'JSONTable',
       label     = 'Operations',
       options   = '[{"key":"seq","label":"Seq","type":"number","required":true},
                     {"key":"operation_name","label":"Operation","type":"text","required":true},
                     {"key":"work_center_id","label":"Work Center","type":"link","link":"WorkCenter","required":true},
                     {"key":"setup_time_mins","label":"Setup (mins)","type":"number"},
                     {"key":"run_time_mins_per_unit","label":"Run (mins/unit)","type":"number"}]'
 WHERE doctype_name = 'Routing' AND fieldname = 'operations';

-- 4. AppraisalCycle.kra_template -> [{kra, weight}]
UPDATE tenant_default.doctype_fields
   SET fieldtype = 'JSONTable',
       label     = 'KRA / KPI Template',
       options   = '[{"key":"kra","label":"KRA / KPI","type":"text","required":true},
                     {"key":"weight","label":"Weight","type":"number","required":true}]'
 WHERE doctype_name = 'AppraisalCycle' AND fieldname = 'kra_template';

-- 5. ReportColumnProfile.columns -> [{key, visible}]
--    (public/app.js's applyReportColumnProfile; `label` is resolved from the
--    live report definition at render time, so it is deliberately not stored)
UPDATE tenant_default.doctype_fields
   SET fieldtype = 'JSONTable',
       label     = 'Columns',
       options   = '[{"key":"key","label":"Column Key","type":"text","required":true},
                     {"key":"visible","label":"Visible","type":"text"}]'
 WHERE doctype_name = 'ReportColumnProfile' AND fieldname = 'columns';

-- 6. ReportFilterPreset.params is a key/value OBJECT, not an array - the
--    report catalog writes {param_key: value} - so it gets JSONMap.
UPDATE tenant_default.doctype_fields
   SET fieldtype = 'JSONMap',
       label     = 'Saved Parameters',
       options   = NULL
 WHERE doctype_name = 'ReportFilterPreset' AND fieldname = 'params';

-- Backfill every already-provisioned tenant schema. Copies tenant_default's
-- rows rather than repeating the specs, so the two cannot drift.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      UPDATE %I.doctype_fields t
         SET fieldtype = d.fieldtype, label = d.label, options = d.options
        FROM tenant_default.doctype_fields d
       WHERE d.doctype_name = t.doctype_name
         AND d.fieldname    = t.fieldname
         AND d.fieldtype IN ('JSONTable', 'JSONMap')
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
