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
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    mfa_secret VARCHAR(64),
    mfa_enabled BOOLEAN DEFAULT FALSE
);

-- 10.1 MFA columns (idempotent add-if-missing, for DBs migrated before this
-- was part of the CREATE TABLE above - see engines/mfa.go, Stage 13.3)
ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS mfa_secret VARCHAR(64);
ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN DEFAULT FALSE;

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
('Item', 'category', 'Category', 'Data', FALSE, NULL, 6),
('Item', 'hsn_code', 'HSN Code', 'Data', FALSE, NULL, 7),
('Item', 'gst_rate', 'GST Rate (%)', 'Number', FALSE, NULL, 8)
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

-- 22. Approval / Workflow Engine (maker-checker) - Stage 13.8
-- approval_log is the append-only audit trail: one row per submit/approve/
-- reject action, independent of the document's current (mutable) status.
CREATE TABLE IF NOT EXISTS tenant_default.approval_log (
    id SERIAL PRIMARY KEY,
    doctype VARCHAR(100) NOT NULL,
    document_id VARCHAR(100) NOT NULL,
    action VARCHAR(20) NOT NULL, -- Submitted, Approved, Rejected, Modified (re-approval reset)
    actor_user_id VARCHAR(100) NOT NULL,
    actor_role VARCHAR(50) NOT NULL,
    amount NUMERIC,
    comment TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_approval_log_doc ON tenant_default.approval_log (doctype, document_id);

-- approval_rules is the amount-slab + role routing config, editable via API
-- the same way prefix_configs/feature_flags are - a doctype with no rule row
-- simply isn't approval-gated (SubmitForApproval rejects it explicitly
-- rather than silently no-op'ing).
CREATE TABLE IF NOT EXISTS tenant_default.approval_rules (
    id SERIAL PRIMARY KEY,
    doctype VARCHAR(100) NOT NULL,
    min_amount NUMERIC NOT NULL DEFAULT 0,
    max_amount NUMERIC, -- NULL = no upper bound
    required_role VARCHAR(50) NOT NULL,
    UNIQUE (doctype, min_amount)
);

-- Pilot doctype: PurchaseOrder. Amounts up to 49999 need a Store Manager;
-- 50000+ needs HR/Admin. HR/Admin can also always approve as the existing
-- catch-all admin role (enforced in engines.DecideApproval, not here).
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('PurchaseOrder', 0, 49999, 'Store Manager'),
('PurchaseOrder', 50000, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;

-- Extend PurchaseOrder's status enum to include the approval workflow states
-- (existing rows/behavior unaffected - Draft/Approved/Closed still work
-- exactly as before for anyone who doesn't use the approval flow).
UPDATE tenant_default.doctype_fields
SET options = 'Draft,Pending Approval,Approved,Rejected,Closed'
WHERE doctype_name = 'PurchaseOrder' AND fieldname = 'status';

-- Give Store Manager read/update access to PurchaseOrder so they can
-- actually see and act on documents routed to them for approval - no prior
-- role_permissions row existed for Store Manager on this doctype at all.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Store Manager', 'PurchaseOrder', TRUE, FALSE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 23. Dedicated Vendor/Customer masters (Stage 13.9) - MB Sec.4.5. Confirmed
-- absent 2026-07-13 by a live-DB check; PurchaseOrder/SalesInvoice only ever
-- had free-text vendor/customer fields. Registered as document_type='Master'
-- so both appear automatically under the existing "Master Definition"
-- submenu with full generic CRUD - no new frontend code needed for that part.
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('Vendor', 'Procurement', 'Master'),
('Customer', 'Sales', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Vendor', 'code', 'Vendor Code', 'Data', TRUE, NULL, 1),
('Vendor', 'name', 'Vendor Name', 'Data', TRUE, NULL, 2),
('Vendor', 'gstin', 'GSTIN', 'Data', FALSE, NULL, 3),
('Vendor', 'bank_account_number', 'Bank Account Number', 'Data', FALSE, NULL, 4),
('Vendor', 'bank_ifsc', 'Bank IFSC', 'Data', FALSE, NULL, 5),
('Vendor', 'contact_phone', 'Contact Phone', 'Data', FALSE, NULL, 6),
('Vendor', 'contact_email', 'Contact Email', 'Data', FALSE, NULL, 7),
('Vendor', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Customer', 'code', 'Customer Code', 'Data', TRUE, NULL, 1),
('Customer', 'name', 'Customer Name', 'Data', TRUE, NULL, 2),
('Customer', 'phone', 'Phone', 'Data', FALSE, NULL, 3),
('Customer', 'email', 'Email', 'Data', FALSE, NULL, 4),
('Customer', 'loyalty_points', 'Loyalty Points', 'Number', FALSE, NULL, 5),
('Customer', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- HR/Admin-only, matching the existing permission pattern for other
-- master doctypes (Brand, Item, etc. - none grant Cashier/Store Manager
-- access to master data management by default in this codebase).
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Vendor', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'Customer', TRUE, TRUE, TRUE, TRUE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 24. RFQ / Vendor Quote / Quote Comparison (Stage 13.12) - procurement
-- went straight to PurchaseOrder before this; not explicitly phased in the
-- gap analysis's own plan, grouped as functional breadth (MB Sec.8.3).
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('RFQ', 'Procurement', 'Transaction'),
('VendorQuote', 'Procurement', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('RFQ', 'code', 'RFQ Number', 'Data', TRUE, NULL, 1),
('RFQ', 'description', 'Item / Requirement Description', 'Data', TRUE, NULL, 2),
('RFQ', 'quantity', 'Quantity', 'Number', TRUE, NULL, 3),
('RFQ', 'target_date', 'Target Date', 'Date', FALSE, NULL, 4),
('RFQ', 'status', 'Status', 'Select', TRUE, 'Draft,Sent,Closed', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('VendorQuote', 'code', 'Quote Number', 'Data', TRUE, NULL, 1),
('VendorQuote', 'rfq_id', 'RFQ Reference', 'Link', TRUE, 'RFQ', 2),
('VendorQuote', 'vendor', 'Vendor', 'Data', TRUE, NULL, 3),
('VendorQuote', 'quoted_price', 'Quoted Price', 'Number', TRUE, NULL, 4),
('VendorQuote', 'lead_time_days', 'Lead Time (days)', 'Number', FALSE, NULL, 5),
('VendorQuote', 'status', 'Status', 'Select', TRUE, 'Submitted,Selected,Rejected', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- HR/Admin creates/manages RFQs and quotes; Store Manager can read/submit
-- quotes for their own procurement needs (same read/update-only pattern
-- given to Store Manager on PurchaseOrder for the approval flow, Stage 13.8).
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'RFQ', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'VendorQuote', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'RFQ', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'VendorQuote', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 25. Sticker/Barcode Printing (Stage 13.15) - MB Sec.15.3. Printer master
-- registered as document_type='Master' (same pattern as Vendor/Customer,
-- Stage 13.9) so it appears under Master Definition automatically.
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('Printer', 'Inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Printer', 'code', 'Printer Code', 'Data', TRUE, NULL, 1),
('Printer', 'name', 'Printer Name', 'Data', TRUE, NULL, 2),
('Printer', 'location', 'Location', 'Data', FALSE, NULL, 3),
('Printer', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Printer', TRUE, TRUE, TRUE, TRUE),
('Cashier', 'Printer', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'Printer', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Print History: an append-only audit trail (who printed what, on which
-- printer, when, and why if it's a reprint) - a dedicated SQL table rather
-- than a doctype/document, matching the approval_log pattern (Stage 13.8),
-- since this is a system-generated log, not a user-editable record.
CREATE TABLE IF NOT EXISTS tenant_default.sticker_print_log (
    id SERIAL PRIMARY KEY,
    sku VARCHAR(100) NOT NULL,
    barcode VARCHAR(100),
    printer_code VARCHAR(100) NOT NULL,
    printed_by VARCHAR(100) NOT NULL,
    copies INT NOT NULL DEFAULT 1,
    reprint_reason TEXT,
    printed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sticker_print_log_sku ON tenant_default.sticker_print_log (sku);

-- 26. HR Foundation (Stage 13.13a) - MB Sec.16.3. Employee is registered as
-- document_type='Master' (appears under Master Definition automatically,
-- same pattern as Vendor/Customer/Printer); Attendance/Leave are
-- 'Transaction' type since they're day-to-day records, not master data.
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('Employee', 'HR', 'Master'),
('Attendance', 'HR', 'Transaction'),
('Leave', 'HR', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Employee', 'code', 'Employee ID', 'Data', TRUE, NULL, 1),
('Employee', 'name', 'Name', 'Data', TRUE, NULL, 2),
('Employee', 'department', 'Department', 'Data', FALSE, NULL, 3),
('Employee', 'designation', 'Designation', 'Data', FALSE, NULL, 4),
('Employee', 'location', 'Location', 'Data', FALSE, NULL, 5),
('Employee', 'reporting_manager', 'Reporting Manager (Employee ID)', 'Data', FALSE, NULL, 6),
('Employee', 'user_id', 'Linked ERP User ID', 'Data', FALSE, NULL, 7),
('Employee', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Attendance', 'code', 'Attendance Code', 'Data', TRUE, NULL, 1),
('Attendance', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
('Attendance', 'date', 'Date', 'Date', TRUE, NULL, 3),
('Attendance', 'location', 'Location', 'Data', FALSE, NULL, 4),
('Attendance', 'status', 'Status', 'Select', TRUE, 'Present,Absent,Late,Leave,Holiday,WeeklyOff', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Leave', 'code', 'Leave Code', 'Data', TRUE, NULL, 1),
('Leave', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
('Leave', 'leave_type', 'Leave Type', 'Select', TRUE, 'Casual,Sick,Earned,Unpaid', 3),
('Leave', 'from_date', 'From Date', 'Date', TRUE, NULL, 4),
('Leave', 'to_date', 'To Date', 'Date', TRUE, NULL, 5),
('Leave', 'days', 'Days', 'Number', TRUE, NULL, 6),
('Leave', 'status', 'Status', 'Select', TRUE, 'Applied,Approved,Rejected', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- HR/Admin manages all HR data. Store Manager can read Employee (know
-- their team) and create/read/update Attendance+Leave for day-to-day
-- store operations (mark attendance, approve leave) - same read/update
-- pattern already given for PurchaseOrder approvals (Stage 13.8).
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Employee', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'Attendance', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'Leave', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Employee', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'Attendance', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'Leave', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 27. Fixed Asset Management (Stage 13.13b) - MB Sec.16.1. Scoped to the
-- asset-specific lifecycle (capitalisation -> depreciation -> transfer ->
-- disposal); the PR -> PO -> GRN -> Vendor Invoice steps before
-- capitalisation reuse the existing Procurement flow (PurchaseOrder/GRN),
-- not duplicated here.
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('Asset', 'Finance', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Asset', 'code', 'Asset Number', 'Data', TRUE, NULL, 1),
('Asset', 'category', 'Category', 'Data', FALSE, NULL, 2),
('Asset', 'serial_number', 'Serial Number', 'Data', FALSE, NULL, 3),
('Asset', 'vendor', 'Vendor', 'Data', FALSE, NULL, 4),
('Asset', 'invoice_number', 'Invoice Number', 'Data', FALSE, NULL, 5),
('Asset', 'acquisition_date', 'Acquisition Date', 'Date', TRUE, NULL, 6),
('Asset', 'capitalisation_date', 'Capitalisation Date', 'Date', FALSE, NULL, 7),
('Asset', 'cost', 'Cost', 'Number', TRUE, NULL, 8),
('Asset', 'location', 'Location', 'Data', TRUE, NULL, 9),
('Asset', 'custodian', 'Custodian', 'Data', FALSE, NULL, 10),
('Asset', 'useful_life_years', 'Useful Life (years)', 'Number', TRUE, NULL, 11),
('Asset', 'disposal_type', 'Disposal Type', 'Select', FALSE, 'Sale,Scrap,WriteOff', 12),
('Asset', 'status', 'Status', 'Select', TRUE, 'Draft,Capitalised,Disposed', 13)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Asset', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Asset', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- New Chart of Accounts entries for the asset lifecycle's GL postings.
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('1400', 'Fixed Assets Account', 'Asset'),
('5300', 'Loss on Asset Disposal', 'Expense')
ON CONFLICT (account_code) DO NOTHING;

-- 28. Expense Management (Stage 13.13c) - MB Sec.16.2. Manager Approval
-- reuses the existing Approval/Workflow Engine (Stage 13.8's amount-slab
-- routing + maker-checker) rather than a separate ad-hoc mechanism -
-- ExpenseClaim -> Pending Approval -> Approved covers "Manager Approval"
-- and the spec's "amount limit"-driven "approval workflow" control in one
-- step. Finance Verification and Payment are added as two further linear
-- stages on top (engines/expense.go), since those aren't amount-routed
-- decisions, just sequential finance processing.
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('ExpenseClaim', 'Finance', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ExpenseClaim', 'code', 'Claim Number', 'Data', TRUE, NULL, 1),
('ExpenseClaim', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
('ExpenseClaim', 'department', 'Department', 'Data', FALSE, NULL, 3),
('ExpenseClaim', 'location', 'Location', 'Data', TRUE, NULL, 4),
('ExpenseClaim', 'expense_date', 'Expense Date', 'Date', TRUE, NULL, 5),
('ExpenseClaim', 'category', 'Category', 'Select', TRUE, 'Conveyance,Travel,Food,Hotel,Fuel,Repair,Medical,Marketing,StoreExpense,Other', 6),
('ExpenseClaim', 'amount', 'Amount', 'Number', TRUE, NULL, 7),
('ExpenseClaim', 'gst_amount', 'GST Amount', 'Number', FALSE, NULL, 8),
('ExpenseClaim', 'purpose', 'Purpose', 'Data', FALSE, NULL, 9),
('ExpenseClaim', 'advance_adjusted', 'Advance Adjusted', 'Number', FALSE, NULL, 10),
('ExpenseClaim', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Verified,Paid', 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Same routing pattern as PurchaseOrder (Stage 13.8): Store Manager
-- approves smaller claims, HR/Admin approves larger ones.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('ExpenseClaim', 0, 4999, 'Store Manager'),
('ExpenseClaim', 5000, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;

-- Cashier gets allow_update=TRUE (not just create) so they can edit and
-- submit their own Draft claims - "submit for approval" (Stage 13.8's
-- handleSubmitApproval) checks "update" permission as its gate, so without
-- this a Cashier could create a claim but never actually submit it, which
-- defeats the point of letting them file one in the first place.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ExpenseClaim', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ExpenseClaim', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'ExpenseClaim', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- New Chart of Accounts entries for expense GL postings. GST Input Credit
-- is an Asset (input tax the business can claim back), not an Expense.
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('1500', 'GST Input Credit Account', 'Asset'),
('5400', 'Employee Expense Account', 'Expense')
ON CONFLICT (account_code) DO NOTHING;

-- 29. CRM/Loyalty (Stage 13.13d, scoped MVP) - CRM/Loyalty add-on blueprint
-- Sec.3.4/3.5. Append-only ledger per the blueprint's own design rule
-- ("Do not directly edit point balance") - balance is always SUM(Earn) -
-- SUM(Burn) from this table, never a stored/editable field.
CREATE TABLE IF NOT EXISTS tenant_default.loyalty_point_ledger (
    id SERIAL PRIMARY KEY,
    customer_id VARCHAR(100) NOT NULL,
    transaction_type VARCHAR(10) NOT NULL, -- Earn, Burn
    points INT NOT NULL,
    reference_doctype VARCHAR(100),
    reference_id VARCHAR(100),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_loyalty_ledger_customer ON tenant_default.loyalty_point_ledger (customer_id);

-- 30. Manufacturing (Stage 13.13e, scoped MVP) - Manufacturing add-on
-- blueprint Sec.7.2/7.3, single-level BOM + linear Production Order only.
INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES
('BOM', 'Manufacturing', 'Master'),
('ProductionOrder', 'Manufacturing', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('BOM', 'code', 'BOM Code', 'Data', TRUE, NULL, 1),
('BOM', 'parent_item', 'Parent Item (Finished Good SKU)', 'Data', TRUE, NULL, 2),
('BOM', 'components', 'Components JSON ([{sku, qty}])', 'Data', TRUE, NULL, 3),
('BOM', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductionOrder', 'code', 'Production Order Number', 'Data', TRUE, NULL, 1),
('ProductionOrder', 'bom_id', 'BOM', 'Link', TRUE, 'BOM', 2),
('ProductionOrder', 'quantity', 'Quantity to Produce', 'Number', TRUE, NULL, 3),
('ProductionOrder', 'location', 'Location', 'Data', TRUE, NULL, 4),
('ProductionOrder', 'status', 'Status', 'Select', TRUE, 'Draft,Material Issued,Completed', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'BOM', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'ProductionOrder', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'BOM', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'ProductionOrder', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 31. PIM Foundation MVP (Stage 15) - PIM Module Developer Blueprint v1.0.
-- Scoped to Family/Attribute framework + Parent/Variant grouping on the
-- existing Item doctype + Content enrichment with maker-checker approval +
-- a Completeness Scoring engine (engines/pim.go). Media Library and Channel
-- Mapping/Publishing are deliberately out of scope for this phase (see
-- docs/micro_checklist.md Stage 15 note). Item stays the single source of
-- product identity/tax/price/barcode per the blueprint's core rule - PIM
-- never becomes a parallel product master, it only adds enrichment
-- doctypes plus 3 optional linking fields onto Item.
--
-- Module-governance tables below are CREATE TABLE IF NOT EXISTS so PIM
-- doesn't depend on db/migrations_stage14a_modules.sql (which introduces
-- the same public.modules/module_entitlements/doctype_meta.module_key
-- objects but, as of this writing, isn't yet wired into README.md/ci.yml's
-- migration apply list) - safe to run whether or not that file also lands.
CREATE TABLE IF NOT EXISTS public.modules (
    module_key VARCHAR(100) PRIMARY KEY,
    display_name VARCHAR(255) NOT NULL,
    description TEXT,
    is_core BOOLEAN DEFAULT FALSE,
    default_enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tenant_default.module_entitlements (
    module_key VARCHAR(100) PRIMARY KEY,
    enabled BOOLEAN DEFAULT TRUE,
    granted_by VARCHAR(100),
    granted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    note TEXT
);

ALTER TABLE tenant_default.doctype_meta ADD COLUMN IF NOT EXISTS module_key VARCHAR(100);

INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('pim', 'Product Information Management', 'Product family/attribute framework, content enrichment with approval, completeness scoring', FALSE, TRUE)
ON CONFLICT (module_key) DO NOTHING;

INSERT INTO tenant_default.module_entitlements (module_key, enabled, granted_by, note) VALUES
('pim', TRUE, 'system', NULL)
ON CONFLICT (module_key) DO NOTHING;

-- ProductFamily: controls which attributes are required for a given
-- industry vertical (jewellery sees polish/purity, electronics sees
-- warranty/voltage) - see ProductFamilyAttribute below for the mapping.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ProductFamily', 'PIM', 'pim', 'Master'),
('ProductAttributeDef', 'PIM', 'pim', 'Master'),
('ProductFamilyAttribute', 'PIM', 'pim', 'Master'),
('ProductAttributeValue', 'PIM', 'pim', 'Transaction'),
('ProductContent', 'PIM', 'pim', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductFamily', 'code', 'Family Code', 'Data', TRUE, NULL, 1),
('ProductFamily', 'name', 'Family Name', 'Data', TRUE, NULL, 2),
('ProductFamily', 'description', 'Description', 'Data', FALSE, NULL, 3),
('ProductFamily', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductAttributeDef', 'code', 'Attribute Code', 'Data', TRUE, NULL, 1),
('ProductAttributeDef', 'label', 'Label', 'Data', TRUE, NULL, 2),
('ProductAttributeDef', 'value_type', 'Value Type', 'Select', TRUE, 'Data,Number,Select,Date', 3),
('ProductAttributeDef', 'value_options', 'Select Options (if Value Type=Select)', 'Data', FALSE, NULL, 4),
('ProductAttributeDef', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductFamilyAttribute', 'code', 'Code', 'Data', TRUE, NULL, 1),
('ProductFamilyAttribute', 'family', 'Family', 'Link', TRUE, 'ProductFamily', 2),
('ProductFamilyAttribute', 'attribute', 'Attribute', 'Link', TRUE, 'ProductAttributeDef', 3),
('ProductFamilyAttribute', 'mandatory', 'Mandatory for Completeness', 'Select', TRUE, 'Yes,No', 4),
('ProductFamilyAttribute', 'display_order', 'Display Order', 'Number', FALSE, NULL, 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ProductAttributeValue id convention: "<item_code>::<attribute_code>" -
-- makes "one value per item+attribute" self-enforcing via the generic doc
-- handler's INSERT...ON CONFLICT(id) DO UPDATE upsert, no new dedup code.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductAttributeValue', 'code', 'Code', 'Data', TRUE, NULL, 1),
('ProductAttributeValue', 'item', 'Item', 'Link', TRUE, 'Item', 2),
('ProductAttributeValue', 'attribute', 'Attribute', 'Link', TRUE, 'ProductAttributeDef', 3),
('ProductAttributeValue', 'value', 'Value', 'Data', TRUE, NULL, 4),
('ProductAttributeValue', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ProductContent id convention: "<item_code>::<language>" - same
-- self-enforcing upsert trick as ProductAttributeValue above. Approval-
-- gated below (single amount-agnostic slab) so it reuses the existing
-- Approval/Workflow Engine (Stage 13.8, engines/approval.go) as-is - zero
-- new approval code, same pattern Expense Management (Stage 13.13c) used.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductContent', 'code', 'Code', 'Data', TRUE, NULL, 1),
('ProductContent', 'product_id', 'Product (Item)', 'Link', TRUE, 'Item', 2),
('ProductContent', 'language', 'Language', 'Data', TRUE, NULL, 3),
('ProductContent', 'title', 'Title', 'Data', TRUE, NULL, 4),
('ProductContent', 'short_desc', 'Short Description', 'Data', FALSE, NULL, 5),
('ProductContent', 'long_desc', 'Long Description', 'Data', FALSE, NULL, 6),
('ProductContent', 'seo_title', 'SEO Title', 'Data', FALSE, NULL, 7),
('ProductContent', 'tags', 'Tags (comma-separated)', 'Data', FALSE, NULL, 8),
('ProductContent', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- New optional fields on the existing Item doctype: family linkage +
-- parent/variant grouping. Deliberately NOT a new physical product table -
-- a variant Item just sets parent_product_code to its parent Item's code,
-- per the blueprint's explicit rule that PIM must never become a parallel
-- product master. variant_option_values uses a simple "Key:Value;Key:Value"
-- shorthand (e.g. "Color:Red;Size:M") for easy manual entry; uniqueness of
-- the combination within a parent is enforced in engines.pim.go, hooked
-- into main.go's handleGenericDoc (mirrors the existing ExpenseClaim
-- validation hook, Stage 13.13c) since nothing like this exists for Item
-- today.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'family', 'Product Family', 'Link', FALSE, 'ProductFamily', 9),
('Item', 'parent_product_code', 'Parent Product Code', 'Data', FALSE, NULL, 10),
('Item', 'variant_option_values', 'Variant Options (Key:Value;Key:Value)', 'Data', FALSE, NULL, 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('ProductContent', 0, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;

-- HR/Admin: full control over the taxonomy (Family/AttributeDef/
-- FamilyAttribute) and content. Store Manager: read-only on taxonomy
-- (shouldn't edit config store-side), maker (create/read/update, no
-- delete) on AttributeValue/Content - same "maker can submit, checker
-- approves" shape as ExpenseClaim (Stage 13.13c). allow_update=TRUE is
-- required for Store Manager to actually submit content for approval,
-- since handleSubmitApproval gates on "update" permission - the exact gap
-- Stage 13.13c hit and fixed for Cashier/ExpenseClaim, pre-empted here.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ProductFamily', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'ProductAttributeDef', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'ProductFamilyAttribute', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'ProductAttributeValue', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'ProductContent', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ProductFamily', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'ProductAttributeDef', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'ProductFamilyAttribute', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'ProductAttributeValue', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'ProductContent', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 32. PIM V2 Alignment: Media, Channel Publishing, Import/Export (Stage
-- 15.2, PIM Module Developer Blueprint V2 - Repo-Enhanced). Rides the
-- existing module_key='pim' registered in section 31 - no new module.
-- PIMProductProfile is a write-through cache/derived-status doctype (see
-- engines/pim.go's CalculateCompleteness) - it is never directly
-- created/updated by a user, so both roles below get read-only.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PIMProductProfile', 'PIM', 'pim', 'Transaction'),
('ProductMedia', 'PIM', 'pim', 'Transaction'),
('Channel', 'PIM', 'pim', 'Master'),
('ChannelCategoryMap', 'PIM', 'pim', 'Master'),
('ChannelFieldMap', 'PIM', 'pim', 'Master'),
('ImportJob', 'PIM', 'pim', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMProductProfile', 'code', 'Code', 'Data', TRUE, NULL, 1),
('PIMProductProfile', 'product_id', 'Product (Item)', 'Link', TRUE, 'Item', 2),
('PIMProductProfile', 'enrichment_status', 'Enrichment Status', 'Select', TRUE, 'Draft,Enrichment In Progress,Pending Approval,Approved,Ready to Publish,Published,Publish Failed,Archived', 3),
('PIMProductProfile', 'completeness_score', 'Completeness Score', 'Number', FALSE, NULL, 4),
('PIMProductProfile', 'missing_fields_json', 'Missing Fields (JSON)', 'Data', FALSE, NULL, 5),
('PIMProductProfile', 'last_scored_at', 'Last Scored At', 'Data', FALSE, NULL, 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductMedia', 'code', 'Code', 'Data', TRUE, NULL, 1),
('ProductMedia', 'item', 'Item', 'Link', TRUE, 'Item', 2),
('ProductMedia', 'media_role', 'Media Role', 'Select', TRUE, 'Main Image,Gallery,Variant Image,Lifestyle,Certificate,Internal QC,Video/Other', 3),
('ProductMedia', 'file_path', 'File Path', 'Data', TRUE, NULL, 4),
('ProductMedia', 'file_type', 'File Type', 'Data', TRUE, NULL, 5),
('ProductMedia', 'checksum', 'Checksum', 'Data', TRUE, NULL, 6),
('ProductMedia', 'width', 'Width (px)', 'Number', FALSE, NULL, 7),
('ProductMedia', 'height', 'Height (px)', 'Number', FALSE, NULL, 8),
('ProductMedia', 'version_no', 'Version', 'Number', FALSE, NULL, 9),
('ProductMedia', 'sort_order', 'Sort Order', 'Number', FALSE, NULL, 10),
('ProductMedia', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Channel', 'code', 'Channel Code', 'Data', TRUE, NULL, 1),
('Channel', 'name', 'Channel Name', 'Data', TRUE, NULL, 2),
('Channel', 'channel_type', 'Channel Type', 'Select', TRUE, 'Website,Marketplace,OMS,Middleware', 3),
('Channel', 'default_locale', 'Default Locale', 'Data', FALSE, NULL, 4),
('Channel', 'default_currency', 'Default Currency', 'Data', FALSE, NULL, 5),
('Channel', 'enabled', 'Enabled', 'Select', TRUE, 'Yes,No', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ChannelCategoryMap', 'code', 'Code', 'Data', TRUE, NULL, 1),
('ChannelCategoryMap', 'channel', 'Channel', 'Link', TRUE, 'Channel', 2),
('ChannelCategoryMap', 'erp_category', 'ERP Category', 'Data', TRUE, NULL, 3),
('ChannelCategoryMap', 'channel_category', 'Channel Category', 'Data', TRUE, NULL, 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ChannelFieldMap.mandatory drives channel-scoped completeness (Decision 3,
-- session plan) - a field optional in ERP/PIM core can still block
-- publish-readiness for a specific channel here.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ChannelFieldMap', 'code', 'Code', 'Data', TRUE, NULL, 1),
('ChannelFieldMap', 'channel', 'Channel', 'Link', TRUE, 'Channel', 2),
('ChannelFieldMap', 'source_field', 'Source Field (ERP/PIM)', 'Data', TRUE, NULL, 3),
('ChannelFieldMap', 'target_field', 'Target Field (Channel)', 'Data', TRUE, NULL, 4),
('ChannelFieldMap', 'mandatory', 'Mandatory for Publish', 'Select', TRUE, 'Yes,No', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ImportJob', 'code', 'Code', 'Data', TRUE, NULL, 1),
('ImportJob', 'doctype_name', 'Doctype', 'Data', TRUE, NULL, 2),
('ImportJob', 'status', 'Status', 'Select', TRUE, 'Completed,Failed', 3),
('ImportJob', 'total_rows', 'Total Rows', 'Number', TRUE, NULL, 4),
('ImportJob', 'success_rows', 'Success Rows', 'Number', TRUE, NULL, 5),
('ImportJob', 'failed_rows', 'Failed Rows', 'Number', TRUE, NULL, 6),
('ImportJob', 'error_csv', 'Error CSV (row-level failures)', 'Data', FALSE, NULL, 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PIMProductProfile', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'PIMProductProfile', TRUE, FALSE, FALSE, FALSE),
('HR/Admin', 'ProductMedia', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ProductMedia', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'Channel', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Channel', TRUE, FALSE, FALSE, FALSE),
('HR/Admin', 'ChannelCategoryMap', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ChannelCategoryMap', TRUE, FALSE, FALSE, FALSE),
('HR/Admin', 'ChannelFieldMap', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ChannelFieldMap', TRUE, FALSE, FALSE, FALSE),
('HR/Admin', 'ImportJob', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ImportJob', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- System-generated publish job queue/log - dedicated SQL tables, not
-- doctypes, same reasoning as sticker_print_log/approval_log (Stage
-- 13.8/13.15): these are written by the background worker
-- (engines.StartPublishQueueWorker), not authored by a user.
CREATE TABLE IF NOT EXISTS tenant_default.pim_publish_queue (
    job_id SERIAL PRIMARY KEY,
    item_code VARCHAR(100) NOT NULL,
    channel_code VARCHAR(100) NOT NULL,
    payload_hash VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'Queued', -- Queued, Processing, Published, Failed
    retry_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_pim_publish_queue_status ON tenant_default.pim_publish_queue (status);

CREATE TABLE IF NOT EXISTS tenant_default.pim_publish_log (
    id SERIAL PRIMARY KEY,
    job_id INT NOT NULL,
    item_code VARCHAR(100) NOT NULL,
    channel_code VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL,
    external_id VARCHAR(150),
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_pim_publish_log_item ON tenant_default.pim_publish_log (item_code);
