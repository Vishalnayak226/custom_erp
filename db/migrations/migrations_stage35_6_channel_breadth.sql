-- Stage 35.6: full connector SDK, Indian marketplace/quick-commerce breadth,
-- WooCommerce, scheduled ATS sync, managed SKU mappings/exceptions and health.

ALTER TABLE tenant_default.channel_product_mapping ADD COLUMN IF NOT EXISTS external_product_id VARCHAR(150);
ALTER TABLE tenant_default.channel_product_mapping ADD COLUMN IF NOT EXISTS location_code VARCHAR(100);

INSERT INTO tenant_default.doctype_meta (name,module,module_key,document_type) VALUES
('ChannelSyncRun','OMS','oms','Transaction'),
('ChannelSKUException','OMS','oms','Transaction')
ON CONFLICT(name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name,fieldname,label,fieldtype,mandatory,options,display_order) VALUES
('Channel','connector_kind','Connector Kind','Select',FALSE,'marketplace,quick_commerce,webstore',8),
('Channel','location_code','Fulfilment Location','Link',FALSE,'Location',9),
('Channel','no_split','Forbid Split Shipment','Check',FALSE,NULL,10),
('Channel','inventory_buffer','Oversell Buffer','Number',FALSE,NULL,11),
('Channel','sync_interval_minutes','Sync Interval (Minutes)','Number',FALSE,NULL,12),
('Channel','last_cursor','Last Order Cursor','Data',FALSE,NULL,13),
('SalesOrder','shipping_state','Shipping State','Data',FALSE,NULL,17),
('ChannelSyncRun','code','Run ID','Data',TRUE,NULL,1),
('ChannelSyncRun','channel','Channel','Link',TRUE,'Channel',2),
('ChannelSyncRun','operation','Operation','Select',TRUE,'Order Pull,Inventory Push,Status Push,Catalogue Push',3),
('ChannelSyncRun','processed','Processed','Number',FALSE,NULL,4),
('ChannelSyncRun','failed','Failed','Number',FALSE,NULL,5),
('ChannelSyncRun','error','Last Error','Text',FALSE,NULL,6),
('ChannelSyncRun','started_at','Started At','Datetime',TRUE,NULL,7),
('ChannelSyncRun','finished_at','Finished At','Datetime',TRUE,NULL,8),
('ChannelSyncRun','status','Status','Select',TRUE,'Success,Failed',9),
('ChannelSKUException','code','Exception ID','Data',TRUE,NULL,1),
('ChannelSKUException','channel','Channel','Link',TRUE,'Channel',2),
('ChannelSKUException','channel_sku','Unmapped Channel SKU','Data',TRUE,NULL,3),
('ChannelSKUException','first_order_id','First Channel Order','Data',FALSE,NULL,4),
('ChannelSKUException','last_order_id','Latest Channel Order','Data',FALSE,NULL,5),
('ChannelSKUException','occurrences','Occurrences','Number',TRUE,NULL,6),
('ChannelSKUException','resolved_sku','Resolved Item','Link',FALSE,'Item',7),
('ChannelSKUException','status','Status','Select',TRUE,'Open,Resolved',8)
ON CONFLICT(doctype_name,fieldname) DO NOTHING;

UPDATE tenant_default.doctype_fields SET options='Generic,Shopify,BigCommerce,Magento,AdobeCommerce,Amazon,Flipkart,Myntra,Meesho,Ajio,Nykaa,Blinkit,Zepto,SwiggyInstamart,WooCommerce' WHERE doctype_name='Channel' AND fieldname='platform';

INSERT INTO tenant_default.role_permissions(role,doctype_name,allow_read,allow_create,allow_update,allow_delete) VALUES
('Super Admin','ChannelSyncRun',TRUE,FALSE,FALSE,FALSE),
('Store Manager','ChannelSyncRun',TRUE,FALSE,FALSE,FALSE),
('Super Admin','ChannelSKUException',TRUE,TRUE,TRUE,TRUE),
('Store Manager','ChannelSKUException',TRUE,TRUE,TRUE,FALSE)
ON CONFLICT(role,doctype_name) DO UPDATE SET allow_read=EXCLUDED.allow_read,allow_create=EXCLUDED.allow_create,allow_update=EXCLUDED.allow_update,allow_delete=EXCLUDED.allow_delete;

CREATE INDEX IF NOT EXISTS idx_channel_sync_run_health ON tenant_default.documents ((data->>'channel'),created_at DESC) WHERE doctype='ChannelSyncRun';
CREATE INDEX IF NOT EXISTS idx_channel_sku_exception_open ON tenant_default.documents ((data->>'channel'),(data->>'channel_sku')) WHERE doctype='ChannelSKUException' AND status='Open' AND deleted_at IS NULL;

-- Existing tenants own independent metadata and mapping tables. Provisioning
-- clones tenant_default for future tenants; this block brings every current
-- tenant to the same additive shape without assuming tenant names.
DO $$
DECLARE
  schema_rec RECORD;
  doctype_list text[] := ARRAY['Channel','SalesOrder','ChannelSyncRun','ChannelSKUException'];
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL
       OR to_regclass(format('%I.channel_product_mapping', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format('ALTER TABLE %I.channel_product_mapping ADD COLUMN IF NOT EXISTS external_product_id VARCHAR(150)', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE %I.channel_product_mapping ADD COLUMN IF NOT EXISTS location_code VARCHAR(100)', schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name,module,module_key,document_type)
      SELECT name,module,module_key,document_type FROM tenant_default.doctype_meta WHERE name=ANY($1)
      ON CONFLICT(name) DO UPDATE SET module=EXCLUDED.module,module_key=EXCLUDED.module_key,document_type=EXCLUDED.document_type
    $f$, schema_rec.schema_name) USING doctype_list;

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name,fieldname,label,fieldtype,mandatory,options,display_order)
      SELECT doctype_name,fieldname,label,fieldtype,mandatory,options,display_order FROM tenant_default.doctype_fields WHERE doctype_name=ANY($1)
      ON CONFLICT(doctype_name,fieldname) DO UPDATE SET label=EXCLUDED.label,fieldtype=EXCLUDED.fieldtype,mandatory=EXCLUDED.mandatory,options=EXCLUDED.options,display_order=EXCLUDED.display_order
    $f$, schema_rec.schema_name) USING doctype_list;

    EXECUTE format($f$
      INSERT INTO %I.role_permissions (role,doctype_name,allow_read,allow_create,allow_update,allow_delete)
      SELECT role,doctype_name,allow_read,allow_create,allow_update,allow_delete FROM tenant_default.role_permissions WHERE doctype_name=ANY($1)
      ON CONFLICT(role,doctype_name) DO UPDATE SET allow_read=EXCLUDED.allow_read,allow_create=EXCLUDED.allow_create,allow_update=EXCLUDED.allow_update,allow_delete=EXCLUDED.allow_delete
    $f$, schema_rec.schema_name) USING doctype_list;

    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_channel_sync_run_health ON %I.documents ((data->>'channel'),created_at DESC) WHERE doctype='ChannelSyncRun'$f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_channel_sku_exception_open ON %I.documents ((data->>'channel'),(data->>'channel_sku')) WHERE doctype='ChannelSKUException' AND status='Open' AND deleted_at IS NULL$f$, schema_rec.schema_name);
  END LOOP;
END $$;
