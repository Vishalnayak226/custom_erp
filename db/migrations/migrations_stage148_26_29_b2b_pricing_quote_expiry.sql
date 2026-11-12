-- ---------------------------------------------------------------------------
-- Stage 148.26 / 148.29: pricing reaches B2B, and quote expiry.
--
-- 148.26 reuses PriceListVersion (148.6, D2: no second price engine). A
-- customer list row may now also carry a karat and weight band (jewellery,
-- keyed per design group), a quota and a floor. Item gets an optional floor
-- price. 148.29 adds the Expired status a quote can be given.
--
-- Metadata only (doctype_fields). No document row is written, and nothing
-- changes for a client until it turns on sales.b2b_pricing / sales.below_floor
-- / crm.quote_expiry. Safe to re-run; tenant_default first, then every other
-- tenant.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'floor_price', 'Floor Price', 'Number', FALSE, NULL, 78)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

UPDATE tenant_default.doctype_fields
SET options = ((options::jsonb) || '[{"key":"karat","label":"Karat","type":"number"},
   {"key":"weight_from","label":"Weight from (g)","type":"number"},
   {"key":"weight_to","label":"Weight to (g)","type":"number"},
   {"key":"quota_qty","label":"Quota qty","type":"number"},
   {"key":"floor","label":"Floor","type":"number"}]'::jsonb)::text
WHERE doctype_name = 'PriceListVersion' AND fieldname = 'items'
  AND options IS NOT NULL AND options LIKE '[%' AND options NOT LIKE '%"key":"karat"%';

UPDATE tenant_default.doctype_fields SET options = 'Draft,Pending Approval,Approved,Rejected,Converted,Expired'
WHERE doctype_name = 'SalesQuotation' AND fieldname = 'status' AND options NOT LIKE '%Expired%';

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
        WHERE (doctype_name, fieldname) IN (('Item', 'floor_price'))
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        UPDATE %1$I.doctype_fields t
        SET options = ((t.options::jsonb) || '[{"key":"karat","label":"Karat","type":"number"},
           {"key":"weight_from","label":"Weight from (g)","type":"number"},
           {"key":"weight_to","label":"Weight to (g)","type":"number"},
           {"key":"quota_qty","label":"Quota qty","type":"number"},
           {"key":"floor","label":"Floor","type":"number"}]'::jsonb)::text
        WHERE t.doctype_name = 'PriceListVersion' AND t.fieldname = 'items'
          AND t.options IS NOT NULL AND t.options LIKE '[%%' AND t.options NOT LIKE '%%"key":"karat"%%'
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        UPDATE %1$I.doctype_fields SET options = 'Draft,Pending Approval,Approved,Rejected,Converted,Expired'
        WHERE doctype_name = 'SalesQuotation' AND fieldname = 'status' AND options NOT LIKE '%%Expired%%'
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
