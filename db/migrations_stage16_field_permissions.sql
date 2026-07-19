-- Stage 16.7: tenant-configurable field-level read/write permissions.
CREATE TABLE IF NOT EXISTS tenant_default.field_permissions (
    role VARCHAR(100) NOT NULL,
    doctype_name VARCHAR(100) NOT NULL,
    fieldname VARCHAR(100) NOT NULL,
    allow_read BOOLEAN NOT NULL DEFAULT TRUE,
    allow_write BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (role, doctype_name, fieldname)
);
-- Demonstration policy: Cashiers do not see or edit item cost/GST fields.
INSERT INTO tenant_default.field_permissions (role, doctype_name, fieldname, allow_read, allow_write) VALUES
('Cashier', 'Item', 'cost_price', FALSE, FALSE),
('Cashier', 'Item', 'gst_rate', FALSE, FALSE)
ON CONFLICT (role, doctype_name, fieldname) DO NOTHING;
