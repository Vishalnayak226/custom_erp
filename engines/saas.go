package engines

import (
	"crypto/rand"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// generateRandomPassword returns a high-entropy, one-time-use password for a
// newly provisioned tenant's admin account.
func generateRandomPassword() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// IsFeatureEnabled checks whether a specific SaaS module feature flag is enabled for the tenant
func IsFeatureEnabled(tenantID string, featureName string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}

	query := fmt.Sprintf("SELECT enabled FROM %s.feature_flags WHERE feature_name = $1", schema)
	var enabled bool
	err = db.DB.QueryRow(query, featureName).Scan(&enabled)
	if err != nil {
		// Default to false if feature flag is not registered
		return false, nil
	}
	return enabled, nil
}

// SetFeatureFlag enables or disables a feature flag for the tenant
func SetFeatureFlag(tenantID string, featureName string, enabled bool) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.feature_flags (feature_name, enabled) 
		VALUES ($1, $2) 
		ON CONFLICT (feature_name) DO UPDATE SET enabled = EXCLUDED.enabled`, schema)
	_, err = db.DB.Exec(query, featureName, enabled)
	return err
}

// ProvisionTenantSchema provisions a new corporate tenant schema cloned from tenant_default templates.
// Returns the freshly generated admin password - it is never persisted in plaintext anywhere and is
// only returned this once, at creation time, for the caller to hand off securely.
// appVersion (Stage 14.6) stamps public.tenants.app_version at provisioning
// time - a point-in-time compat/audit record of which build last touched
// this tenant's schema, not a live per-request version dispatch (one running
// process can only ever serve one binary version). Callers pass the running
// binary's own currentAppVersion(); tests/tooling can pass "" to leave it
// unset.
func ProvisionTenantSchema(tenantID string, schemaName string, appVersion string) (string, error) {
	// 49.1.5: schemaName is interpolated into DDL below, so it is checked
	// against the same identifier rule db.GetTenantSchema applies on the way
	// back out rather than trusted from the caller.
	if !validSQLIdentifier(schemaName) {
		return "", fmt.Errorf("%q is not a usable schema name", schemaName)
	}

	// 49.1.5: the registry mapping and CREATE SCHEMA used to run as two
	// separate autocommitted statements ahead of the clone/seed transaction,
	// so a failure anywhere in the clone left a registry row resolving to an
	// empty schema - a tenant that authenticates and then 500s on every
	// request, with no state that says it was never finished. PostgreSQL DDL
	// is transactional, so all of it now commits or none of it does.
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// 1. Insert tenant registry mapping
	_, err = tx.Exec(`
		INSERT INTO public.tenants (tenant_id, name, schema_name, app_version, lifecycle_status, lifecycle_changed_at, provisioned_at)
		VALUES ($1, $1, $2, NULLIF($3, ''), 'active', NOW(), NOW())
		ON CONFLICT (tenant_id) DO NOTHING`, tenantID, schemaName, appVersion)
	if err != nil {
		return "", fmt.Errorf("failed to register tenant mapping: %v", err)
	}

	// 49.1.5: re-running provisioning over a tenant whose one-time credential
	// has already been rotated would overwrite a real, in-use admin password
	// (step 5's ON CONFLICT DO UPDATE) - the same class of hazard as R-01,
	// where re-running db/migration.sql resets rotated seed passwords. Refuse
	// that case specifically; a tenant that was created and never logged into
	// can still be re-provisioned, which is what makes an interrupted first
	// attempt recoverable.
	var bootstrapConsumed sql.NullTime
	if err = tx.QueryRow(`SELECT bootstrap_consumed_at FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&bootstrapConsumed); err != nil {
		return "", fmt.Errorf("failed to read tenant provisioning state: %v", err)
	}
	if bootstrapConsumed.Valid {
		return "", fmt.Errorf("tenant %s is already provisioned and its admin credential has been rotated - "+
			"refusing to re-provision it (use tenantctl rotate-bootstrap if the credential needs reissuing)", tenantID)
	}

	// 2. Create Schema
	_, err = tx.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName))
	if err != nil {
		return "", fmt.Errorf("failed to create tenant schema: %v", err)
	}

	// 3. Clone all table structures from tenant_default template.
	//
	// 26.11.2: a from-scratch load-test run against a freshly provisioned
	// tenant hit "relation tenant_scaletest0726.accounting_periods does not
	// exist" on every single GL posting - this list had drifted badly out of
	// sync with tenant_default's actual table set (confirmed via `SELECT
	// tablename FROM pg_tables WHERE schemaname = 'tenant_default'`: 41
	// tables exist, only 23 were ever cloned here). The 18 below were each
	// added by a later migration that created the table only in
	// tenant_default and never touched this list, so every tenant
	// provisioned since has been silently missing them - breaking GL
	// posting (accounting_periods), the entire maker-checker approval engine
	// (approval_log/approval_rules), WMS bin tracking (bin_stock/
	// bin_stock_lpn), marketplace/CRM integrations (channel_credentials/
	// clevertap_credentials/clevertap_event_log), Loyalty
	// (loyalty_point_ledger/loyalty_redemption_otp_challenges/
	// loyalty_tier_rules), payment reconciliation (payment_utr_log), PIM
	// publish (pim_publish_log/pim_publish_queue), POS offline sync
	// (pos_offline_heartbeats), PIM content history
	// (product_content_versions), label printing (sticker_print_log), and
	// usage-limit enforcement (tenant_limits) for every one of those
	// tenants from the moment they were created.
	tables := []string{
		"prefix_configs",
		"sequence_counters",
		"dynamic_labels",
		"audit_logs",
		"system_error_logs",
		"doctype_meta",
		"doctype_fields",
		"users",
		"role_permissions",
		"documents",
		"inventory_availability",
		"inventory_reservation",
		"integration_event_outbox",
		"integration_event_log",
		"gl_accounts",
		"gl_postings",
		"channel_product_mapping",
		"channel_order_mapping",
		"feature_flags",
		"system_settings",
		"module_entitlements",
		"extension_hooks",
		"extension_hook_log",
		"field_permissions",
		"accounting_periods",
		"approval_log",
		"approval_rules",
		"bin_stock",
		"bin_stock_lpn",
		"bin_stock_batch",
		"bin_stock_owner",
		"channel_credentials",
		"clevertap_credentials",
		"clevertap_event_log",
		"loyalty_point_ledger",
		"loyalty_redemption_otp_challenges",
		"loyalty_tier_rules",
		"payment_utr_log",
		"pim_publish_log",
		"pim_publish_queue",
		"pos_offline_heartbeats",
		"product_content_versions",
		"sticker_print_log",
		"tenant_limits",
		"api_credentials",
		"api_idempotency_keys",
		"api_request_log",
		"async_jobs",
	}

	for _, table := range tables {
		// Stage 38.2 is intentionally safe to ship before its migration is
		// applied: existing app behavior and tenant provisioning must keep
		// working during that window. Once the template table exists it is
		// cloned like every other tenant-local table; before then only this
		// new, unused table is skipped. Missing established core tables still
		// fail loudly below exactly as before.
		// Stage 42.1.3 adds bin_stock_batch (and 42.5.5 adds bin_stock_owner,
		// 38.6 adds async_jobs) to this list for the same reason Stage 38.2
		// added the three api_* tables: the binary can legitimately ship
		// before its migration has been applied, and provisioning a tenant in
		// that window must keep working. Once the template table exists it is
		// cloned like every other tenant-local table. Established core tables
		// still fail loudly below exactly as before - that is the 26.11.2 bug
		// this guard is deliberately narrow enough not to re-open.
		if table == "api_credentials" || table == "api_idempotency_keys" || table == "api_request_log" || table == "bin_stock_batch" || table == "bin_stock_owner" || table == "async_jobs" {
			var templateExists bool
			if err = tx.QueryRow(`SELECT to_regclass('tenant_default.` + table + `') IS NOT NULL`).Scan(&templateExists); err != nil {
				return "", fmt.Errorf("failed to inspect %s template: %v", table, err)
			}
			if !templateExists {
				continue
			}
		}
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (LIKE tenant_default.%s INCLUDING ALL)", schemaName, table, table)
		_, err = tx.Exec(query)
		if err != nil {
			return "", fmt.Errorf("failed to clone table structure for %s: %v", table, err)
		}
	}

	// 4. Seed metadata and master catalog configurations from template schema.
	// Deliberately excludes "users" - cloning it would give every new tenant the exact
	// same admin password hash as tenant_default. A fresh admin account with a unique,
	// randomly generated password is created explicitly below instead (step 5).
	//
	// 26.11.2: approval_rules/loyalty_tier_rules added alongside the table-
	// clone fix above - both are template/default config data (each has its
	// own seed INSERT in the migration that created it, same shape as
	// gl_accounts/role_permissions above), unlike the other 16 newly-cloned
	// tables, which are per-tenant transactional data (approval_log,
	// bin_stock*, loyalty_point_ledger, payment_utr_log, pim_publish_*,
	// pos_offline_heartbeats, product_content_versions, sticker_print_log,
	// clevertap_event_log - correctly empty for a new tenant) or per-tenant
	// secrets (channel_credentials, clevertap_credentials - copying
	// tenant_default's own live API credentials into every new tenant would
	// be a real security bug) or genuinely-starts-unconfigured (
	// accounting_periods, tenant_limits - an unset limit_key already means
	// "no limit configured" by design, see migrations_stage25_ops_status.sql).
	seeds := []string{
		"doctype_meta",
		"doctype_fields",
		"role_permissions",
		"gl_accounts",
		"prefix_configs",
		"feature_flags",
		"module_entitlements",
		"field_permissions",
		"approval_rules",
		"loyalty_tier_rules",
	}

	for _, seedTable := range seeds {
		query := fmt.Sprintf("INSERT INTO %s.%s SELECT * FROM tenant_default.%s ON CONFLICT DO NOTHING", schemaName, seedTable, seedTable)
		_, err = tx.Exec(query)
		if err != nil {
			return "", fmt.Errorf("failed to seed table data for %s: %v", seedTable, err)
		}
	}

	// 5. Create a unique admin account for this tenant with a freshly generated password.
	password, err := generateRandomPassword()
	if err != nil {
		return "", fmt.Errorf("failed to generate admin password: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash admin password: %v", err)
	}
	_, err = tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.users (id, username, password_hash, email, role, status)
		VALUES ('admin', 'admin', $1, $2, 'HR/Admin', 'Active')
		ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash`, schemaName),
		string(hash), fmt.Sprintf("admin@%s.local", tenantID))
	if err != nil {
		return "", fmt.Errorf("failed to create tenant admin user: %v", err)
	}

	// 6. (49.1.5) Record the one-time credential and its expiry, and open the
	// tenant's evidence trail. The stored hash is the same value written into
	// the admin row above; because bcrypt salts every hash, "the admin's
	// stored hash is still byte for byte the one we issued" is the cheap,
	// offline-checkable proof that this credential has never been rotated -
	// which is what the login path refuses on once the window closes.
	//
	// The account is created with the HR/Admin role, which RequiresMFA
	// already treats as MFA-mandatory: /login routes it into TOTP enrollment
	// before it can ever be issued a session token, so the one-time password
	// alone is not enough to reach the tenant's data.
	ttl := bootstrapCredentialTTL()
	if err = recordBootstrapCredentialTx(tx, tenantID, string(hash), ttl); err != nil {
		return "", fmt.Errorf("failed to record the tenant's bootstrap credential: %v", err)
	}
	if err = recordTenantLifecycleEventTx(tx, tenantID, TenantEventProvisioned, "system",
		fmt.Sprintf("schema %s created and seeded (app version %q)", schemaName, appVersion)); err != nil {
		return "", err
	}
	if err = recordTenantLifecycleEventTx(tx, tenantID, TenantEventBootstrapIssued, "system",
		fmt.Sprintf("one-time admin credential issued for %s.admin, valid for %s; MFA enrollment is mandatory at first login",
			schemaName, ttl)); err != nil {
		return "", err
	}

	// 49.7.4: deploy/postgres_harden.sql's ALTER DEFAULT PRIVILEGES only
	// covers a schema that already existed when it ran - it cannot know
	// about a tenant schema this call creates afterwards. Without this
	// grant, every tenant provisioned on an already-hardened cluster would
	// get a schema that erp_app (the runtime role deploy/erp.env's
	// DATABASE_URL would then point at) has no USAGE on at all - "permission
	// denied for schema" on its very first request. Found by actually
	// running the hardening script against a scratch database end to end
	// rather than assuming ALTER DEFAULT PRIVILEGES retroactively covers a
	// schema that did not exist yet.
	//
	// Conditional on erp_app/erp_backup actually existing so this is a
	// silent no-op on every database that has not run that script - which is
	// every database today, since it ships in this same change. Whichever
	// role is running this transaction (erp_migrate after hardening; the
	// single shared owner role before it) already owns everything it just
	// created, so it can GRANT on it without needing any extra privilege of
	// its own.
	if err := grantLeastPrivilegeSchemaAccessTx(tx, schemaName); err != nil {
		return "", fmt.Errorf("failed to grant least-privilege access on new tenant schema: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	InvalidateTenantLifecycleCache(tenantID)
	return password, nil
}

// grantLeastPrivilegeSchemaAccessTx grants erp_app (DML) and erp_backup
// (SELECT) access on a just-created tenant schema and its objects, and sets
// up default privileges for anything created in it later, matching exactly
// the per-schema grants deploy/postgres_harden.sql issues for every schema
// that existed when it ran. A no-op - not an error - when neither role
// exists, which is every database that has not run that script yet.
//
// schemaName has already passed validSQLIdentifier in ProvisionTenantSchema,
// its only caller, before reaching here.
func grantLeastPrivilegeSchemaAccessTx(tx *sql.Tx, schemaName string) error {
	var hardened bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'erp_app')`).Scan(&hardened); err != nil {
		return err
	}
	if !hardened {
		return nil
	}
	_, err := tx.Exec(fmt.Sprintf(`
		GRANT USAGE ON SCHEMA %[1]s TO erp_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %[1]s TO erp_app;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %[1]s TO erp_app;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA %[1]s TO erp_app;
		GRANT USAGE ON SCHEMA %[1]s TO erp_backup;
		GRANT SELECT ON ALL TABLES IN SCHEMA %[1]s TO erp_backup;
		GRANT SELECT ON ALL SEQUENCES IN SCHEMA %[1]s TO erp_backup;
		ALTER DEFAULT PRIVILEGES IN SCHEMA %[1]s GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO erp_app;
		ALTER DEFAULT PRIVILEGES IN SCHEMA %[1]s GRANT USAGE, SELECT ON SEQUENCES TO erp_app;
		ALTER DEFAULT PRIVILEGES IN SCHEMA %[1]s GRANT EXECUTE ON FUNCTIONS TO erp_app;
		ALTER DEFAULT PRIVILEGES IN SCHEMA %[1]s GRANT SELECT ON TABLES TO erp_backup;
		ALTER DEFAULT PRIVILEGES IN SCHEMA %[1]s GRANT SELECT ON SEQUENCES TO erp_backup;
	`, schemaName))
	return err
}
