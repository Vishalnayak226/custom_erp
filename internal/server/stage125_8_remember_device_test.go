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

// Stage 125.8: "Remember this device" - a correct code plus the tick box
// gives the browser a token that stands in for the code (never the password)
// until the client's days run out, the password changes, the setting goes to
// 0 or the person forgets their devices.
func TestStage125RememberDevice(t *testing.T) {
	db.InitDB(testConnStr())
	var migrated bool
	_ = db.DB.QueryRow(`SELECT to_regclass('tenant_default.mfa_trusted_devices') IS NOT NULL`).Scan(&migrated)
	if !migrated {
		t.Skip("needs db/migrations_stage125_8_mfa_trusted_devices.sql applied")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	tenantID, schema := "s1258"+suffix, "tenant_s1258"+suffix
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
		db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantID)
		engines.InvalidateTenantLifecycleCache(tenantID)
	}()
	if _, err := engines.ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	const password = "Remember-Device-125.8"
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	secret, _ := engines.GenerateTOTPSecret()
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET password_hash = $1, mfa_enabled = TRUE, mfa_secret = $2 WHERE id = 'admin'`, schema), string(hash), secret); err != nil {
		t.Fatal(err)
	}

	login := func(deviceToken string) map[string]interface{} {
		body, _ := json.Marshal(map[string]string{"username": "admin", "password": password, "device_token": deviceToken})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(string(body)))
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		rec := httptest.NewRecorder()
		handleLogin(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
		}
		out := map[string]interface{}{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	verify := func(remember bool) map[string]interface{} {
		code, _ := engines.GenerateTOTPCode(secret)
		body, _ := json.Marshal(map[string]interface{}{"code": code, "remember_device": remember})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/mfa/verify", strings.NewReader(string(body)))
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-User-ID", "admin")
		r.Header.Set("Resolved-Purpose", "mfa_challenge")
		rec := httptest.NewRecorder()
		handleMFAVerify(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("verify: %d %s", rec.Code, rec.Body.String())
		}
		out := map[string]interface{}{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	asksCode := func(out map[string]interface{}) bool { return out["mfa_required"] == true }

	first := login("")
	if !asksCode(first) || first["remember_days"] != float64(30) {
		t.Fatalf("first login = %v, want the code asked with remember_days 30", first)
	}
	if out := verify(false); out["device_token"] != nil {
		t.Fatalf("a device was remembered without the tick: %v", out)
	}
	session := verify(true)
	token, _ := session["device_token"].(string)
	if token == "" || session["device_days"] != float64(30) {
		t.Fatalf("verify with the tick = %v", session)
	}

	if out := login(token); asksCode(out) || out["token"] == nil {
		t.Fatalf("remembered device still asked for the code: %v", out)
	}
	if !asksCode(login("not-the-token")) {
		t.Fatal("an unknown device token skipped the code")
	}
	if engines.CountMFADevices(tenantID, "admin") != 1 {
		t.Fatal("expected one remembered device")
	}

	// A password change ends it.
	db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET credential_version = credential_version + 1 WHERE id = 'admin'`, schema))
	if !asksCode(login(token)) {
		t.Fatal("a remembered device outlived a password change")
	}

	// Setting 0 ends every remembered device.
	token, _ = verify(true)["device_token"].(string)
	if err := engines.SetSetting(tenantID, engines.SettingMFARememberDays, "0", "test"); err != nil {
		t.Fatal(err)
	}
	if out := login(token); !asksCode(out) || out["remember_days"] != float64(0) {
		t.Fatalf("with the option off: %v", out)
	}
	if out := verify(true); out["device_token"] != nil {
		t.Fatal("a device was remembered with the option off")
	}
	if err := engines.SetSetting(tenantID, engines.SettingMFARememberDays, "30", "test"); err != nil {
		t.Fatal(err)
	}
	if asksCode(login(token)) {
		t.Fatal("turning the option back on did not restore an unexpired device")
	}

	// Forgetting, and a new authenticator, end it too.
	if n, err := engines.ForgetMFADevices(tenantID, "admin"); err != nil || n < 1 {
		t.Fatalf("ForgetMFADevices = %d, %v", n, err)
	}
	if !asksCode(login(token)) {
		t.Fatal("a forgotten device skipped the code")
	}
	token, _ = verify(true)["device_token"].(string)
	db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET mfa_pending_secret = mfa_secret WHERE id = 'admin'`, schema))
	if err := engines.ConfirmReenrollSecret(tenantID, "admin"); err != nil {
		t.Fatal(err)
	}
	if !asksCode(login(token)) {
		t.Fatal("a remembered device outlived a new authenticator")
	}
}
