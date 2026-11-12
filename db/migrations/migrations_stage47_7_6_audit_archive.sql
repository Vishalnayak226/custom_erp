-- ---------------------------------------------------------------------------
-- Stage 47.7.6 - audit evidence archive: integrity-checked encrypted/
-- compressed archive, manifest-then-delete, and a tested query/export/restore
-- drill. Closes the one remaining piece of 47.7 (A-07/A-30).
--
-- Retention/legal-hold schema (audit_retention_policy, audit_legal_hold) and
-- checkpoints (audit_checkpoints) already exist from
-- migrations_stage47_7_audit_evidence.sql. This adds the archive record
-- itself and the pointer from a checkpoint to the archive that now covers it.
--
-- ONE ARCHIVE == ONE CHECKPOINT WINDOW, never a partial window and never more
-- than one checkpoint. That keeps the invariant simple: a checkpoint's
-- verification either reads the live audit_logs rows in [from_seq, to_seq]
-- (archive_id IS NULL) or reads the one archive file that holds exactly that
-- range (archive_id set) - never a mix. See engines/audit_archive.go.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS tenant_default.audit_archives (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    checkpoint_id UUID NOT NULL,
    from_seq BIGINT NOT NULL,
    to_seq BIGINT NOT NULL,
    row_count INT NOT NULL,
    unsigned_count INT NOT NULL DEFAULT 0,
    -- Copied from the checkpoint that sealed this window, so a restored or
    -- queried archive can be checked against the value that was sealed
    -- without a join, and so the archive stays self-describing even if the
    -- checkpoint row is ever pruned.
    row_digest VARCHAR(64) NOT NULL,
    file_path TEXT NOT NULL,
    -- SHA-256 of the encrypted file AS STORED ON DISK - catches the file
    -- being swapped or corrupted independently of the row_digest inside it,
    -- which is only checked after decryption succeeds.
    file_sha256 VARCHAR(64) NOT NULL,
    manifest_signature VARCHAR(64) NOT NULL,
    sig_version VARCHAR(20) NOT NULL,
    compressed_bytes INT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Set by a restore drill or a real legal-recovery restore. NOT cleared
    -- back to NULL by a restore - it is history, not current state; current
    -- state is "does audit_checkpoints.archive_id still point here", which a
    -- restore also clears.
    restored_at TIMESTAMP,
    restored_by VARCHAR(100)
);

CREATE INDEX IF NOT EXISTS idx_audit_archives_checkpoint
    ON tenant_default.audit_archives (checkpoint_id);
CREATE INDEX IF NOT EXISTS idx_audit_archives_created
    ON tenant_default.audit_archives (created_at DESC);

-- NULL = still hot (rows live in audit_logs; verified straight off the
-- table, exactly as before this migration). Set only after
-- RunAuditArchive has written the archive file, self-verified it can be
-- read back correctly, AND recorded this row - in that order - and only
-- then deletes the rows. See the manifest-then-delete sequencing in
-- engines/audit_archive.go's RunAuditArchive.
ALTER TABLE tenant_default.audit_checkpoints
    ADD COLUMN IF NOT EXISTS archive_id UUID REFERENCES tenant_default.audit_archives(id);
