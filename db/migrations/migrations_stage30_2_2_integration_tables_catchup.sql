-- Stage 30.2.2 (docs/UX_MANUAL_AUDIT.md): catch-up for Stage 9.1's Unicommerce
-- (db/migration.sql Section 34) and Pine Labs (Section 35) tables. Both
-- sections were added to the base schema file after this database was first
-- provisioned, and migration.sql is only ever run to create a database from
-- scratch - so every already-existing database is missing all five tables.
--
-- Consequence found live: six endpoints return HTTP 500 outright, and the
-- Pine Labs background reconciliation worker retries against
-- pinelabs_transactions roughly every 8 minutes, filling system_error_logs
-- with the same relation-does-not-exist error forever.
--
-- Exactly the same class of drift as
-- db/migrations_stage26_7_4b_clevertap_tables_catchup.sql, and fixed the same
-- way: identical definitions to migration.sql's own, applied as a catch-up.
-- The general fix for the drift itself - so a table added to migration.sql
-- can't silently miss existing databases again - is the migration runner
-- added alongside this file (db/migrate.go, `erp-server -migrate`).

CREATE TABLE IF NOT EXISTS tenant_default.unicommerce_credentials (
    store_code VARCHAR(100) PRIMARY KEY,
    api_key VARCHAR(255) NOT NULL,
    api_secret VARCHAR(255) NOT NULL,
    base_url VARCHAR(255) NOT NULL,
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tenant_default.unicommerce_inventory_sync (
    sku VARCHAR(100) NOT NULL,
    store_code VARCHAR(100) NOT NULL,
    quantity INT NOT NULL DEFAULT 0,
    last_synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (sku, store_code)
);

CREATE TABLE IF NOT EXISTS tenant_default.unicommerce_order_mapping (
    order_id VARCHAR(200) PRIMARY KEY,
    channel_order_id VARCHAR(200) NOT NULL,
    store_code VARCHAR(100) NOT NULL,
    status VARCHAR(50) DEFAULT 'Imported',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tenant_default.pinelabs_credentials (
    terminal_id VARCHAR(100) PRIMARY KEY,
    api_key VARCHAR(255) NOT NULL,
    merchant_id VARCHAR(255) NOT NULL,
    base_url VARCHAR(255) NOT NULL,
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tenant_default.pinelabs_transactions (
    id SERIAL PRIMARY KEY,
    transaction_id VARCHAR(200) NOT NULL UNIQUE,
    terminal_id VARCHAR(100) NOT NULL,
    cart_number VARCHAR(200),
    amount NUMERIC(12,2) NOT NULL,
    status VARCHAR(50) DEFAULT 'Completed',
    payment_mode VARCHAR(50) DEFAULT 'Card',
    reconciled BOOLEAN DEFAULT FALSE,
    reconciled_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Every other already-provisioned tenant schema needs them too - a second
-- tenant provisioned before this fix has the same five holes tenant_default
-- did. New tenants are unaffected (ProvisionTenantSchema clones the shape of
-- tenant_default at provisioning time).
-- The DDL is repeated verbatim per schema rather than cloned with
-- `LIKE ... INCLUDING ALL`: INCLUDING DEFAULTS would copy
-- pinelabs_transactions.id's default as a reference to tenant_default's OWN
-- sequence, so every tenant would silently share one id counter.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.unicommerce_credentials (store_code VARCHAR(100) PRIMARY KEY, api_key VARCHAR(255) NOT NULL, api_secret VARCHAR(255) NOT NULL, base_url VARCHAR(255) NOT NULL, active BOOLEAN DEFAULT TRUE, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)', schema_rec.schema_name);
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.unicommerce_inventory_sync (sku VARCHAR(100) NOT NULL, store_code VARCHAR(100) NOT NULL, quantity INT NOT NULL DEFAULT 0, last_synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (sku, store_code))', schema_rec.schema_name);
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.unicommerce_order_mapping (order_id VARCHAR(200) PRIMARY KEY, channel_order_id VARCHAR(200) NOT NULL, store_code VARCHAR(100) NOT NULL, status VARCHAR(50) DEFAULT ''Imported'', created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)', schema_rec.schema_name);
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.pinelabs_credentials (terminal_id VARCHAR(100) PRIMARY KEY, api_key VARCHAR(255) NOT NULL, merchant_id VARCHAR(255) NOT NULL, base_url VARCHAR(255) NOT NULL, active BOOLEAN DEFAULT TRUE, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)', schema_rec.schema_name);
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.pinelabs_transactions (id SERIAL PRIMARY KEY, transaction_id VARCHAR(200) NOT NULL UNIQUE, terminal_id VARCHAR(100) NOT NULL, cart_number VARCHAR(200), amount NUMERIC(12,2) NOT NULL, status VARCHAR(50) DEFAULT ''Completed'', payment_mode VARCHAR(50) DEFAULT ''Card'', reconciled BOOLEAN DEFAULT FALSE, reconciled_at TIMESTAMP, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)', schema_rec.schema_name);
  END LOOP;
END $$;
