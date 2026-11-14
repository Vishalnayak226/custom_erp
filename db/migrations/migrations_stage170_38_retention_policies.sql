-- ---------------------------------------------------------------------------
-- Stage 170.38: retention per record type. One row per record type (and, for
-- a sweep that clears one personal field, that field): how long it is kept,
-- then what happens to it (clear field / purge / anonymise), and the legal
-- basis. The seeded rows are exactly the windows the code used before:
--   GatePass.driver_phone   90 days   clear field
--   audit_logs source_ip    180 days  clear field (CERT-In floor)
--   users.last_sign_in_ip   180 days  clear field (CERT-In floor)
-- plus Customer / Employee / Vendor with no wait before an erasure request
-- (today's behaviour). A tenant with no row falls back to the same values in
-- code, so nothing changes until an administrator edits a row.
-- Additive only; safe to run twice.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.retention_policies (
        record_type  VARCHAR(100) NOT NULL,
        field_name   VARCHAR(100) NOT NULL DEFAULT '',
        keep_for     INTEGER      NOT NULL DEFAULT 0,
        keep_unit    VARCHAR(10)  NOT NULL DEFAULT 'days',
        then_action  VARCHAR(20)  NOT NULL DEFAULT 'clear field',
        legal_basis  TEXT         NOT NULL DEFAULT '',
        note         TEXT         NOT NULL DEFAULT '',
        updated_by   VARCHAR(100) NOT NULL DEFAULT 'system',
        updated_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (record_type, field_name)
      )$f$, schema_rec.schema_name);
    EXECUTE format($f$
      INSERT INTO %I.retention_policies (record_type, field_name, keep_for, keep_unit, then_action, legal_basis, note) VALUES
        ('GatePass',   'driver_phone',   90,  'days', 'clear field', 'Reach the driver about the dispatch', 'Cleared 90 days after the gate pass is made'),
        ('audit_logs', 'source_ip',      180, 'days', 'clear field', 'Statutory: CERT-In log directions', 'Address and device of a security event; never below 180 days'),
        ('users',      'last_sign_in_ip',180, 'days', 'clear field', 'Statutory: CERT-In log directions', 'Never below 180 days'),
        ('Customer',   '',               0,   'months', 'anonymise', 'Consent or account closure', 'Erased on request; 0 = no wait'),
        ('Employee',   '',               0,   'months', 'anonymise', 'Employment', 'After the employment end date; 0 = no wait. Payroll and tax records are kept.'),
        ('Vendor',     '',               0,   'months', 'anonymise', 'Vendor relationship', 'After the vendor is made Inactive; 0 = no wait. Tax and ledger records are kept.')
      ON CONFLICT (record_type, field_name) DO NOTHING$f$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
