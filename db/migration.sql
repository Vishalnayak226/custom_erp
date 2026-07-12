-- Migration Script for ERP Phase 1 (Stage 1)

-- 1. Create tenants table in public schema
CREATE TABLE IF NOT EXISTS public.tenants (
    tenant_id VARCHAR(100) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    schema_name VARCHAR(100) NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Insert a default tenant
INSERT INTO public.tenants (tenant_id, name, schema_name)
VALUES ('default', 'Default ERP Client', 'tenant_default')
ON CONFLICT (tenant_id) DO NOTHING;

-- 2. Create the default tenant schema
CREATE SCHEMA IF NOT EXISTS tenant_default;

-- 3. Create prefix_configs table
CREATE TABLE IF NOT EXISTS tenant_default.prefix_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    doc_type VARCHAR(50) NOT NULL UNIQUE,
    prefix VARCHAR(50) NOT NULL,
    separator VARCHAR(10) DEFAULT '/',
    padding_width INT DEFAULT 6,
    reset_frequency VARCHAR(50) DEFAULT 'ANNUAL', -- ANNUAL, MONTHLY, NEVER
    active_status BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Insert default configurations for standard document types
INSERT INTO tenant_default.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency)
VALUES 
('PR', 'PR', '/', 6, 'ANNUAL'),
('PO', 'PO', '/', 6, 'ANNUAL'),
('GRN', 'GRN', '/', 6, 'ANNUAL'),
('TO', 'TO', '/', 6, 'ANNUAL'),
('TI', 'TI', '/', 6, 'ANNUAL'),
('SI', 'SI', '/', 6, 'ANNUAL')
ON CONFLICT (doc_type) DO NOTHING;

-- 4. Create sequence_counters table
CREATE TABLE IF NOT EXISTS tenant_default.sequence_counters (
    doc_type VARCHAR(50) NOT NULL,
    store_code VARCHAR(50) NOT NULL,
    financial_year VARCHAR(50) NOT NULL,
    current_val BIGINT DEFAULT 0,
    PRIMARY KEY (doc_type, store_code, financial_year)
);

