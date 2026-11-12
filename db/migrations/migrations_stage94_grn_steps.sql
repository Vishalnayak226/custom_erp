-- ---------------------------------------------------------------------------
-- Stage 94 (user, 2026-10-18): goods receipt in steps - receive, verify,
-- price check into child GRNs, MRP and posting, optional payment.
-- "All should be save in table to extract in report and edit is always
-- allowed if user do mistake in any stage."
--
-- grn_line: one row per GRN line with every step's figures (PO, received,
-- invoice, GRN qty / price, previous price, group, MRP, who and when). The GRN
-- document stays the source; engines/grn_steps.go rewrites a GRN's rows on
-- every save, so reports read columns instead of parsing JSON.
--
-- grn_line_change: one row per field changed at a step (old -> new, who,
-- when, why) - the correction log report reads it.
--
-- GRNExcessApproval: the maker-checker record for keeping goods above the PO
-- (or not on any PO) beyond the vendor's over-receipt tolerance (D6). It runs
-- through the existing approval engine; a decision hook in grn_steps.go
-- releases or returns the excess.
--
-- Additive only. New tables into tenant_default (SyncAllTenantSchemas copies
-- them to every client after migrate); the doctype, its permissions and its
-- approval rule into every schema. Existing GRNs and their notes are untouched.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant_default.grn_line (
  grn_id          VARCHAR(100)  NOT NULL,
  line_no         INT           NOT NULL,
  grn_code        VARCHAR(100),
  flow            VARCHAR(20),
  vendor          VARCHAR(100),
  location        VARCHAR(100),
  vendor_invoice_no VARCHAR(100),
  po_id           VARCHAR(100),
  sku             VARCHAR(200)  NOT NULL,
  po_qty          NUMERIC(14,3),
  po_rate         NUMERIC(14,2),
  received_qty    NUMERIC(14,3),
  invoice_qty     NUMERIC(14,3),
  invoice_price   NUMERIC(14,2),
  grn_qty         NUMERIC(14,3),
  grn_price       NUMERIC(14,2),
  gst_rate        NUMERIC(6,2),
  rejected_qty    NUMERIC(14,3),
  damaged_qty     NUMERIC(14,3),
  rejection_reason TEXT,
  damage_reason   TEXT,
  excess_qty      NUMERIC(14,3),
  excess_action   VARCHAR(20),
  prev_price      NUMERIC(14,2),
  prev_grn        VARCHAR(100),
  prev_vendor     VARCHAR(100),
  prev_date       DATE,
  price_group     VARCHAR(20),
  child_key       VARCHAR(10),
  child_name      VARCHAR(100),
  old_mrp         NUMERIC(14,2),
  new_mrp         NUMERIC(14,2),
  batch_no        VARCHAR(100),
  step            VARCHAR(20),
  child_status    VARCHAR(20),
  received_by     VARCHAR(100),
  received_at     TIMESTAMPTZ,
  verified_by     VARCHAR(100),
  verified_at     TIMESTAMPTZ,
  priced_by       VARCHAR(100),
  priced_at       TIMESTAMPTZ,
  posted_by       VARCHAR(100),
  posted_at       TIMESTAMPTZ,
  updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  PRIMARY KEY (grn_id, line_no)
);
-- The previous purchase price (D4) is the latest posted line of a SKU.
CREATE INDEX IF NOT EXISTS grn_line_sku_posted_idx ON tenant_default.grn_line (sku, posted_at DESC);
CREATE INDEX IF NOT EXISTS grn_line_step_idx ON tenant_default.grn_line (step);

CREATE TABLE IF NOT EXISTS tenant_default.grn_line_change (
  id          BIGSERIAL     PRIMARY KEY,
  grn_id      VARCHAR(100)  NOT NULL,
  line_no     INT,
  sku         VARCHAR(200),
  step        VARCHAR(20)   NOT NULL,
  field       VARCHAR(60)   NOT NULL,
  old_value   TEXT,
  new_value   TEXT,
  reason      TEXT,
  changed_by  VARCHAR(100),
  changed_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS grn_line_change_grn_idx ON tenant_default.grn_line_change (grn_id, changed_at);

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_meta (name, module, document_type, module_key) VALUES
        ('GRNExcessApproval', 'Procurement', 'Transaction', 'procurement')
      ON CONFLICT (name) DO NOTHING$sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('GRNExcessApproval', 'grn_id', 'Goods receipt', 'Link', TRUE, 'GRN', 1),
        ('GRNExcessApproval', 'vendor', 'Vendor', 'Link', FALSE, 'Vendor', 2),
        ('GRNExcessApproval', 'excess_lines', 'Excess lines', 'Text', TRUE, NULL, 3),
        ('GRNExcessApproval', 'excess_value', 'Excess value', 'Currency', FALSE, NULL, 4),
        ('GRNExcessApproval', 'reason', 'Why keep it', 'Text', FALSE, NULL, 5),
        ('GRNExcessApproval', 'requested_by', 'Asked by', 'Data', FALSE, NULL, 6),
        ('GRNExcessApproval', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 7)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('Store Manager', 'GRNExcessApproval', TRUE, FALSE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING$sql$, schema_rec.schema_name);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
          ('GRNExcessApproval', 0, NULL, 'Store Manager')
        ON CONFLICT (doctype, min_amount) DO NOTHING$sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
