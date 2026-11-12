-- Stage 49.2.1 (recertification): a periodic access review. An administrator
-- confirms or revokes each user's role and stores; every decision is a row.
--
-- access_reviews is append-only in practice (the app only inserts): who decided,
-- when, what the user held at that moment (role, home store, store access) and
-- the outcome. The review is due again after security.access_review_days
-- (Off by default).
--
-- Additive: one new tenant table, no existing row touched, no client data written.
CREATE TABLE IF NOT EXISTS tenant_default.access_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(100) NOT NULL,
    username VARCHAR(100) NOT NULL DEFAULT '',
    role VARCHAR(100) NOT NULL DEFAULT '',
    location_code VARCHAR(100) NOT NULL DEFAULT '',
    store_access TEXT NOT NULL DEFAULT '',
    decision VARCHAR(20) NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    decided_by VARCHAR(100) NOT NULL DEFAULT '',
    decided_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT access_reviews_decision_check CHECK (decision IN ('Confirmed', 'Revoked'))
);

CREATE INDEX IF NOT EXISTS idx_access_reviews_user ON tenant_default.access_reviews (user_id, decided_at DESC);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.access_reviews (LIKE tenant_default.access_reviews INCLUDING ALL)', schema_rec.schema_name);
  END LOOP;
END $$;
