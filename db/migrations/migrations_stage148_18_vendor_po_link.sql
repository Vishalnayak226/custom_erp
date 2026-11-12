-- ---------------------------------------------------------------------------
-- Stage 148.18: the vendor's signed link to one purchase order.
--
--   vendor_po_links      one row per link: the PO it is for, who made it, when it
--                        expires, whether it was revoked. The link itself is signed
--                        (HMAC over client, link id, PO and expiry); nothing secret
--                        is stored.
--   vendor_po_proposals  a vendor's proposed quantity / date for one PO line,
--                        waiting for the buyer (or applied at once for a trusted
--                        vendor).
--   vendor_po_files      an invoice the vendor uploaded; it waits for buyer review
--                        and never posts. Capped at 2 MB and 5 per link.
--   vendor_unit_names    a vendor's own name for a unit ("pieces" = Nos), per vendor.
--
-- Vendor.vendor_link_trusted (blank = No) lets a vendor's proposals apply without
-- the buyer. Everything is empty / blank until the setting procurement.vendor_link
-- is turned on, so no client changes. No business row is written.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.vendor_po_links (
        id               VARCHAR(32) PRIMARY KEY,
        po_id            VARCHAR(200) NOT NULL,
        vendor_id        VARCHAR(200) NOT NULL DEFAULT '',
        created_by       VARCHAR(200) NOT NULL DEFAULT '',
        created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
        expires_at       TIMESTAMPTZ NOT NULL,
        revoked_at       TIMESTAMPTZ,
        revoked_by       VARCHAR(200) NOT NULL DEFAULT '',
        last_opened_at   TIMESTAMPTZ,
        open_count       INTEGER NOT NULL DEFAULT 0,
        last_reminded_at TIMESTAMPTZ
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS vendor_po_links_po_idx ON %I.vendor_po_links (po_id, created_at DESC)
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.vendor_po_proposals (
        id            BIGSERIAL PRIMARY KEY,
        link_id       VARCHAR(32) NOT NULL,
        po_id         VARCHAR(200) NOT NULL,
        line_no       INTEGER NOT NULL,
        qty           NUMERIC(18,3),
        promised_date VARCHAR(10) NOT NULL DEFAULT '',
        vendor_unit   VARCHAR(40) NOT NULL DEFAULT '',
        note          VARCHAR(200) NOT NULL DEFAULT '',
        status        VARCHAR(20) NOT NULL DEFAULT 'Pending',
        auto_applied  BOOLEAN NOT NULL DEFAULT FALSE,
        created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
        decided_by    VARCHAR(200) NOT NULL DEFAULT '',
        decided_at    TIMESTAMPTZ,
        decision_note VARCHAR(300) NOT NULL DEFAULT ''
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS vendor_po_proposals_po_idx ON %I.vendor_po_proposals (po_id, status, created_at DESC)
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.vendor_po_files (
        id           BIGSERIAL PRIMARY KEY,
        link_id      VARCHAR(32) NOT NULL,
        po_id        VARCHAR(200) NOT NULL,
        file_name    VARCHAR(120) NOT NULL,
        content_type VARCHAR(60) NOT NULL,
        size_bytes   INTEGER NOT NULL,
        data         BYTEA NOT NULL,
        invoice_no   VARCHAR(60) NOT NULL DEFAULT '',
        invoice_date VARCHAR(10) NOT NULL DEFAULT '',
        amount       NUMERIC(18,2),
        status       VARCHAR(20) NOT NULL DEFAULT 'Pending review',
        created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
        reviewed_by  VARCHAR(200) NOT NULL DEFAULT '',
        reviewed_at  TIMESTAMPTZ,
        review_note  VARCHAR(300) NOT NULL DEFAULT ''
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS vendor_po_files_po_idx ON %I.vendor_po_files (po_id, created_at DESC)
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.vendor_unit_names (
        vendor_id   VARCHAR(200) NOT NULL,
        vendor_unit VARCHAR(40) NOT NULL,
        item_unit   VARCHAR(40) NOT NULL,
        created_by  VARCHAR(200) NOT NULL DEFAULT '',
        created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
        PRIMARY KEY (vendor_id, vendor_unit)
      )
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES ('Vendor', 'vendor_link_trusted', 'Link changes apply at once', 'Select', FALSE, ',No,Yes', 88)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
