package engines

import (
	"context"
	"custom_erp/db"
	"testing"
)

// Stage 47.16.3 - the address on a sign-in record, its register row and its
// retention.
func TestStage47163SignInAddressIsRecordedRegisteredAndCleared(t *testing.T) {
	db.InitDB(testConnStr())
	if _, ok := personalDataSystemColumns["users.last_sign_in_ip"]; !ok {
		t.Fatal("users.last_sign_in_ip is personal data and must be in the register")
	}
	const id = "t47163-eng"
	seedLifecycleUser(t, id, RoleCashier, "Active")
	addr := func() (ip, at string) {
		db.DB.QueryRow(`SELECT COALESCE(last_sign_in_ip, ''), COALESCE(last_sign_in_at::text, '') FROM tenant_default.users WHERE id = $1`, id).Scan(&ip, &at)
		return
	}

	// No request: the time is stamped and no address is invented.
	RecordSignIn("default", id)
	if ip, at := addr(); ip != "" || at == "" {
		t.Fatalf("no request: ip %q at %q", ip, at)
	}
	// A request carries the resolved address; garbage is not stored.
	RecordSignInFrom(WithRequestSource(context.Background(), "198.51.100.77", "Mozilla/5.0"), "default", id)
	if ip, _ := addr(); ip != "198.51.100.77" {
		t.Fatalf("address = %q", ip)
	}
	RecordSignInFrom(WithRequestSource(context.Background(), "not-an-ip; DROP", "Mozilla/5.0"), "default", id)
	if ip, _ := addr(); ip != "198.51.100.77" {
		t.Fatalf("a non-address replaced the stored one: %q", ip)
	}

	// Recent sign-in keeps it; one past 180 days loses the address, not the time.
	if n, err := clearUserSignInAddress("default", "tenant_default", auditSourceRetentionDays); err != nil {
		t.Fatal(err)
	} else if ip, _ := addr(); ip == "" {
		t.Fatalf("a recent address was cleared (%d rows)", n)
	}
	db.DB.Exec(`UPDATE tenant_default.users SET last_sign_in_at = NOW() - INTERVAL '181 days' WHERE id = $1`, id)
	if _, err := clearUserSignInAddress("default", "tenant_default", auditSourceRetentionDays); err != nil {
		t.Fatal(err)
	}
	if ip, at := addr(); ip != "" || at == "" {
		t.Fatalf("after 181 days: ip %q at %q (time must stay)", ip, at)
	}
}
