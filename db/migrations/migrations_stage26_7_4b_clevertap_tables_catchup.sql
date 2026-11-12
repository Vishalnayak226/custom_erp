-- Catch-up fix: Stage 9.1's CleverTap tables (db/migration.sql Section 36)
-- were added to the base schema file after this dev DB was first
-- provisioned, so they were never actually created here - discovered while
-- building Stage 26.7.4 (Campaign), which is the first feature to actually
-- exercise engines/clevertap.go's LogCleverTapEvent against a real DB.
-- Identical definition to migration.sql's own, just applied as a catch-up
-- for any already-provisioned tenant schema that predates that section.
CREATE TABLE IF NOT EXISTS tenant_default.clevertap_credentials (
    account_id VARCHAR(100) PRIMARY KEY,
    passcode VARCHAR(255) NOT NULL,
    region VARCHAR(50) DEFAULT 'in1',
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tenant_default.clevertap_event_log (
    id SERIAL PRIMARY KEY,
    event_name VARCHAR(200) NOT NULL,
    customer_id VARCHAR(200) NOT NULL,
    event_data JSONB,
    status VARCHAR(50) DEFAULT 'Pending',
    sent_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
