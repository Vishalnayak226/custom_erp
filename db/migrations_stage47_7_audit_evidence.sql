-- ---------------------------------------------------------------------------
-- Stage 47.7 - audit evidence: independently signed events + checkpoints
-- (audit findings A-07/A-30).
--
-- THE DECISION THIS IMPLEMENTS (user, 2026-09-09). 47.7.2 offered two models:
-- serialize the chain scope, or sign independently and checkpoint. The second
-- was chosen, and the reason is concrete rather than aesthetic.
--
-- What is there today is the worst of both. engines/logs.go computes each row's
-- checksum from the PREVIOUS row's checksum - chain semantics - but
-- deliberately does not lock, and says so in its own comment: "Worst case under
-- real concurrency is two rows briefly chaining from the same parent". That is
-- precisely the failure 47.7.2 names: "two writers cannot create siblings the
-- verifier calls broken." So under concurrency the verifier reports tampering
-- on data that is perfectly clean, which trains an operator to ignore it.
--
-- Adding the lock instead would serialize every audit write per tenant. Audit
-- logging runs on nearly every request, and Stage 47.3 had just finished making
-- checkout concurrent on purpose (deterministic lock ordering so two tills can
-- sell at once). A per-tenant audit chain would reintroduce a global
-- serialization point on every sale and undo that.
--
-- Independent signatures have no ordering dependency, so concurrency is a
-- non-issue by construction. What a per-row signature alone cannot detect is
-- DELETION - remove a row and the survivors still verify. Checkpoints close
-- that: a periodic digest over a definite range of rows, so a missing row
-- changes both the count and the digest. Same shape as AWS QLDB digests and
-- Certificate Transparency signed tree heads.
-- ---------------------------------------------------------------------------

-- --- 47.7.1/47.7.2: the signed event ---------------------------------------
--
-- `checksum` is left exactly as it is. It is not migrated, not recomputed and
-- not dropped: 345,808 of the 383,810 rows in the development database (90.1%,
-- matching the audit's own "≈90%" estimate) never had one, and rewriting
-- history to make old rows look signed is the fabricated-evidence outcome
-- 47.7.4 explicitly forbids. The new columns start empty and fill going
-- forward; the boundary between the two eras is recorded, not hidden.
ALTER TABLE tenant_default.audit_logs
    ADD COLUMN IF NOT EXISTS seq BIGSERIAL,
    -- HMAC over this row's own content only - no dependency on any other row,
    -- which is what makes concurrent writers safe.
    ADD COLUMN IF NOT EXISTS signature VARCHAR(64),
    -- Which signing scheme/key produced `signature`, so a key rotation or a
    -- format change is a new version rather than a silent reinterpretation of
    -- old evidence (47.7.8's key-rotation case).
    ADD COLUMN IF NOT EXISTS sig_version VARCHAR(20),
    -- 47.7.1's required fields that the row shape simply did not carry:
    -- which entity the mutation was about, and the correlation/idempotency
    -- handle that ties it to the request and to Stage 47.3's command record.
    ADD COLUMN IF NOT EXISTS entity_type VARCHAR(100),
    ADD COLUMN IF NOT EXISTS entity_id VARCHAR(200),
    ADD COLUMN IF NOT EXISTS correlation_id VARCHAR(100);

-- seq must be unique for a checkpoint to name a definite range.
CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_logs_seq ON tenant_default.audit_logs (seq);

-- --- 47.7.5: indexes for MEASURED access patterns --------------------------
--
-- Only the four the API actually issues today: newest-first (the log screen's
-- default), by actor, by entity, and by correlation. Deliberately no
-- speculative index - the item says so, and every extra index is a write cost
-- on a table that takes a row on nearly every request.
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_desc
    ON tenant_default.audit_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor
    ON tenant_default.audit_logs (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_entity
    ON tenant_default.audit_logs (entity_type, entity_id, created_at DESC)
    WHERE entity_type IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_logs_correlation
    ON tenant_default.audit_logs (correlation_id)
    WHERE correlation_id IS NOT NULL;

-- --- 47.7.2: checkpoints ---------------------------------------------------
--
-- One row per verified window. `row_digest` is a digest over the ORDERED
-- signatures of every row in [from_seq, to_seq], so removing a row changes
-- both `row_count` and `row_digest`, and inserting a backdated one changes the
-- digest. `prev_checkpoint_signature` chains the CHECKPOINTS - there are few
-- of them and they are written by one scheduled job, so chaining them costs
-- nothing and gives the whole series a single verifiable head.
CREATE TABLE IF NOT EXISTS tenant_default.audit_checkpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_seq BIGINT NOT NULL,
    to_seq   BIGINT NOT NULL,
    row_count INT NOT NULL,
    -- Rows in the window that carry no signature (the legacy era). Recorded so
    -- a checkpoint states its own coverage honestly instead of implying it
    -- verified rows it could not.
    unsigned_count INT NOT NULL DEFAULT 0,
    row_digest VARCHAR(64) NOT NULL,
    prev_checkpoint_signature VARCHAR(64),
    signature VARCHAR(64) NOT NULL,
    sig_version VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- 'Sealed' marks the one-off legacy boundary checkpoint (47.7.4);
    -- 'Periodic' is the ongoing job's output.
    kind VARCHAR(20) NOT NULL DEFAULT 'Periodic',
    CONSTRAINT audit_checkpoints_range_check CHECK (to_seq >= from_seq)
);

CREATE INDEX IF NOT EXISTS idx_audit_checkpoints_range
    ON tenant_default.audit_checkpoints (to_seq DESC);

-- --- 47.7.6: retention, legal hold and erasure -----------------------------
--
-- Versioned rather than a constant, because a retention period is a legal
-- position that changes and whose PREVIOUS value has to remain explainable:
-- "why was this deleted in March" needs the rule that was in force in March.
CREATE TABLE IF NOT EXISTS tenant_default.audit_retention_policy (
    id SERIAL PRIMARY KEY,
    version INT NOT NULL,
    hot_window_days INT NOT NULL,
    archive_after_days INT NOT NULL,
    -- NULL = keep for ever. An explicit NULL rather than a sentinel number,
    -- so "indefinite" cannot be confused with "a very long time".
    delete_after_days INT,
    effective_from TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    approved_by VARCHAR(100),
    note TEXT
);

-- A legal hold suspends deletion for a scope, and must outrank retention.
CREATE TABLE IF NOT EXISTS tenant_default.audit_legal_hold (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope_entity_type VARCHAR(100),
    scope_entity_id VARCHAR(200),
    scope_actor VARCHAR(100),
    reason TEXT NOT NULL,
    placed_by VARCHAR(100) NOT NULL,
    placed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    released_by VARCHAR(100),
    released_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_legal_hold_active
    ON tenant_default.audit_legal_hold (scope_entity_type, scope_entity_id)
    WHERE released_at IS NULL;

-- The shipped default: a year hot, archive after a year, never auto-delete.
-- Deliberately conservative - deleting audit evidence by default is not a
-- decision a migration should make for a tenant.
INSERT INTO tenant_default.audit_retention_policy
    (version, hot_window_days, archive_after_days, delete_after_days, approved_by, note)
SELECT 1, 365, 365, NULL, 'stage47.7-default',
       'Shipped default. Auto-deletion is OFF (delete_after_days NULL) - enabling it is a legal decision a tenant must take deliberately.'
WHERE NOT EXISTS (SELECT 1 FROM tenant_default.audit_retention_policy);
