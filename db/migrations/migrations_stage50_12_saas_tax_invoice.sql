-- 50.12 / BLD-026 (ask 193): the platform's subscription invoice carries the
-- fields Rule 46 of the CGST Rules asks of a tax invoice, and splits the tax
-- by place of supply (CGST + SGST within the State, IGST across States).
-- Additive and safe to run twice. A row with status 'pending' is a payment
-- received while the supplier GSTIN or SAC was not yet set: it has no tax
-- invoice number until they are.
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS status            VARCHAR(10) NOT NULL DEFAULT 'issued';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS note              TEXT NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS fiscal_year       VARCHAR(9) NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS invoice_date      DATE;
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS supplier_name     TEXT NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS supplier_address  TEXT NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS supplier_gstin    VARCHAR(15) NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS recipient_name    TEXT NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS recipient_address TEXT NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS recipient_gstin   VARCHAR(15) NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS pos_state_code    VARCHAR(2) NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS pos_state_name    TEXT NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS sac               VARCHAR(10) NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS description       TEXT NOT NULL DEFAULT '';
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS cgst_paise        BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS sgst_paise        BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.saas_invoices ADD COLUMN IF NOT EXISTS igst_paise        BIGINT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS saas_invoices_tenant_idx ON public.saas_invoices (tenant_id, created_at DESC);
