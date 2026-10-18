-- ---------------------------------------------------------------------------
-- Stage 55.17a - marketing consent: no consent, no SMS / WhatsApp.
--
--   * Customer.consent_sms / consent_whatsapp: ticked only when the customer
--     has agreed to offers on that channel. consent_updated_at and
--     consent_source record when and by whom it last changed (stamped by the
--     save that changes it). Existing customers have neither ticked, so no
--     campaign reaches them until their consent is recorded.
--   * Campaign.channel: what a campaign is sent by (WhatsApp or SMS). The
--     campaign worker sends only to customers who agreed to that channel; a
--     campaign without one sends nothing.
--
-- Additive and idempotent, replayed into every tenant schema. No customer or
-- campaign record is changed.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('Customer', 'consent_sms', 'Agrees to offers by SMS', 'Check', FALSE, NULL, 61),
        ('Customer', 'consent_whatsapp', 'Agrees to offers by WhatsApp', 'Check', FALSE, NULL, 62),
        ('Customer', 'consent_updated_at', 'Consent recorded on', 'Data', FALSE, NULL, 63),
        ('Customer', 'consent_source', 'Consent recorded by', 'Data', FALSE, NULL, 64),
        ('Campaign', 'channel', 'Sent by', 'Select', TRUE, 'WhatsApp,SMS', 3)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
