-- ---------------------------------------------------------------------------
-- Stage 51.3: persist and lock the tenant's industry profile selection.
--
-- SwitchIndustryProfile (engines/doctype.go) already restricts who can call
-- it (requireHRAdmin/IsSuperAdmin, in handleSwitchIndustry) - but nothing
-- persisted whether a tenant had already chosen one, so that same Super
-- Admin could switch it again at any time with no friction. This table is a
-- one-row-per-tenant-schema singleton (id is always 1) recording the
-- currently locked industry, who set it and when. Once a row exists here,
-- handleSwitchIndustry refuses a further switch unless the caller passes an
-- explicit override + reason (see engines.GetIndustryLock/SwitchIndustryProfile).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant_default.industry_lock (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    industry_code VARCHAR(50) NOT NULL,
    set_by VARCHAR(255) NOT NULL,
    set_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    override_count INT NOT NULL DEFAULT 0,
    CONSTRAINT industry_lock_singleton CHECK (id = 1)
);

-- Existing tenant schemas are independent physical copies. Keep the table
-- shape identical everywhere; engines.ProvisionTenantSchema separately adds
-- industry_lock to the template-clone list for tenants created later.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'CREATE TABLE IF NOT EXISTS %I.industry_lock (LIKE tenant_default.industry_lock INCLUDING ALL)',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
