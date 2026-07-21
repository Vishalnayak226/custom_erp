-- Stage 24: Security & Loopholes Hardening. Additive-only, matches this
-- repo's own established migration convention (ALTER TABLE ... ADD COLUMN
-- IF NOT EXISTS / CREATE INDEX IF NOT EXISTS / INSERT ... ON CONFLICT DO
-- NOTHING throughout) - nothing here is a destructive rework of an existing
-- table. See docs/ERP_LOOPHOLES_ANALYSIS.md and micro_checklist.md Stage 24
-- for the source findings each block below closes.

-- 24.1: real per-user location code. handlers_auth.go's login previously
-- hard-coded every session's location claim to "HO" regardless of the
-- user's actual assignment, silently defeating every location-scoped
-- authorization check in handleGenericDoc. Defaults every existing row to
-- 'HO' too - the exact behavior every user had before this column existed,
-- so nothing changes until an HR/Admin explicitly assigns a real location.
ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS location_code VARCHAR(50) NOT NULL DEFAULT 'HO';

-- 24.5: idempotency key for financial postings. Not UNIQUE (one
-- PostDoubleEntry call inserts multiple gl_postings rows - one per debit
-- account, one per credit account - all sharing the same key), just
-- indexed for the existence check PostDoubleEntry now runs before inserting.
ALTER TABLE tenant_default.gl_postings ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(255);
CREATE INDEX IF NOT EXISTS idx_gl_postings_idempotency_key ON tenant_default.gl_postings (idempotency_key) WHERE idempotency_key IS NOT NULL;

-- 24.7: CreateReservation's ATS read gained a FOR UPDATE lock (code change,
-- engines/inventory.go); this is the "missing index" half of the same
-- finding (#16 + #18) - inventory_reservation.expires_at has no index today
-- despite being the predicate the cleanup-expired-reservations sweep
-- filters on.
CREATE INDEX IF NOT EXISTS idx_inventory_reservation_expires_at ON tenant_default.inventory_reservation (expires_at);

-- 24.10: optimistic locking for the generic doc-engine's update path. A
-- caller can now pass expected_version in its update payload to get a real
-- conflict (409) instead of a silent last-write-wins overwrite; omitting it
-- (every existing caller today) preserves the exact old behavior.
ALTER TABLE tenant_default.documents ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;

-- 24.11: vendor invoice payment override now routes through the maker-checker
-- approval engine instead of paying unilaterally on any non-empty reason
-- string. Any amount requires HR/Admin - overriding a 3-way-match mismatch
-- is itself the high-risk action here, not the rupee amount being overridden
-- (unlike PurchaseOrder's amount-tiered slabs).
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('VendorInvoice', 0, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;

-- 24.24: audit-log tamper-evidence hash chain. NULL/empty for every row
-- written before this migration - engines.VerifyAuditLogChain's walk treats
-- an empty stored checksum as "not yet checksummed" rather than a break.
ALTER TABLE tenant_default.audit_logs ADD COLUMN IF NOT EXISTS checksum VARCHAR(64);
