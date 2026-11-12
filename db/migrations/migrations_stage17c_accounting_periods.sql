-- Stage 17.4: admin-managed accounting periods with a one-way close control.
-- PostDoubleEntry rejects any posting while CURRENT_DATE falls inside a
-- Closed period; corrections happen via a new reversal posting dated in the
-- current open period, never by mutating a historical gl_postings row.
CREATE TABLE IF NOT EXISTS tenant_default.accounting_periods (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    period_name VARCHAR(100) NOT NULL,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'Open' CHECK (status IN ('Open', 'Closed')),
    closed_by VARCHAR(100),
    closed_at TIMESTAMP,
    created_by VARCHAR(100) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (end_date >= start_date)
);

CREATE INDEX IF NOT EXISTS idx_accounting_periods_closed_range
    ON tenant_default.accounting_periods (start_date, end_date)
    WHERE status = 'Closed';