-- 5. Create dynamic_labels table
CREATE TABLE IF NOT EXISTS tenant_default.dynamic_labels (
    original_text VARCHAR(255) PRIMARY KEY,
    custom_text VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 6. Create audit_logs table
CREATE TABLE IF NOT EXISTS tenant_default.audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(100) NOT NULL,
    action VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    details TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 7. Create system_error_logs table
CREATE TABLE IF NOT EXISTS tenant_default.system_error_logs (
    log_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    correlation_id UUID,
    severity VARCHAR(50) NOT NULL, -- PANIC, ERROR, WARNING, INFO
    module_source VARCHAR(100) NOT NULL,
    error_message TEXT NOT NULL,
    stack_trace TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 8. Create doctype_meta table
CREATE TABLE IF NOT EXISTS tenant_default.doctype_meta (
    name VARCHAR(100) PRIMARY KEY,
    module VARCHAR(100) NOT NULL,
    document_type VARCHAR(50) DEFAULT 'Master', -- 'Master' or 'Transaction'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 9. Create doctype_fields table
CREATE TABLE IF NOT EXISTS tenant_default.doctype_fields (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    doctype_name VARCHAR(100) NOT NULL REFERENCES tenant_default.doctype_meta(name) ON DELETE CASCADE,
    fieldname VARCHAR(100) NOT NULL,
    label VARCHAR(100) NOT NULL,
    fieldtype VARCHAR(50) NOT NULL, -- 'Data', 'Number', 'Select', 'Check', 'Date', 'Link'
    mandatory BOOLEAN DEFAULT FALSE,
    options TEXT,
    display_order INT DEFAULT 0,
    UNIQUE (doctype_name, fieldname)
);

-- 10. Create users table
CREATE TABLE IF NOT EXISTS tenant_default.users (
    id VARCHAR(100) PRIMARY KEY,
    username VARCHAR(100) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    email VARCHAR(255),
    role VARCHAR(50) DEFAULT 'Standard',
    status VARCHAR(20) DEFAULT 'Active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 11. Create role_permissions table
CREATE TABLE IF NOT EXISTS tenant_default.role_permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role VARCHAR(50) NOT NULL,
    doctype_name VARCHAR(100) NOT NULL REFERENCES tenant_default.doctype_meta(name) ON DELETE CASCADE,
    allow_read BOOLEAN DEFAULT TRUE,
    allow_create BOOLEAN DEFAULT FALSE,
    allow_update BOOLEAN DEFAULT FALSE,
    allow_delete BOOLEAN DEFAULT FALSE,
    UNIQUE (role, doctype_name)
);

-- 12. Create generic documents table
CREATE TABLE IF NOT EXISTS tenant_default.documents (
    id VARCHAR(100) PRIMARY KEY,
    doctype VARCHAR(100) NOT NULL REFERENCES tenant_default.doctype_meta(name) ON DELETE CASCADE,
    data JSONB NOT NULL,
    status VARCHAR(50) DEFAULT 'Active',
    created_by VARCHAR(100) NOT NULL REFERENCES tenant_default.users(id) ON DELETE CASCADE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_documents_data ON tenant_default.documents USING gin (data);
CREATE INDEX IF NOT EXISTS idx_documents_doctype_status ON tenant_default.documents (doctype, status);

-- 13. Seed default users
-- Each account has a unique password hash (see DEV_CREDENTIALS.local.txt, gitignored, generated by the dev who set this up).
-- These are dev-only placeholders - rotate before any non-dev use.
INSERT INTO tenant_default.users (id, username, password_hash, email, role, status) VALUES
('admin', 'admin', '$2a$10$8IqlLMaxVylUfYsKtF2bxOsN8udFN3XKEeSVbHWuRmMToWCvHuv6W', 'admin@erp.com', 'HR/Admin', 'Active'),
('cashier1', 'cashier1', '$2a$10$u2OOnj/nClI2tPmLfyTPpuePXesLvp1oOwzfK4EAmKFNxbYeJzS5u', 'cashier1@erp.com', 'Cashier', 'Active'),
('manager1', 'manager1', '$2a$10$fHhJ.2w4FG65vw.GNGYn3erEqsCrXUmuI3loj1lJIH58fCVW7gfli', 'manager1@erp.com', 'Store Manager', 'Active'),
('system', 'system', '$2a$10$pGKA1HuK0gtwNaDkE5a25eOzmPgz9cobEJIHeL2RU1e2x7iwema8W', 'system@erp.com', 'HR/Admin', 'Active')
ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash;

-- 14. Seed default Doctype metadata
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('Brand', 'Master Data', 'Master'),
('Style', 'Master Data', 'Master'),
('Color', 'Master Data', 'Master')
ON CONFLICT (name) DO NOTHING;

-- Seed fields for Brand
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Brand', 'code', 'Code', 'Data', TRUE, NULL, 1),
('Brand', 'name', 'Name', 'Data', TRUE, NULL, 2),
('Brand', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed fields for Style
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Style', 'code', 'Code', 'Data', TRUE, NULL, 1),
('Style', 'name', 'Name', 'Data', TRUE, NULL, 2),
('Style', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed fields for Color
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Color', 'code', 'Code', 'Data', TRUE, NULL, 1),
('Color', 'name', 'Name', 'Data', TRUE, NULL, 2),
('Color', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 15. Seed default Role Permissions
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Brand', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'Style', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'Color', TRUE, TRUE, TRUE, TRUE),
('Cashier', 'Brand', TRUE, FALSE, FALSE, FALSE),
('Cashier', 'Style', TRUE, FALSE, FALSE, FALSE),
('Cashier', 'Color', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 16. Audit Log Triggers for Documents
CREATE OR REPLACE FUNCTION tenant_default.log_document_changes()
RETURNS TRIGGER AS $$
DECLARE
    key TEXT;
    old_val TEXT;
    new_val TEXT;
    user_id_val VARCHAR(100);
BEGIN
    user_id_val := COALESCE(NEW.created_by, 'system');
    
    FOR key IN SELECT jsonb_object_keys(OLD.data)
    LOOP
        old_val := OLD.data ->> key;
        new_val := NEW.data ->> key;
        
        IF old_val IS DISTINCT FROM new_val THEN
            INSERT INTO tenant_default.audit_logs (user_id, action, status, details)
            VALUES (
                user_id_val,
                'UPDATE_' || NEW.doctype,
                'SUCCESS',
                'Field "' || key || '" modified from "' || old_val || '" to "' || new_val || '" for Document ID: ' || NEW.id
            );
        END IF;
    END LOOP;
    
    FOR key IN SELECT jsonb_object_keys(NEW.data)
    LOOP
        IF NOT OLD.data ? key THEN
            new_val := NEW.data ->> key;
            INSERT INTO tenant_default.audit_logs (user_id, action, status, details)
            VALUES (
                user_id_val,
                'UPDATE_' || NEW.doctype,
                'SUCCESS',
                'Field "' || key || '" added with value "' || new_val || '" for Document ID: ' || NEW.id
            );
        END IF;
    END LOOP;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION tenant_default.log_document_insert_delete()
RETURNS TRIGGER AS $$
DECLARE
    user_id_val VARCHAR(100);
BEGIN
    IF (TG_OP = 'INSERT') THEN
        user_id_val := COALESCE(NEW.created_by, 'system');
        INSERT INTO tenant_default.audit_logs (user_id, action, status, details)
        VALUES (
            user_id_val,
            'CREATE_' || NEW.doctype,
            'SUCCESS',
            'Created Document ID: ' || NEW.id || ' with data: ' || NEW.data::text
        );
        RETURN NEW;
    ELSIF (TG_OP = 'DELETE') THEN
        user_id_val := COALESCE(OLD.created_by, 'system');
        INSERT INTO tenant_default.audit_logs (user_id, action, status, details)
        VALUES (
            user_id_val,
            'DELETE_' || OLD.doctype,
            'SUCCESS',
            'Deleted Document ID: ' || OLD.id || ' having data: ' || OLD.data::text
        );
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_log_document_changes
AFTER UPDATE ON tenant_default.documents
FOR EACH ROW
EXECUTE FUNCTION tenant_default.log_document_changes();

CREATE TRIGGER trg_log_document_insert_delete
AFTER INSERT OR DELETE ON tenant_default.documents
FOR EACH ROW
EXECUTE FUNCTION tenant_default.log_document_insert_delete();

-- 17. Add Omnichannel & WMS Scale Foundation Tables
CREATE TABLE IF NOT EXISTS tenant_default.inventory_availability (
    sku VARCHAR(100) NOT NULL,
    location_code VARCHAR(100) NOT NULL,
    on_hand INT NOT NULL DEFAULT 0,
    available INT NOT NULL DEFAULT 0,
    committed INT NOT NULL DEFAULT 0,
    reserved INT NOT NULL DEFAULT 0,
    safety_stock INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (sku, location_code)
);

CREATE TABLE IF NOT EXISTS tenant_default.inventory_reservation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sku VARCHAR(100) NOT NULL,
    location_code VARCHAR(100) NOT NULL,
    quantity INT NOT NULL DEFAULT 0,
    reservation_type VARCHAR(50) NOT NULL, -- 'Online', 'Cart', 'StoreCustomer', 'Transfer'
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tenant_default.integration_event_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_name VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(50) DEFAULT 'Pending', -- 'Pending', 'Dispatched', 'Failed'
    attempts INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tenant_default.integration_event_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL,
    channel VARCHAR(100) NOT NULL, -- 'Shopify', 'WMS', 'OMS'
    status VARCHAR(50) NOT NULL, -- 'Success', 'Failed'
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Seed WMS DocTypes metadata
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('Item', 'Inventory', 'Master'),
('PurchaseOrder', 'Procurement', 'Transaction'),
('ASN', 'Inbound', 'Transaction'),
('SalesInvoice', 'Sales', 'Transaction'),
('TransferOrder', 'Inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- Seed fields for Item
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'code', 'Code', 'Data', TRUE, NULL, 1),
('Item', 'name', 'Name', 'Data', TRUE, NULL, 2),
('Item', 'barcode', 'Barcode', 'Data', TRUE, NULL, 3),
('Item', 'weight', 'Weight', 'Number', FALSE, NULL, 4),
('Item', 'volume', 'Volume', 'Number', FALSE, NULL, 5),
('Item', 'category', 'Category', 'Data', FALSE, NULL, 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed fields for PurchaseOrder
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PurchaseOrder', 'po_number', 'PO Number', 'Data', TRUE, NULL, 1),
('PurchaseOrder', 'vendor', 'Vendor', 'Data', TRUE, NULL, 2),
('PurchaseOrder', 'target_warehouse', 'Target Warehouse', 'Data', TRUE, NULL, 3),
('PurchaseOrder', 'status', 'Status', 'Select', TRUE, 'Draft,Approved,Closed', 4),
('PurchaseOrder', 'location', 'Location', 'Data', TRUE, NULL, 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed fields for ASN
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ASN', 'asn_number', 'ASN Number', 'Data', TRUE, NULL, 1),
('ASN', 'po_number', 'PO Number', 'Data', TRUE, NULL, 2),
('ASN', 'status', 'Status', 'Select', TRUE, 'Expected,Received,Cancelled', 3),
('ASN', 'location', 'Location', 'Data', TRUE, NULL, 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed fields for SalesInvoice
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SalesInvoice', 'invoice_number', 'Invoice Number', 'Data', TRUE, NULL, 1),
('SalesInvoice', 'customer', 'Customer', 'Data', FALSE, NULL, 2),
('SalesInvoice', 'status', 'Status', 'Select', TRUE, 'Draft,Approved,Paid,Cancelled', 3),
('SalesInvoice', 'location', 'Location', 'Data', TRUE, NULL, 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed fields for TransferOrder
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('TransferOrder', 'transfer_number', 'Transfer Number', 'Data', TRUE, NULL, 1),
('TransferOrder', 'from_warehouse', 'From Warehouse', 'Data', TRUE, NULL, 2),
('TransferOrder', 'to_warehouse', 'To Warehouse', 'Data', TRUE, NULL, 3),
('TransferOrder', 'status', 'Status', 'Select', TRUE, 'Draft,Approved,Dispatched,Received', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed role permissions for new WMS doctypes
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Item', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'PurchaseOrder', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'ASN', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'SalesInvoice', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'TransferOrder', TRUE, TRUE, TRUE, TRUE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 18. Add GL Accounts and Double-Entry Postings Tables
CREATE TABLE IF NOT EXISTS tenant_default.gl_accounts (
    account_code VARCHAR(50) PRIMARY KEY,
    account_name VARCHAR(100) NOT NULL,
    account_type VARCHAR(50) NOT NULL -- 'Asset', 'Liability', 'Equity', 'Revenue', 'Expense'
);

CREATE TABLE IF NOT EXISTS tenant_default.gl_postings (
    posting_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_code VARCHAR(50) NOT NULL REFERENCES tenant_default.gl_accounts(account_code) ON DELETE RESTRICT,
    debit INT NOT NULL DEFAULT 0,
    credit INT NOT NULL DEFAULT 0,
    document_type VARCHAR(50) NOT NULL,
    document_id VARCHAR(100) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Seed Chart of Accounts
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('1100', 'Cash/Bank Account', 'Asset'),
('1200', 'Inventory Control Account', 'Asset'),
('2100', 'GRN Suspense Account', 'Liability'),
('4100', 'Sales Revenue Account', 'Revenue'),
('5100', 'Cost of Goods Sold (COGS) Account', 'Expense')
ON CONFLICT (account_code) DO NOTHING;

-- Seed POSCart and SalesReturn doctype metadata
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('POSCart', 'Sales', 'Transaction'),
('SalesReturn', 'Sales', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- Seed fields for POSCart
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('POSCart', 'cart_number', 'Cart Number', 'Data', TRUE, NULL, 1),
('POSCart', 'location', 'Location', 'Data', TRUE, NULL, 2),
('POSCart', 'payment_mode', 'Payment Mode', 'Select', TRUE, 'Cash,Card,UPI', 3),
('POSCart', 'amount_paid', 'Amount Paid', 'Number', TRUE, NULL, 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed fields for SalesReturn
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SalesReturn', 'return_number', 'Return Number', 'Data', TRUE, NULL, 1),
('SalesReturn', 'invoice_id', 'Invoice ID', 'Data', TRUE, NULL, 2),
('SalesReturn', 'amount_refunded', 'Amount Refunded', 'Number', TRUE, NULL, 3)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed permissions for POSCart and SalesReturn
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'POSCart', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'SalesReturn', TRUE, TRUE, TRUE, TRUE),
('Cashier', 'POSCart', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'SalesReturn', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 19. Add Shopify / Channel mapping tables
CREATE TABLE IF NOT EXISTS tenant_default.channel_product_mapping (
    sku VARCHAR(100) NOT NULL,
    channel VARCHAR(50) NOT NULL,
    channel_sku VARCHAR(100) NOT NULL,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (sku, channel)
);

CREATE TABLE IF NOT EXISTS tenant_default.channel_order_mapping (
    order_id VARCHAR(100) NOT NULL,
    channel VARCHAR(50) NOT NULL,
    channel_order_id VARCHAR(100) NOT NULL,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (channel_order_id, channel)
);

-- 20. Seed FulfillmentTask doctype metadata
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('FulfillmentTask', 'Inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- Seed fields for FulfillmentTask
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('FulfillmentTask', 'code', 'Task ID', 'Data', TRUE, NULL, 1),
('FulfillmentTask', 'order_id', 'Order ID', 'Data', TRUE, NULL, 2),
('FulfillmentTask', 'location_code', 'Location Code', 'Data', TRUE, NULL, 3),
('FulfillmentTask', 'status', 'Status', 'Select', TRUE, 'Pending,Picking,Packed,Dispatched,Rejected', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed role permissions for FulfillmentTask
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'FulfillmentTask', TRUE, TRUE, TRUE, TRUE),
('Cashier', 'FulfillmentTask', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Seed new accounts to Chart of Accounts
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('1300', 'Accounts Receivable', 'Asset'),
('5200', 'Marketplace Commission Expense', 'Expense')
ON CONFLICT (account_code) DO NOTHING;

-- Seed MarketplaceSettlement and LogisticsBooking doctype metadata
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('MarketplaceSettlement', 'Finance', 'Transaction'),
('LogisticsBooking', 'Inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- Seed fields for MarketplaceSettlement
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('MarketplaceSettlement', 'code', 'Settlement ID', 'Data', TRUE, NULL, 1),
('MarketplaceSettlement', 'channel', 'Channel', 'Select', TRUE, 'Shopify,Amazon', 2),
('MarketplaceSettlement', 'payout_date', 'Payout Date', 'Date', TRUE, NULL, 3),
('MarketplaceSettlement', 'total_sale', 'Total Sale', 'Number', TRUE, NULL, 4),
('MarketplaceSettlement', 'commission', 'Commission Deducted', 'Number', TRUE, NULL, 5),
('MarketplaceSettlement', 'net_payout', 'Net Payout', 'Number', TRUE, NULL, 6),
('MarketplaceSettlement', 'status', 'Status', 'Select', TRUE, 'Unreconciled,Reconciled', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed fields for LogisticsBooking
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('LogisticsBooking', 'code', 'Booking ID', 'Data', TRUE, NULL, 1),
('LogisticsBooking', 'order_id', 'Order ID', 'Data', TRUE, NULL, 2),
('LogisticsBooking', 'carrier', 'Carrier Name', 'Data', TRUE, NULL, 3),
('LogisticsBooking', 'tracking_number', 'Tracking Number', 'Data', TRUE, NULL, 4),
('LogisticsBooking', 'shipping_charge', 'Shipping Charge', 'Number', TRUE, NULL, 5),
('LogisticsBooking', 'status', 'Status', 'Select', TRUE, 'Shipped,In-Transit,Delivered', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Seed permissions for MarketplaceSettlement and LogisticsBooking
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'MarketplaceSettlement', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'LogisticsBooking', TRUE, TRUE, TRUE, TRUE),
('Cashier', 'MarketplaceSettlement', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'LogisticsBooking', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 21. Create feature_flags table per tenant
CREATE TABLE IF NOT EXISTS tenant_default.feature_flags (
    feature_name VARCHAR(100) PRIMARY KEY,
    enabled BOOLEAN DEFAULT TRUE
);

-- Seed some default feature flags
INSERT INTO tenant_default.feature_flags (feature_name, enabled) VALUES
('wms_integration', TRUE),
('oms_integration', TRUE),
('advanced_forecasting', TRUE)
ON CONFLICT (feature_name) DO NOTHING;
