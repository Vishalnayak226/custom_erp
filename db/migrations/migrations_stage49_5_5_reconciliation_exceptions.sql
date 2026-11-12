-- Stage 49.5.5: exceptions on the order -> shipment -> invoice -> payment
-- reconciliation. A mismatch is dismissed only with a note (evidence); the
-- figures at the time are kept so the dismissal lapses when they change.
-- Additive. Latest row per (mismatch_kind, ref_key) wins.
CREATE TABLE IF NOT EXISTS tenant_default.reconciliation_exceptions (
    id BIGSERIAL PRIMARY KEY,
    mismatch_kind VARCHAR(60) NOT NULL,
    ref_key VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL,
    fingerprint TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL CHECK (btrim(note) <> ''),
    attachment TEXT NOT NULL DEFAULT '',
    changed_by VARCHAR(100) NOT NULL,
    changed_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reconciliation_exceptions_key
  ON tenant_default.reconciliation_exceptions (mismatch_kind, ref_key, id DESC);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.reconciliation_exceptions (LIKE tenant_default.reconciliation_exceptions INCLUDING ALL)', schema_rec.schema_name);
  END LOOP;
END $$;
