package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// TestCheckTenantLimitNoRowIsNotAnError pins the documented, deliberate
// "absent config = open" behavior that must survive the AUD-06 fix below -
// only sql.ErrNoRows is a pass, not every error.
func TestCheckTenantLimitNoRowIsNotAnError(t *testing.T) {
	db.InitDB(testConnStr())
	if err := CheckTenantLimit("default", "aud06_unconfigured_key", 999999); err != nil {
		t.Fatalf("expected no error for an unconfigured limit key, got: %v", err)
	}
}

// TestCheckTenantLimitStillEnforcesConfiguredLimit is a regression guard for
// the ordinary, unchanged path: a configured limit that is exceeded still
// returns the same *ValidationError (SAAS-0193), not the new operational
// error shape.
func TestCheckTenantLimitStillEnforcesConfiguredLimit(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const key = "aud06_regression_key"
	if _, err := db.DB.Exec(
		"INSERT INTO "+schema+".tenant_limits (tenant_id, limit_key, limit_value) VALUES ($1, $2, $3) "+
			"ON CONFLICT (tenant_id, limit_key) DO UPDATE SET limit_value = EXCLUDED.limit_value",
		"default", key, 5); err != nil {
		t.Fatalf("seed limit: %v", err)
	}
	defer func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".tenant_limits WHERE tenant_id = $1 AND limit_key = $2", "default", key)
	}()

	if err := CheckTenantLimit("default", key, 5); err != nil {
		t.Errorf("usage at the configured limit should pass, got: %v", err)
	}
	err = CheckTenantLimit("default", key, 6)
	if err == nil {
		t.Fatal("expected usage over the configured limit to be rejected")
	}
	verr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError for a genuinely exceeded limit, got %T: %v", err, err)
	}
	if verr.Code != "SAAS-0193" {
		t.Errorf("expected SAAS-0193, got %q", verr.Code)
	}
}

// TestCheckTenantLimitPropagatesOperationalFailure is Stage 50/AUD-06: the
// prior code returned nil (open) for ANY error from the limit-value lookup,
// including one that has nothing to do with "no limit configured" - a
// connection drop, permission failure or, as reproduced here, the table
// itself becoming unreachable mid-request. That silently authorized
// unlimited usage exactly when the limit check was broken. This reproduces
// the failure the same way Stage 50's other AUD fixes reproduce a fault:
// by breaking the one thing under test and confirming the caller can tell,
// then undoing it in the same test regardless of outcome.
func TestCheckTenantLimitPropagatesOperationalFailure(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	if _, err := db.DB.Exec("ALTER TABLE " + schema + ".tenant_limits RENAME TO tenant_limits_aud06_hidden"); err != nil {
		t.Fatalf("hide table: %v", err)
	}
	defer func() {
		_, _ = db.DB.Exec("ALTER TABLE " + schema + ".tenant_limits_aud06_hidden RENAME TO tenant_limits")
	}()

	err = CheckTenantLimit("default", "max_users", 1)
	if err == nil {
		t.Fatal("expected the broken lookup to be reported as an error, not silently treated as no-limit-configured")
	}
	if _, ok := err.(*ValidationError); ok {
		t.Errorf("an operational failure must not be reported as a *ValidationError (limit-reached) - got: %v", err)
	}
	if !strings.Contains(err.Error(), "max_users") {
		t.Errorf("expected the error to name the limit key being checked, got: %v", err)
	}
}
