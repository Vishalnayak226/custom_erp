-- Stage 14.17-14.20: 3rd-Party Extension Isolation / Source Protection
--
-- Mechanism: out-of-process HTTP webhook hooks at a small, explicit set of
-- hook points (document.before_save/after_save), invoked over plain
-- HTTP+JSON - a hired developer's own localhost server satisfies the
-- contract just as well as a real cloud endpoint, no serverless dependency
-- required. This is the actual thing that keeps core source away from a
-- 3rd party: the contract is language-agnostic and lives in extension-sdk/
-- (meant to be split into its own repo before any real handoff), and the
-- only credential handed over is a tenant-and-doctype-scoped token
-- (engines.SignExtensionToken) - never the core repo, never another
-- client's data.

CREATE TABLE IF NOT EXISTS tenant_default.extension_hooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hook_point VARCHAR(50) NOT NULL CHECK (hook_point IN ('document.before_save', 'document.after_save')),
    doctype VARCHAR(100) NOT NULL,   -- specific doctype name, or '*' for every doctype
    target_url TEXT NOT NULL,
    secret VARCHAR(255) NOT NULL,    -- HMAC signing secret, generated server-side, shown once at creation
    enabled BOOLEAN DEFAULT TRUE,
    timeout_ms INT NOT NULL DEFAULT 3000,
    created_by VARCHAR(100),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Stores a payload hash by default, not the raw body, unless a hook
-- explicitly opts into full logging later (not built in this pass) -
-- matches the "generated + never retrievable again" caution already used
-- elsewhere (tenant admin passwords) applied to a different kind of
-- sensitive data (business document contents).
CREATE TABLE IF NOT EXISTS tenant_default.extension_hook_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hook_id UUID NOT NULL,
    request_payload_hash VARCHAR(64),
    response_status INT,
    latency_ms INT,
    error TEXT,
    called_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_extension_hook_log_hook ON tenant_default.extension_hook_log (hook_id, called_at DESC);
