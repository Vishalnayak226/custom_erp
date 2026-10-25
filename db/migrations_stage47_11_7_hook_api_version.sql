-- ---------------------------------------------------------------------------
-- Stage 47.11.7: each extension hook records the hook-contract version it was
-- written against (engines/extensions.go HookAPIVersion), so a breaking change
-- to the payload can keep serving older hooks through a deprecation window.
-- Existing hooks were written against version 1, the only one so far.
-- Additive: one column with a default; nothing else changes.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  s RECORD;
BEGIN
  FOR s IN
    SELECT table_schema FROM information_schema.tables
    WHERE table_name = 'extension_hooks' AND table_schema LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format('ALTER TABLE %I.extension_hooks ADD COLUMN IF NOT EXISTS api_version INT NOT NULL DEFAULT 1', s.table_schema);
  END LOOP;
END
$mig$;
