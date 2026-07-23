package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 25.8 (SAAS-0193): a per-tenant, keyed limit table rather than a
// single hardcoded plan tier - engines/saas.go's IsFeatureEnabled already
// establishes the "per-tenant row, absent = default behavior" shape for
// module entitlements; this mirrors it for numeric usage limits. An unset
// limit_key means no limit is configured for that tenant, which passes
// (open by default), matching every other optional-config convention in
// this codebase (OPS_ALERT_WEBHOOK_URL unset -> no-op, etc.) rather than
// failing closed on missing configuration.

// CheckTenantLimit looks up tenantID's configured limit for limitKey and
// compares it against currentUsage (the count *after* the action being
// gated would take effect, e.g. existing active users + 1 for a create).
// No configured row is not an error - it means this tenant has no cap on
// that key.
func CheckTenantLimit(tenantID, limitKey string, currentUsage int) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var limitValue int
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT limit_value FROM %s.tenant_limits WHERE tenant_id = $1 AND limit_key = $2`, schema),
		tenantID, limitKey).Scan(&limitValue)
	if err != nil {
		// No row = no limit configured for this tenant/key - not a failure.
		return nil
	}
	if currentUsage > limitValue {
		return &ValidationError{Code: "SAAS-0193", Message: fmt.Sprintf("%s limit of %d reached for this plan (currently %d)", limitKey, limitValue, currentUsage)}
	}
	return nil
}
