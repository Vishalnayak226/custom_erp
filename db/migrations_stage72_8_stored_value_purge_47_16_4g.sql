-- ---------------------------------------------------------------------------
-- Stage 47.16.4g: a client's data reset failed on a client with gift-card
-- events - stored_value_ledger's append-only trigger (Stage 72.8) refused
-- every DELETE, the reset's included.
--
-- The trigger now accepts the one sanctioned delete gl_postings_guard
-- accepts (db/migrations_stage47_16_4b_gl_postings_guard.sql): a transaction
-- that names itself a purge with SET LOCAL erp.sanctioned_write = 'purge'
-- (engines/tenant_reset.go ResetTenantData, operator-run, approved by a
-- second person). Every other DELETE, and every UPDATE, is still refused: a
-- gift card balance changes only by a new event.
--
-- Named after 72.8 so a fresh install defines the function once 72.8 has
-- created it, never before (72.8's CREATE OR REPLACE would undo this).
-- One function in public serves every client's trigger, so nothing is
-- replayed per schema. Additive and idempotent.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION public.stored_value_ledger_append_only() RETURNS trigger
LANGUAGE plpgsql AS $fn$
BEGIN
  IF TG_OP = 'DELETE' AND COALESCE(current_setting('erp.sanctioned_write', true), '') = 'purge' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'stored_value_ledger is append-only: a gift card balance changes only by a new event';
END
$fn$;
