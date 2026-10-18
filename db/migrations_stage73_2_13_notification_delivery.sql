-- ---------------------------------------------------------------------------
-- Stage 73.2.13 - Notifications with a real delivery state.
--
-- engines/notifications.go no longer writes "Sent" fire-and-forget. A send is
-- Queued (log row + delivery job in one transaction), Retrying while the job
-- runner backs off, Accepted once the provider answers 2xx, and Delivered or
-- Failed only on the provider's word (its answer, or a later callback on
-- POST /api/public/v1/notifications/{id}/delivery-status) - or Failed when
-- every attempt has run out. "Sent" stays a valid value for rows written
-- before this stage: they were attempted, never confirmed.
--
-- NotificationLog gains read-only evidence fields. Engine-written only, as
-- before; no role gains create/update.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
      SET options = 'Queued,Retrying,Accepted,Delivered,Failed,Skipped-NoConfig,Skipped-NoTemplate,Skipped-Sandbox,Skipped-SideEffectsOff,Sent'
      WHERE doctype_name = 'NotificationLog' AND fieldname = 'dispatch_status'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT v.* FROM (VALUES
        ('NotificationLog', 'attempts', 'Attempts', 'Number', FALSE, NULL, 8),
        ('NotificationLog', 'last_attempt_at', 'Last attempt', 'Data', FALSE, NULL, 9),
        ('NotificationLog', 'provider_ref', 'Provider reference', 'Data', FALSE, NULL, 10),
        ('NotificationLog', 'provider_http_status', 'Provider HTTP status', 'Number', FALSE, NULL, 11),
        ('NotificationLog', 'final_at', 'Final state at', 'Data', FALSE, NULL, 12),
        ('NotificationLog', 'job_id', 'Delivery job', 'Data', FALSE, NULL, 13)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = v.doctype_name)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
