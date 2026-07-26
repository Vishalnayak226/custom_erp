-- Stage 26.6.5: Payment-file (bank-file) generation for a payment-proposal
-- batch + duplicate-UTR check. A real UTR (Unique Transaction Reference)
-- only exists once the bank has actually processed a transfer - this ERP
-- has no real bank API integration (same credentials-gated-later precedent
-- as 26.2.x), so UTRs are recorded after the fact (manually, or from a
-- future bank-statement import), not generated here. A dedicated table
-- with a UNIQUE constraint on utr is the duplicate-UTR check itself -
-- DB-enforced, not just app-checked, so it holds even under a race.
CREATE TABLE IF NOT EXISTS tenant_default.payment_utr_log (
    id SERIAL PRIMARY KEY,
    proposal_id VARCHAR(100) NOT NULL,
    invoice_id VARCHAR(100) NOT NULL,
    utr VARCHAR(50) NOT NULL UNIQUE,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
