package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// Stage 49.2.3: MFA assurance tiers. Off by default, report logs, enforce
// refuses a person with no authenticator; administrators are never refused
// here because sign-in already gates them.

func seedTierUser(t *testing.T, id, role string, mfa bool) (pw string, cleanup func()) {
	t.Helper()
	pw = randomTestPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, mfa_enabled, mfa_secret)
		VALUES ($1, $1, $2, $3, $4, 'Active', $5, 'JBSWY3DPEHPK3PXP')`, id, string(hash), id+"@erp.com", role, mfa); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return pw, func() {
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id)
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE user_id = $1`, id)
	}
}

func setTierPolicy(t *testing.T, v string) {
	t.Helper()
	if err := engines.SetSetting("default", engines.SettingMFATierPolicy, v, "test"); err != nil {
		t.Fatalf("set policy: %v", err)
	}
}

func callTiered(t *testing.T, id, role, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	token := engines.SignToken(id, id, role, "default", "HO", 1)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	return doRequest(t, apiMiddleware(ok), method, path, token, nil)
}

func TestMFATiersForActionMapping(t *testing.T) {
	cases := []struct {
		pattern, capability string
		bulk                bool
		want                []string
	}{
		{"POST /api/v1/hr/run-payroll", "hr.payroll", false, []string{engines.MFATierPayroll}},
		{"POST /api/v1/approval/decide", "approval.default", false, []string{engines.MFATierFinanceApproval}},
		{"POST /api/v1/procurement/vendor-invoice/pay", "finance.post", false, []string{engines.MFATierFinanceApproval}},
		{"POST /api/v1/integrations/credentials/rotate", "admin.integrations", false, []string{engines.MFATierConnectorSecret}},
		{"GET /api/v1/marketplace/channels/{channel}/credentials", "oms.default", false, []string{engines.MFATierConnectorSecret}},
		{"GET /api/v1/reports/export/{id}", "reports.default", true, []string{engines.MFATierBulkExport}},
		{"GET /api/v1/hr/payroll-export", "hr.payroll", true, []string{engines.MFATierPayroll, engines.MFATierBulkExport}},
		{"POST /api/v1/admin/security/freeze-exports", "admin.audit-logs", false, []string{engines.MFATierBreakGlass}},
		{"GET /api/v1/inbox", "approval.default", false, nil},
		{"POST /api/v1/finance/journal-voucher", "finance.post", false, nil},
	}
	for _, c := range cases {
		if got := engines.MFATiersForAction(c.pattern, c.capability, c.bulk); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: tiers %v, want %v", c.pattern, got, c.want)
		}
	}
}

