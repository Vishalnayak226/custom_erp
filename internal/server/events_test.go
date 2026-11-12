package server

import (
	"custom_erp/db"
	"testing"
)

// Stage 49.11.1: the lock that repeated failed sign-ins put on an account is
// its own audit event (LOGIN / ACCOUNT_LOCKED), next to the failures.
func TestStage49AccountLockIsAnAuditEvent(t *testing.T) {
	db.InitDB(testConnStr())
	const user = "stage49-lock-event"
	_, cleanup := seedMFATestUser(t, user)
	defer cleanup()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE user_id = $1 AND action = 'LOGIN' AND status = 'ACCOUNT_LOCKED'`, user)
	threshold := accountLockoutThresholdFor("default")
	for i := 0; i < threshold; i++ {
		doRequestFromIP(t, apiMiddleware(handleLogin), "POST", "/api/v1/login", "", map[string]string{"username": user, "password": "wrong-password-49111"}, mfaTestIP())
	}
	var events int
	if err := db.DB.QueryRow(`SELECT count(*) FROM tenant_default.audit_logs WHERE user_id = $1 AND action = 'LOGIN' AND status = 'ACCOUNT_LOCKED'`, user).Scan(&events); err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if events != 1 {
		t.Fatalf("%d ACCOUNT_LOCKED events after %d failed sign-ins, want 1", events, threshold)
	}
}
