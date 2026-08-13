-- Stage 38.3 + 38.5 + 38.9: the safety spine every public API route depends on.
--
-- Built as one migration because the three are one mechanism, not three: a
-- request is admitted (38.3 quota), de-duplicated (38.5 idempotency) and
-- recorded (38.9 traffic log) by the same middleware pass, and the traffic log
-- is also what makes the daily quota survive a process restart. Shipping any
-- one of them alone would leave the public surface either unbounded, unsafe to
-- retry, or unobservable.
--
-- Additive only: two nullable columns on the existing credential table plus two
-- new tables. Nothing here changes existing behaviour, and no /api/public/v1
-- route is reachable without the middleware that reads them.

-- 38.3: per-credential overrides. NULL means "use the tenant's configured
-- default" (platform.public_api_* settings), so raising a plan-wide limit does
-- not require touching every key, while a single noisy integration can still be
-- pinned without affecting anyone else.
ALTER TABLE tenant_default.api_credentials
    ADD COLUMN IF NOT EXISTS rate_limit_per_minute INT,
    ADD COLUMN IF NOT EXISTS daily_quota INT;

ALTER TABLE tenant_default.api_credentials
    DROP CONSTRAINT IF EXISTS api_credentials_rate_limit_positive;
ALTER TABLE tenant_default.api_credentials
    ADD CONSTRAINT api_credentials_rate_limit_positive
    CHECK (rate_limit_per_minute IS NULL OR rate_limit_per_minute > 0);
ALTER TABLE tenant_default.api_credentials
    DROP CONSTRAINT IF EXISTS api_credentials_daily_quota_positive;
ALTER TABLE tenant_default.api_credentials
    ADD CONSTRAINT api_credentials_daily_quota_positive
    CHECK (daily_quota IS NULL OR daily_quota > 0);

-- 38.5: idempotency keys. The unique constraint is the whole mechanism - the
-- INSERT itself is the lock, so two concurrent retries of the same request
-- cannot both proceed regardless of how the application schedules them.
--
-- request_fingerprint is a SHA-256 of method + path + body. Replaying a stored
-- response for a DIFFERENT request that happened to reuse a key would be worse
-- than no idempotency at all, so a fingerprint mismatch is an error, never a
-- replay.
CREATE TABLE IF NOT EXISTS tenant_default.api_idempotency_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credential_id UUID NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    request_fingerprint CHAR(64) NOT NULL,
    method VARCHAR(10) NOT NULL,
    path VARCHAR(512) NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'In Progress'
        CHECK (state IN ('In Progress', 'Completed')),
    response_status INT,
    response_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ,
    UNIQUE (credential_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_api_idempotency_created
    ON tenant_default.api_idempotency_keys (created_at);

-- 38.9: per-credential traffic log. Deliberately a raw table, not a document:
-- it is append-only high-volume operational data that must never appear in a
-- generic document list, and it carries no business meaning to approve or edit.
-- No request or response body is stored - only the metadata needed to answer
-- "who called what, when, and what happened", which keeps customer payloads out
-- of a table that is read for support and capacity questions.
CREATE TABLE IF NOT EXISTS tenant_default.api_request_log (
    id BIGSERIAL PRIMARY KEY,
    credential_id UUID,
    key_prefix VARCHAR(32),
    method VARCHAR(10) NOT NULL,
    path VARCHAR(512) NOT NULL,
    required_scope VARCHAR(64),
    status_code INT NOT NULL,
    duration_ms INT NOT NULL,
    correlation_id VARCHAR(64),
    client_ip VARCHAR(64),
    idempotency_key VARCHAR(255),
    outcome VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- The quota reader counts one credential's calls since midnight; the retention
-- sweeper deletes by age. Both are covered by this one index.
CREATE INDEX IF NOT EXISTS idx_api_request_log_credential_time
    ON tenant_default.api_request_log (credential_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_request_log_time
    ON tenant_default.api_request_log (created_at);

-- Existing tenant schemas are independent physical copies - same catch-up loop
-- the Stage 38.2 migration uses.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'ALTER TABLE IF EXISTS %I.api_credentials
         ADD COLUMN IF NOT EXISTS rate_limit_per_minute INT,
         ADD COLUMN IF NOT EXISTS daily_quota INT',
      schema_rec.schema_name
    );
    EXECUTE format(
      'CREATE TABLE IF NOT EXISTS %I.api_idempotency_keys (LIKE tenant_default.api_idempotency_keys INCLUDING ALL)',
      schema_rec.schema_name
    );
    EXECUTE format(
      'CREATE TABLE IF NOT EXISTS %I.api_request_log (LIKE tenant_default.api_request_log INCLUDING ALL)',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
