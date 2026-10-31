-- Stage 49.18.2 (remaining part): every bounded state table states its index
-- and query SLO (engines/bounded_state.go, SecurityStateStores). Eleven of the
-- tables a retention sweep deletes from had no index on the column the sweep
-- ages them by, so every hourly sweep read the whole table to find nothing -
-- a cost that grew with the table it was meant to keep small. Each gets one.
--
-- Additive: CREATE INDEX IF NOT EXISTS only, in tenant_default and every
-- client schema that has the table; no row is read or changed.
--
-- Named to sort after Stage 140.12: the runner orders by stage number, and
-- tables it indexes (crm_events, crm_pref_tokens - Stage 101) are created by
-- later-numbered migrations, so under 49.18.2 a fresh database had nothing to
-- index. Keep it after the last migration that creates one of its tables.

DO $$
DECLARE
  s RECORD;
  ix RECORD;
BEGIN
  FOR s IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    FOR ix IN
      SELECT * FROM (VALUES
        ('idx_async_jobs_completed',          'async_jobs',               'completed_at'),
        ('idx_command_idempotency_completed', 'command_idempotency',      'completed_at'),
        ('idx_api_oauth_tokens_expires',      'api_oauth_tokens',         'expires_at'),
        ('idx_outbox_created',                'integration_event_outbox', 'created_at'),
        ('idx_integration_event_log_created', 'integration_event_log',    'created_at'),
        ('idx_extension_hook_log_called',     'extension_hook_log',       'called_at'),
        ('idx_clevertap_event_log_created',   'clevertap_event_log',      'created_at'),
        ('idx_pim_publish_log_created',       'pim_publish_log',          'created_at'),
        ('idx_system_error_logs_created',     'system_error_logs',        'created_at'),
        ('crm_pref_tokens_created_idx',       'crm_pref_tokens',          'created_at'),
        ('crm_events_occurred_idx',           'crm_events',               'occurred_at')
      ) AS t(index_name, table_name, column_name)
    LOOP
      IF EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_schema = s.schema_name AND table_name = ix.table_name AND column_name = ix.column_name) THEN
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON %I.%I (%I)', ix.index_name, s.schema_name, ix.table_name, ix.column_name);
      END IF;
    END LOOP;
  END LOOP;
END $$;
