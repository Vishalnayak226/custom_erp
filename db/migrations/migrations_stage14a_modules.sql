-- Stage 14.1-14.5: Module Registry + Per-Tenant Module Entitlements
--
-- Formalizes the free-text tenant_default.doctype_meta.module label and the
-- existing feature_flags/featureGate pattern (engines/saas.go) into a governed
-- module catalog that can be toggled per tenant/client, enforced across all
-- route groups (not just the 3 integration flags wired today).
--
-- module_key is a NEW, purpose-built taxonomy for access control - finer
-- grained than the legacy free-text `module` column (e.g. Asset/ExpenseClaim
-- are both labelled "Finance" there, but need independent on/off switches
-- here). The legacy `module` column is untouched - this is additive.

-- 1. Global module catalog (same tier as public.tenants)
CREATE TABLE IF NOT EXISTS public.modules (
    module_key VARCHAR(100) PRIMARY KEY,
    display_name VARCHAR(255) NOT NULL,
    description TEXT,
    is_core BOOLEAN DEFAULT FALSE,      -- core modules can never be disabled for a tenant
    default_enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('master_data',  'Master Data',        'Brand/Style/Color/Size/Model/Batch masters', TRUE,  TRUE),
('inventory',    'Inventory',          'Stock ledger, items, transfers, fulfillment tasks', TRUE, TRUE),
('sales',        'Sales & POS',        'Checkout, customers, sales invoices/returns', TRUE, TRUE),
('finance',      'Finance',            'GL postings, trial balance', TRUE, TRUE),
('procurement',  'Procurement',        'Purchase orders, GRN, vendors', FALSE, TRUE),
('hr',           'HR Foundation',      'Employees, attendance, leave, payroll export', FALSE, TRUE),
('manufacturing','Manufacturing',      'BOM, production orders', FALSE, TRUE),
('assets',       'Fixed Assets',       'Asset capitalisation, depreciation, transfer, disposal', FALSE, TRUE),
('expenses',     'Expense Management', 'Claims, verification, payment', FALSE, TRUE),
('rfq',          'RFQ / Vendor Quotes','RFQ, vendor quote comparison', FALSE, TRUE),
('stickers',     'Sticker/Barcode Printing', 'Printer masters, sticker print jobs', FALSE, TRUE),
('crm_loyalty',  'CRM / Loyalty',      'Loyalty point ledger, redeem/earn', FALSE, TRUE),
('reports',      'Report Catalog',     'Stock/sales/vendor/ageing reports', FALSE, TRUE)
ON CONFLICT (module_key) DO NOTHING;

-- 2. Per-tenant module entitlements (cloned into every tenant schema exactly
-- like feature_flags is today - see engines.ProvisionTenantSchema)
CREATE TABLE IF NOT EXISTS tenant_default.module_entitlements (
    module_key VARCHAR(100) PRIMARY KEY,
    enabled BOOLEAN DEFAULT TRUE,
    granted_by VARCHAR(100),
    granted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    note TEXT
);

INSERT INTO tenant_default.module_entitlements (module_key, enabled, granted_by, note) VALUES
('master_data',  TRUE, 'system', 'core module - always enabled'),
('inventory',    TRUE, 'system', 'core module - always enabled'),
('sales',        TRUE, 'system', 'core module - always enabled'),
('finance',      TRUE, 'system', 'core module - always enabled'),
('procurement',  TRUE, 'system', NULL),
('hr',           TRUE, 'system', NULL),
('manufacturing',TRUE, 'system', NULL),
('assets',       TRUE, 'system', NULL),
('expenses',     TRUE, 'system', NULL),
('rfq',          TRUE, 'system', NULL),
('stickers',     TRUE, 'system', NULL),
('crm_loyalty',  TRUE, 'system', NULL),
('reports',      TRUE, 'system', NULL)
ON CONFLICT (module_key) DO NOTHING;

-- 3. Link doctype_meta to the new taxonomy (additive - legacy `module` text
-- column is untouched and keeps rendering in the UI as before)
ALTER TABLE tenant_default.doctype_meta ADD COLUMN IF NOT EXISTS module_key VARCHAR(100);

UPDATE tenant_default.doctype_meta SET module_key = CASE name
    WHEN 'Asset'                 THEN 'assets'
    WHEN 'ExpenseClaim'          THEN 'expenses'
    WHEN 'GLPost'                THEN 'finance'
    WHEN 'MarketplaceSettlement' THEN 'finance'
    WHEN 'Attendance'            THEN 'hr'
    WHEN 'Employee'              THEN 'hr'
    WHEN 'Leave'                 THEN 'hr'
    WHEN 'ASN'                   THEN 'procurement'
    WHEN 'FulfillmentTask'       THEN 'inventory'
    WHEN 'Item'                  THEN 'inventory'
    WHEN 'LogisticsBooking'      THEN 'inventory'
    WHEN 'Printer'               THEN 'stickers'
    WHEN 'StockLedgerEntry'      THEN 'inventory'
    WHEN 'TransferOrder'         THEN 'inventory'
    WHEN 'BOM'                   THEN 'manufacturing'
    WHEN 'ProductionOrder'       THEN 'manufacturing'
    WHEN 'Batch'                 THEN 'master_data'
    WHEN 'Brand'                 THEN 'master_data'
    WHEN 'Color'                 THEN 'master_data'
    WHEN 'Model'                 THEN 'master_data'
    WHEN 'Size'                  THEN 'master_data'
    WHEN 'Style'                 THEN 'master_data'
    WHEN 'POSInvoice'            THEN 'sales'
    WHEN 'GRN'                   THEN 'procurement'
    WHEN 'PurchaseOrder'         THEN 'procurement'
    WHEN 'RFQ'                   THEN 'rfq'
    WHEN 'Vendor'                THEN 'procurement'
    WHEN 'VendorQuote'           THEN 'rfq'
    WHEN 'Customer'              THEN 'sales'
    WHEN 'POSCart'               THEN 'sales'
    WHEN 'SalesInvoice'          THEN 'sales'
    WHEN 'SalesReturn'           THEN 'sales'
    ELSE module_key
END
WHERE module_key IS NULL;
