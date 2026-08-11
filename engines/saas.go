package engines

import (
	"crypto/rand"
	"custom_erp/db"
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
	// 1. Insert tenant registry mapping
	_, err := db.DB.Exec(`
		INSERT INTO public.tenants (tenant_id, name, schema_name, app_version)
		VALUES ($1, $1, $2, NULLIF($3, ''))
		ON CONFLICT (tenant_id) DO NOTHING`, tenantID, schemaName, appVersion)
	if err != nil {
		return "", fmt.Errorf("failed to register tenant mapping: %v", err)
	}

	// 2. Create Schema
	_, err = db.DB.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName))
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
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	for _, table := range tables {
		// Stage 38.2 is intentionally safe to ship before its migration is
		// applied: existing app behavior and tenant provisioning must keep
		// working during that window. Once the template table exists it is
		// cloned like every other tenant-local table; before then only this
		// new, unused table is skipped. Missing established core tables still
		// fail loudly below exactly as before.
		if table == "api_credentials" {
			var templateExists bool
			if err = tx.QueryRow(`SELECT to_regclass('tenant_default.api_credentials') IS NOT NULL`).Scan(&templateExists); err != nil {
				return "", fmt.Errorf("failed to inspect api_credentials template: %v", err)
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

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return password, nil
}
