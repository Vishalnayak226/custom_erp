-- Stage 35.7: commercial bundles, virtual-SKU ATS/order explosion and
-- stocked-kit assembly/disassembly. Manufacturing BOM/VAS remains separate.

INSERT INTO tenant_default.doctype_meta (name,module,module_key,document_type) VALUES
('ProductBundle','OMS','oms','Master'),
('BundleAssembly','OMS','oms','Transaction')
ON CONFLICT(name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name,fieldname,label,fieldtype,mandatory,options,display_order) VALUES
('ProductBundle','code','Bundle ID','Data',TRUE,NULL,1),
('ProductBundle','bundle_sku','Bundle SKU','Link',TRUE,'Item',2),
('ProductBundle','name','Bundle Name','Data',TRUE,NULL,3),
('ProductBundle','fulfillment_mode','Fulfillment Mode','Select',TRUE,'Virtual,Stocked',4),
('ProductBundle','pricing_mode','Pricing Mode','Select',TRUE,'Parent Price,Fixed Price,Component Price',5),
('ProductBundle','fixed_price','Fixed Unit Price','Currency',FALSE,NULL,6),
('ProductBundle','components','Components','JSONTable',TRUE,NULL,7),
('ProductBundle','status','Status','Select',TRUE,'Active,Inactive',8),
('BundleAssembly','code','Operation ID','Data',TRUE,NULL,1),
('BundleAssembly','bundle','Product Bundle','Link',TRUE,'ProductBundle',2),
('BundleAssembly','bundle_sku','Bundle SKU','Link',TRUE,'Item',3),
('BundleAssembly','location_code','Location','Link',TRUE,'Location',4),
('BundleAssembly','quantity','Quantity','Number',TRUE,NULL,5),
('BundleAssembly','operation','Operation','Select',TRUE,'Assemble,Disassemble',6),
('BundleAssembly','request_key','Idempotency Key','Data',FALSE,NULL,7),
('BundleAssembly','component_snapshot','Component Snapshot','JSON',TRUE,NULL,8),
('BundleAssembly','posted_at','Posted At','Datetime',FALSE,NULL,9),
('BundleAssembly','error','Error','Text',FALSE,NULL,10),
('BundleAssembly','status','Status','Select',TRUE,'Draft,Completed,Failed',11),
('SalesOrderLine','bundle_sku','Source Bundle SKU','Link',FALSE,'Item',10),
('SalesOrderLine','bundle_quantity','Ordered Bundle Quantity','Number',FALSE,NULL,11),
('SalesOrderLine','bundle_component_qty','Component Qty per Bundle','Number',FALSE,NULL,12),
('SalesOrderLine','bundle_pricing_mode','Bundle Pricing Mode','Data',FALSE,NULL,13)
ON CONFLICT(doctype_name,fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions(role,doctype_name,allow_read,allow_create,allow_update,allow_delete) VALUES
('Super Admin','ProductBundle',TRUE,TRUE,TRUE,TRUE),
('Store Manager','ProductBundle',TRUE,TRUE,TRUE,FALSE),
('Super Admin','BundleAssembly',TRUE,TRUE,FALSE,FALSE),
('Store Manager','BundleAssembly',TRUE,TRUE,FALSE,FALSE)
ON CONFLICT(role,doctype_name) DO UPDATE SET allow_read=EXCLUDED.allow_read,allow_create=EXCLUDED.allow_create,allow_update=EXCLUDED.allow_update,allow_delete=EXCLUDED.allow_delete;

CREATE UNIQUE INDEX IF NOT EXISTS idx_product_bundle_active_sku
ON tenant_default.documents ((data->>'bundle_sku'))
WHERE doctype='ProductBundle' AND status='Active' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bundle_assembly_sku_date
ON tenant_default.documents ((data->>'bundle_sku'),created_at DESC)
WHERE doctype='BundleAssembly' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_bundle_assembly_request_key
ON tenant_default.documents ((data->>'request_key'))
WHERE doctype='BundleAssembly' AND deleted_at IS NULL AND COALESCE(data->>'request_key','')<>'';

DO $$
DECLARE
  schema_rec RECORD;
  doctype_list text[] := ARRAY['ProductBundle','BundleAssembly','SalesOrderLine'];
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta',schema_rec.schema_name)) IS NULL THEN CONTINUE; END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta(name,module,module_key,document_type)
      SELECT name,module,module_key,document_type FROM tenant_default.doctype_meta WHERE name=ANY($1)
      ON CONFLICT(name) DO UPDATE SET module=EXCLUDED.module,module_key=EXCLUDED.module_key,document_type=EXCLUDED.document_type
    $f$,schema_rec.schema_name) USING doctype_list;
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields(doctype_name,fieldname,label,fieldtype,mandatory,options,display_order)
      SELECT doctype_name,fieldname,label,fieldtype,mandatory,options,display_order FROM tenant_default.doctype_fields WHERE doctype_name=ANY($1)
      ON CONFLICT(doctype_name,fieldname) DO UPDATE SET label=EXCLUDED.label,fieldtype=EXCLUDED.fieldtype,mandatory=EXCLUDED.mandatory,options=EXCLUDED.options,display_order=EXCLUDED.display_order
    $f$,schema_rec.schema_name) USING doctype_list;
    EXECUTE format($f$
      INSERT INTO %I.role_permissions(role,doctype_name,allow_read,allow_create,allow_update,allow_delete)
      SELECT role,doctype_name,allow_read,allow_create,allow_update,allow_delete FROM tenant_default.role_permissions WHERE doctype_name=ANY($1)
      ON CONFLICT(role,doctype_name) DO UPDATE SET allow_read=EXCLUDED.allow_read,allow_create=EXCLUDED.allow_create,allow_update=EXCLUDED.allow_update,allow_delete=EXCLUDED.allow_delete
    $f$,schema_rec.schema_name) USING doctype_list;
    EXECUTE format($f$CREATE UNIQUE INDEX IF NOT EXISTS idx_product_bundle_active_sku ON %I.documents ((data->>'bundle_sku')) WHERE doctype='ProductBundle' AND status='Active' AND deleted_at IS NULL$f$,schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_bundle_assembly_sku_date ON %I.documents ((data->>'bundle_sku'),created_at DESC) WHERE doctype='BundleAssembly' AND deleted_at IS NULL$f$,schema_rec.schema_name);
    EXECUTE format($f$CREATE UNIQUE INDEX IF NOT EXISTS idx_bundle_assembly_request_key ON %I.documents ((data->>'request_key')) WHERE doctype='BundleAssembly' AND deleted_at IS NULL AND COALESCE(data->>'request_key','')<>''$f$,schema_rec.schema_name);
  END LOOP;
END $$;
