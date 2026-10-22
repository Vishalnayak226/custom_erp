-- ---------------------------------------------------------------------------
-- Stage 102 (user, 2026-10-22: "Stage 102 store app: about 10 left. Build
-- all") - the store app's push alerts, the shared phone, and labels asked
-- from the phone.
--
--   * web_push_vapid: the client's one Web Push key pair (102.1.3). The
--     private key is kept only encrypted (engines/web_push.go, its own
--     keyring); the public key is what a phone subscribes with.
--   * web_push_subscriptions: one row per phone and user that turned alerts
--     on - the push service's endpoint and the phone's keys, the store it
--     alerts for, and which alerts it wants.
--   * web_push_seen: what was already alerted, so an approval or a low stock
--     line is pushed once. scope is 'user:<id>' or 'store:<code>'.
--   * store_app_devices: phones a manager registered to a store (102.1.4).
--     Only the SHA-256 of the phone's token is kept; the phone holds it.
--     removed_at ends every PIN session the phone issued (the middleware
--     checks the session's device on each call).
--   * store_app_pins: a person's store app PIN (bcrypt, like passwords) and
--     its own lockout, apart from the password's.
--   * StoreLabelRequest: labels asked for from the phone (102.3.4). A
--     transaction with item lines, so Sticker Printing > Print from
--     Transaction on the store's computer loads and prints it through the
--     printer routing it already uses (Stage 98.4 / 127.5). Engine-written.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  s RECORD;
BEGIN
  FOR s IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.users', s.schema_name)) IS NULL;

    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.web_push_vapid (
        id              SMALLINT     PRIMARY KEY DEFAULT 1 CHECK (id = 1),
        public_key      TEXT         NOT NULL,
        private_key_enc BYTEA        NOT NULL,
        created_at      TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);

    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.web_push_subscriptions (
        id              BIGSERIAL    PRIMARY KEY,
        user_id         VARCHAR(100) NOT NULL,
        endpoint        TEXT         NOT NULL UNIQUE,
        p256dh          TEXT         NOT NULL,
        auth            TEXT         NOT NULL,
        store           VARCHAR(100) NOT NULL DEFAULT '',
        alert_approvals BOOLEAN      NOT NULL DEFAULT TRUE,
        alert_low_stock BOOLEAN      NOT NULL DEFAULT TRUE,
        user_agent      VARCHAR(300),
        created_at      TIMESTAMP    NOT NULL DEFAULT NOW(),
        last_sent_at    TIMESTAMP,
        failures        INTEGER      NOT NULL DEFAULT 0,
        last_error      TEXT
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS web_push_subscriptions_user_idx ON %I.web_push_subscriptions (user_id)', s.schema_name);

    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.web_push_seen (
        scope      VARCHAR(120) NOT NULL,
        ref_key    VARCHAR(300) NOT NULL,
        created_at TIMESTAMP    NOT NULL DEFAULT NOW(),
        PRIMARY KEY (scope, ref_key)
      )$f$, s.schema_name);

    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.store_app_devices (
        id            VARCHAR(40)  PRIMARY KEY,
        store         VARCHAR(100) NOT NULL,
        label         VARCHAR(160) NOT NULL DEFAULT '',
        token_hash    VARCHAR(64)  NOT NULL UNIQUE,
        registered_by VARCHAR(100) NOT NULL,
        registered_at TIMESTAMP    NOT NULL DEFAULT NOW(),
        last_used_at  TIMESTAMP,
        last_user     VARCHAR(100),
        removed_at    TIMESTAMP,
        removed_by    VARCHAR(100)
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS store_app_devices_store_idx ON %I.store_app_devices (store)', s.schema_name);

    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.store_app_pins (
        user_id      VARCHAR(100) PRIMARY KEY,
        pin_hash     VARCHAR(100) NOT NULL,
        failed_count INTEGER      NOT NULL DEFAULT 0,
        locked_until TIMESTAMP,
        updated_at   TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);

    CONTINUE WHEN to_regclass(format('%I.doctype_meta', s.schema_name)) IS NULL;

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, document_type, module_key)
      VALUES ('StoreLabelRequest', 'Inventory', 'Transaction', 'inventory')
      ON CONFLICT (name) DO NOTHING
    $sql$, s.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('StoreLabelRequest', 'location', 'Store', 'Data', TRUE, NULL, 1),
        ('StoreLabelRequest', 'requested_by', 'Asked by', 'Data', TRUE, NULL, 2),
        ('StoreLabelRequest', 'items', 'Labels', 'JSONTable', TRUE,
         '[{"key":"sku","label":"Item","type":"link","link":"Item","required":true},{"key":"qty","label":"Labels","type":"number","required":true},{"key":"batch_no","label":"Lot"}]', 3),
        ('StoreLabelRequest', 'line_count', 'Lines', 'Number', FALSE, NULL, 4),
        ('StoreLabelRequest', 'note', 'Note', 'Data', FALSE, NULL, 5),
        ('StoreLabelRequest', 'status', 'Status', 'Select', TRUE, 'Requested,Printed,Cancelled', 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, s.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'StoreLabelRequest', TRUE, FALSE, FALSE, FALSE),
             ('Store Manager', 'StoreLabelRequest', TRUE, FALSE, FALSE, FALSE),
             ('Cashier', 'StoreLabelRequest', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, s.schema_name);
  END LOOP;
END
$mig$;
