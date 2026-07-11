package engines

import (
	"custom_erp/db"
	"fmt"
)

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

// ProvisionTenantSchema provisions a new corporate tenant schema cloned from tenant_default templates
func ProvisionTenantSchema(tenantID string, schemaName string) error {
	// 1. Insert tenant registry mapping
	_, err := db.DB.Exec(`
		INSERT INTO public.tenants (tenant_id, name, schema_name) 
		VALUES ($1, $1, $2) 
		ON CONFLICT (tenant_id) DO NOTHING`, tenantID, schemaName)
	if err != nil {
		return fmt.Errorf("failed to register tenant mapping: %v", err)
	}

	// 2. Create Schema
	_, err = db.DB.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName))
	if err != nil {
		return fmt.Errorf("failed to create tenant schema: %v", err)
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
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, table := range tables {
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (LIKE tenant_default.%s INCLUDING ALL)", schemaName, table, table)
		_, err = tx.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to clone table structure for %s: %v", table, err)
		}
	}

	// 4. Seed metadata and master catalog configurations from template schema
	seeds := []string{
		"doctype_meta",
		"doctype_fields",
		"role_permissions",
		"gl_accounts",
		"prefix_configs",
		"feature_flags",
		"users",
	}

	for _, seedTable := range seeds {
		query := fmt.Sprintf("INSERT INTO %s.%s SELECT * FROM tenant_default.%s ON CONFLICT DO NOTHING", schemaName, seedTable, seedTable)
		_, err = tx.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to seed table data for %s: %v", seedTable, err)
		}
	}

	return tx.Commit()
}
