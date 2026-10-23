package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// TestStage134LoginAbuse (49.2.5): an unknown name and a wrong password get
// the same answer; failed tries from one address back off (another address
// does not); a locked account still lets its owner in from a remembered
// device; and spray / distributed guessing are noticed once per window.
func TestStage134LoginAbuse(t *testing.T) {
	db.InitDB(testConnStr())
	var migrated bool
	_ = db.DB.QueryRow(`SELECT to_regclass('tenant_default.mfa_trusted_devices') IS NOT NULL`).Scan(&migrated)
	if !migrated {
		t.Skip("needs db/migrations_stage125_8_mfa_trusted_devices.sql applied")
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	tenantID, schema := "s134l"+suffix, "tenant_s134l"+suffix
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
		db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantID)
		engines.InvalidateTenantLifecycleCache(tenantID)
	}()
	if _, err := engines.ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	const password = "Login-Abuse-134.5"
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	secret, _ := engines.GenerateTOTPSecret()
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET password_hash = $1, mfa_enabled = TRUE, mfa_secret = $2 WHERE id = 'admin'`, schema), string(hash), secret); err != nil {
		t.Fatal(err)
	}

	login := func(user, pass, device, addr string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"username": user, "password": pass, "device_token": device})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(string(body)))
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.RemoteAddr = addr + ":5000"
		rec := httptest.NewRecorder()
		handleLogin(rec, r)
		return rec
	}

	unknown, wrong := login("nobody-134", "x", "", "203.0.113.1"), login("admin", "wrong", "", "203.0.113.2")
	if unknown.Code != wrong.Code || !strings.Contains(unknown.Body.String(), "USERAC-0021") || !strings.Contains(wrong.Body.String(), "USERAC-0021") {
		t.Fatalf("an unknown name and a wrong password answer alike: %d %s / %d %s", unknown.Code, unknown.Body, wrong.Code, wrong.Body)
	}

	for i := 0; i < 2; i++ {
		if rec := login("admin", "wrong", "", "203.0.113.2"); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("try %d backed off too early", i+2)
		}
	}
	if rec := login("admin", password, "", "203.0.113.2"); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("after 3 failures the next try waits, password unchecked: %d %s", rec.Code, rec.Body)
	}
	if rec := login("Nobody-134", "x", "", "203.0.113.1"); rec.Code == http.StatusTooManyRequests {
		t.Fatal("one failure for a name does not back off")
	}
	if rec := login("admin", password, "", "198.51.100.7"); rec.Code != http.StatusOK {
		t.Fatalf("another address is not held back: %d %s", rec.Code, rec.Body)
	}

	// A remembered device, then the account locked by someone else's guesses.
	code, _ := engines.GenerateTOTPCode(secret)
	vb, _ := json.Marshal(map[string]interface{}{"code": code, "remember_device": true})
	vr := httptest.NewRequest(http.MethodPost, "/api/v1/auth/mfa/verify", strings.NewReader(string(vb)))
	vr.Header.Set("Resolved-Tenant-ID", tenantID)
	vr.Header.Set("Resolved-User-ID", "admin")
	vr.Header.Set("Resolved-Purpose", "mfa_challenge")
	vrec := httptest.NewRecorder()
	handleMFAVerify(vrec, vr)
	var session map[string]interface{}
	_ = json.Unmarshal(vrec.Body.Bytes(), &session)
	device, _ := session["device_token"].(string)
	if device == "" {
		t.Fatalf("no device token: %s", vrec.Body)
	}
	db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET failed_login_count = 10, locked_until = NOW() + interval '30 minutes' WHERE id = 'admin'`, schema))
	if rec := login("admin", password, "", "192.0.2.50"); rec.Code == http.StatusOK {
		t.Fatal("a locked account refuses the password alone")
	}
	if rec := login("admin", "wrong", device, "192.0.2.51"); rec.Code == http.StatusOK {
		t.Fatal("a remembered device does not stand in for the password")
	}
	if rec := login("admin", password, device, "192.0.2.52"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("the owner's remembered device signs in through the lock: %d %s", rec.Code, rec.Body)
	}
	var count int
	var locked bool
	db.DB.QueryRow(fmt.Sprintf(`SELECT failed_login_count, locked_until IS NOT NULL FROM %s.users WHERE id = 'admin'`, schema)).Scan(&count, &locked)
	if count != 0 || locked {
		t.Fatalf("signing in through the lock clears it: count %d locked %v", count, locked)
	}

	sprays := 0
	for i := 0; i < 12; i++ {
		if spray, _ := loginFailed(tenantID, fmt.Sprintf("spray-%d", i), "192.0.2.99"); spray {
			sprays++
		}
	}
	guesses := 0
	for i := 0; i < 12; i++ {
		if _, dist := loginFailed(tenantID, "target-134", fmt.Sprintf("192.0.2.%d", 100+i)); dist {
			guesses++
		}
	}
	if sprays != 1 || guesses != 1 {
		t.Fatalf("each pattern is reported once per window: spray %d, distributed %d", sprays, guesses)
	}
	for d, want := range map[time.Duration]string{time.Second: "1 second", 4 * time.Second: "4 seconds", 61 * time.Second: "2 minutes", 15 * time.Minute: "15 minutes"} {
		if got := loginWaitText(d); got != want {
			t.Errorf("loginWaitText(%v) = %q, want %q", d, got, want)
		}
	}
}
