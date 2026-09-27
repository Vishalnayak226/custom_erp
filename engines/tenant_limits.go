package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
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
	if errors.Is(err, sql.ErrNoRows) {
		// No row = no limit configured for this tenant/key - not a failure.
		return nil
	}
	if err != nil {
		// Stage 50/AUD-06: a connection/timeout/permission/query failure is
		// not "no limit configured" - the previous blanket `err != nil ->
		// nil` treated every lookup failure as an absent limit, which
		// silently authorized unlimited usage exactly when the limit check
		// itself was broken. Propagate it so the caller can fail the request
		// instead of the action it was meant to gate.
		return fmt.Errorf("checking %s limit for tenant %s: %w", limitKey, tenantID, err)
	}
	if currentUsage > limitValue {
		return &ValidationError{Code: "SAAS-0193", Message: fmt.Sprintf("%s limit of %d reached for this plan (currently %d)", limitKey, limitValue, currentUsage)}
	}
	return nil
}