func TestMFATierPolicyOffReportEnforce(t *testing.T) {
	db.InitDB(testConnStr())
	defer setTierPolicy(t, "off")
	const path = "/api/v1/reports/export/abc"

	_, cleanNo := seedTierUser(t, "__tier_nomfa__", "Cashier", false)
	defer cleanNo()
	_, cleanYes := seedTierUser(t, "__tier_mfa__", "Cashier", true)
	defer cleanYes()
	_, cleanAdm := seedTierUser(t, "__tier_admin__", "Admin", false)
	defer cleanAdm()

	setTierPolicy(t, "off")
	if rec := callTiered(t, "__tier_nomfa__", "Cashier", "GET", path); rec.Code != http.StatusOK {
		t.Fatalf("off: want 200 got %d %s", rec.Code, rec.Body.String())
	}

	setTierPolicy(t, "report")
	if rec := callTiered(t, "__tier_nomfa__", "Cashier", "GET", path); rec.Code != http.StatusOK {
		t.Fatalf("report: must still run, got %d %s", rec.Code, rec.Body.String())
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE action = 'MFA_TIER' AND status = 'REPORT_WOULD_REFUSE' AND user_id = '__tier_nomfa__'`).Scan(&n)
	if n == 0 {
		t.Fatal("report: no audit row for the action that would be refused")
	}

	setTierPolicy(t, "enforce")
	if rec := callTiered(t, "__tier_nomfa__", "Cashier", "GET", path); rec.Code != http.StatusForbidden {
		t.Fatalf("enforce, no authenticator: want 403 got %d %s", rec.Code, rec.Body.String())
	}
	if rec := callTiered(t, "__tier_mfa__", "Cashier", "GET", path); rec.Code != http.StatusOK {
		t.Fatalf("enforce, enrolled: want 200 got %d %s", rec.Code, rec.Body.String())
	}
	// A route outside every tier is untouched.
	if rec := callTiered(t, "__tier_nomfa__", "Cashier", "GET", "/api/v1/inbox"); rec.Code != http.StatusOK {
		t.Fatalf("enforce, untiered route: want 200 got %d", rec.Code)
	}
	// Administrators are gated at sign-in, not refused here.
	if rec := callTiered(t, "__tier_admin__", "Admin", "GET", path); rec.Code != http.StatusOK {
		t.Fatalf("enforce, admin: want 200 got %d %s", rec.Code, rec.Body.String())
	}
}

func TestMFATierEnforceAsksEnrolledUserForCodeAtLogin(t *testing.T) {
	db.InitDB(testConnStr())
	defer setTierPolicy(t, "off")
	pw, clean := seedTierUser(t, "__tier_login__", "Cashier", true)
	defer clean()
	ip := mfaTestIP()
	login := func() string {
		rec := doRequestFromIP(t, apiMiddleware(handleLogin), "POST", "/api/v1/login", "", map[string]string{"username": "__tier_login__", "password": pw}, ip)
		if rec.Code != http.StatusOK {
			t.Fatalf("login %d %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	setTierPolicy(t, "off")
	if body := login(); !strings.Contains(body, `"token"`) || strings.Contains(body, "mfa_required") {
		t.Fatalf("off: expected a plain session, got %s", body)
	}
	setTierPolicy(t, "enforce")
	if body := login(); !strings.Contains(body, "mfa_required") {
		t.Fatalf("enforce: expected a code challenge, got %s", body)
	}
}

// Recovery codes are stored only as SHA-256 of the normalised code, never the
// code itself (49.2.3: single-use, hashed).
func TestMFARecoveryCodesStoredHashedOnly(t *testing.T) {
	db.InitDB(testConnStr())
	_, clean := seedTierUser(t, "__tier_rc__", "Cashier", true)
	defer clean()
	defer db.DB.Exec(`DELETE FROM tenant_default.mfa_recovery_codes WHERE user_id = '__tier_rc__'`)
	codes, err := engines.GenerateRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if err := engines.ReplaceRecoveryCodes("default", "__tier_rc__", codes); err != nil {
		t.Fatal(err)
	}
	rows, err := db.DB.Query(`SELECT code_hash FROM tenant_default.mfa_recovery_codes WHERE user_id = '__tier_rc__'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	stored := map[string]bool{}
	for rows.Next() {
		var h string
		rows.Scan(&h)
		stored[h] = true
	}
	if len(stored) != len(codes) {
		t.Fatalf("stored %d hashes for %d codes", len(stored), len(codes))
	}
	for _, c := range codes {
		if stored[c] {
			t.Fatalf("raw code %q is stored in the clear", c)
		}
		sum := sha256.Sum256([]byte(strings.ReplaceAll(c, "-", "")))
		if !stored[hex.EncodeToString(sum[:])] {
			t.Fatalf("code %q has no SHA-256 row", c)
		}
	}
	ok, err := engines.ConsumeRecoveryCode("default", "__tier_rc__", codes[0])
	if err != nil || !ok {
		t.Fatalf("first use: %v %v", ok, err)
	}
	if ok, _ := engines.ConsumeRecoveryCode("default", "__tier_rc__", codes[0]); ok {
		t.Fatal("a code worked twice")
	}
}
