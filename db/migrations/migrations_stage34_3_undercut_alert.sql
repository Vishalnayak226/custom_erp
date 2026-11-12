-- Stage 34.3: competitor undercut alerting.
--
-- Two small additive pieces; the dispatch mechanism itself is entirely
-- existing (engines/notifications.go's DispatchNotification, the same path
-- order/return lifecycle events already use).

-- 1. Worker de-duplication state. Single-row, exactly the shape
--    public.patch_intake_state uses (migrations_stage14d_patchintake.sql), so
--    a cycle only considers CompetitorPrice rows created since the previous
--    successful cycle. Without this the worker would re-alert on the same
--    standing undercut on every single tick.
CREATE TABLE IF NOT EXISTS public.competitor_undercut_state (
    id INT PRIMARY KEY DEFAULT 1,
    last_run_at TIMESTAMP,
    CONSTRAINT competitor_undercut_single_row CHECK (id = 1)
);
INSERT INTO public.competitor_undercut_state (id, last_run_at) VALUES (1, NULL) ON CONFLICT (id) DO NOTHING;

-- 2. Make 'Competitor Undercut' a selectable NotificationTemplate event, so an
--    admin can author the alert body in the existing Setup UI rather than the
--    event being a magic string only the Go code knows about.
--
--    Appended to the existing option list rather than replacing it, and guarded
--    so re-running the migration cannot append it twice.
UPDATE tenant_default.doctype_fields
   SET options = options || ',Competitor Undercut'
 WHERE doctype_name = 'NotificationTemplate'
   AND fieldname = 'event'
   AND options NOT LIKE '%Competitor Undercut%';

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format($f$
      UPDATE %I.doctype_fields
         SET options = options || ',Competitor Undercut'
       WHERE doctype_name = 'NotificationTemplate'
         AND fieldname = 'event'
         AND options NOT LIKE '%%Competitor Undercut%%'
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
