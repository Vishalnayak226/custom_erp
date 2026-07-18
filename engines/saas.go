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

	// 3. Clone all table structures from tenant_default template
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
		"module_entitlements",
		"extension_hooks",
		"extension_hook_log",
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	for _, table := range tables {
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
	seeds := []string{
		"doctype_meta",
		"doctype_fields",
		"role_permissions",
		"gl_accounts",
		"prefix_configs",
		"feature_flags",
		"module_entitlements",
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
