-- Stage 24, deferred items built on explicit user request (24.27-24.32).
-- Additive-only, same convention as every other migration file in this repo.
-- See docs/micro_checklist.md Stage 24 for the source findings each block
-- below closes.

-- 24.28: password reset flow. reset_token_hash stores a SHA-256 hash of the
-- reset token, never the token itself - a DB leak alone can't be used to
-- reset a password, the same reasoning session tokens already get (this
-- app never stores a raw session token either, just verifies signatures).
ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS reset_token_hash VARCHAR(64);
ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS reset_token_expires_at TIMESTAMP;

-- 24.31: optional per-field max length, checked by ValidateDocument
-- alongside the existing mandatory/format/select/link checks. NULL (every
-- existing field today) means "no explicit per-field limit configured" -
-- ValidateDocument still applies a blanket default cap in that case, this
-- column only lets a specific field's limit be tightened or loosened.
ALTER TABLE tenant_default.doctype_fields ADD COLUMN IF NOT EXISTS max_length INT;
