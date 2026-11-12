-- ---------------------------------------------------------------------------
-- Stage 47.16.4f (G6 of docs/security/gst-companies-act-controls.md): the
-- IRP's raw response kept, and its acknowledgement frozen.
--
--   * irp_attempts.response_body: the IRP's response to a generate or a
--     lookup that returned an IRN, as received (the signed invoice and QR
--     inside it are the IRP's own signature). Secrets are masked before it is
--     stored (engines/einvoice_worker.go irpEvidenceBody); credentials and the
--     session token are never in it.
--   * irp_documents: once a row carries an IRN, the acknowledgement - irn,
--     ack_no, ack_date, signed_invoice, signed_qr - and the request it
--     acknowledged (payload, payload_hash) never change, and the row is never
--     deleted. A field still empty may be filled once. Cancelling an IRN
--     writes its own columns (state, cancelled_at), not these.
--
-- The trigger's name says "immutable", so a master renumber leaves the frozen
-- request as it was sent (engines/master_recode.go recodeTargets).
-- A deliberate purge (SET LOCAL erp.sanctioned_write = 'purge') may delete.
--
-- Named to sort right after migrations_stage75_2_einvoice.sql, which creates
-- the tables: a fresh install runs it second. Additive and idempotent; every
-- tenant schema that has the e-invoice tables.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION public.irp_documents_ack_immutable() RETURNS trigger
LANGUAGE plpgsql AS $fn$
DECLARE
  changed TEXT[] := ARRAY[]::TEXT[];
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF NULLIF(OLD.irn, '') IS NULL OR COALESCE(current_setting('erp.sanctioned_write', true), '') = 'purge' THEN
      RETURN OLD;
    END IF;
    RAISE EXCEPTION 'e-invoice % has an IRN - its record is kept, not deleted', OLD.doc_number
      USING ERRCODE = 'check_violation';
  END IF;
  IF NULLIF(OLD.irn, '') IS NOT NULL AND NEW.irn IS DISTINCT FROM OLD.irn THEN changed := changed || 'irn'::TEXT; END IF;
  IF NULLIF(OLD.ack_no, '') IS NOT NULL AND NEW.ack_no IS DISTINCT FROM OLD.ack_no THEN changed := changed || 'ack_no'::TEXT; END IF;
  IF OLD.ack_date IS NOT NULL AND NEW.ack_date IS DISTINCT FROM OLD.ack_date THEN changed := changed || 'ack_date'::TEXT; END IF;
  IF NULLIF(OLD.signed_invoice, '') IS NOT NULL AND NEW.signed_invoice IS DISTINCT FROM OLD.signed_invoice THEN changed := changed || 'signed_invoice'::TEXT; END IF;
  IF NULLIF(OLD.signed_qr, '') IS NOT NULL AND NEW.signed_qr IS DISTINCT FROM OLD.signed_qr THEN changed := changed || 'signed_qr'::TEXT; END IF;
  IF NULLIF(OLD.irn, '') IS NOT NULL THEN
    IF NEW.payload IS DISTINCT FROM OLD.payload THEN changed := changed || 'payload'::TEXT; END IF;
    IF NEW.payload_hash IS DISTINCT FROM OLD.payload_hash THEN changed := changed || 'payload_hash'::TEXT; END IF;
  END IF;
  IF cardinality(changed) = 0 THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'e-invoice % is acknowledged by the IRP: % cannot change', OLD.doc_number, array_to_string(changed, ', ')
    USING ERRCODE = 'check_violation';
END
$fn$;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'irp_attempts') THEN
      EXECUTE format('ALTER TABLE %I.irp_attempts ADD COLUMN IF NOT EXISTS response_body TEXT', schema_rec.schema_name);
    END IF;
    CONTINUE WHEN NOT EXISTS (SELECT 1 FROM information_schema.tables
                              WHERE table_schema = schema_rec.schema_name AND table_name = 'irp_documents');
    IF NOT EXISTS (SELECT 1 FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE n.nspname = schema_rec.schema_name AND c.relname = 'irp_documents' AND t.tgname = 'irp_documents_ack_immutable') THEN
      EXECUTE format($sql$
        CREATE TRIGGER irp_documents_ack_immutable BEFORE UPDATE OR DELETE ON %I.irp_documents
        FOR EACH ROW EXECUTE FUNCTION public.irp_documents_ack_immutable()
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
