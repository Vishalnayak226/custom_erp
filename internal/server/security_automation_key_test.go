package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

const testGuideKey = "guide-test-key-195"

func setTestGuideKey(t *testing.T) {
	sum := sha256.Sum256([]byte(testGuideKey))
	t.Setenv(automationKeyEnv, hex.EncodeToString(sum[:]))
}

// 195.1: the key is checked once, taken off the request, and only its hash
// lives on the server; unset (or malformed), the whole feature is off.
func TestStage195AutomationKeyCheck(t *testing.T) {
	req := func(key, automated, ua string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/login", nil)
		if key != "" {
			r.Header.Set(automationKeyHeader, key)
		}
		if automated != "" {
			r.Header.Set(automatedBrowserHeader, automated)
		}
		if ua != "" {
			r.Header.Set("User-Agent", ua)
		}
		return takeAutomationKey(r)
	}
	headless := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/131.0.0.0 Safari/537.36"

	t.Setenv(automationKeyEnv, "")
	if r := req(testGuideKey, "1", headless); hasAutomationKey(r) || automatedSignInRefused(r) {
		t.Fatal("unset: no key is accepted and no browser is refused")
	}
	t.Setenv(automationKeyEnv, "not-a-hash")
	if r := req(testGuideKey, "1", ""); hasAutomationKey(r) || automatedSignInRefused(r) {
		t.Fatal("a malformed hash turns the feature off, it does not lock everyone out")
	}

	setTestGuideKey(t)
	right := req(testGuideKey, "1", headless)
	if !hasAutomationKey(right) || automatedSignInRefused(right) {
		t.Fatal("the right key is accepted and its browser may sign in")
	}
	if right.Header.Get(automationKeyHeader) != "" {
		t.Fatal("the key is taken off the request so nothing downstream can log it")
	}
	if r := req(testGuideKey+"x", "1", ""); hasAutomationKey(r) || !automatedSignInRefused(r) {
		t.Fatal("a wrong key is no key")
	}
	if r := req("", "1", ""); !automatedSignInRefused(r) {
		t.Fatal("an automated browser without the key is refused")
	}
	if r := req("", "", headless); !automatedSignInRefused(r) {
		t.Fatal("a headless browser is refused even when it hides navigator.webdriver")
	}
	if r := req("", "", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"); automatedSignInRefused(r) {
		t.Fatal("an ordinary browser signs in as before")
	}
}

// 195.1: the key lifts the per-minute budget; sign-in's budget stays, so a
// leaked key cannot be used to guess passwords. Everyone else is unchanged.
func TestStage195AutomationKeyLiftsTheRateLimitButNotSignIn(t *testing.T) {
	db.InitDB(testConnStr())
	setTestGuideKey(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	send := func(path, addr, key string) int {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = addr
		if key != "" {
			r.Header.Set(automationKeyHeader, key)
		}
		rec := httptest.NewRecorder()
		apiMiddleware(ok)(rec, r)
		return rec.Code
	}
	stamp := time.Now().UnixNano() % 200

	guide := fmt.Sprintf("10.195.1.%d:1234", stamp)
	for i := 1; i <= 100; i++ {
		if code := send("/api/v1/system/environment", guide, testGuideKey); code == http.StatusTooManyRequests {
			t.Fatalf("call %d with the key was rate limited", i)
		}
	}
	other := fmt.Sprintf("10.195.2.%d:1234", stamp)
	last := 0
	for i := 1; i <= 61; i++ {
		last = send("/api/v1/system/environment", other, testGuideKey+"x")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("a wrong key keeps the ordinary limit: call 61 = %d, want 429", last)
	}
	signIn := fmt.Sprintf("10.195.3.%d:1234", stamp)
	for i := 1; i <= 6; i++ {
		last = send("/api/v1/login", signIn, testGuideKey)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("sign-in keeps 5 a minute even with the key: call 6 = %d, want 429", last)
	}
}

// 195.1: "Same error it should show" - an automated browser without the key
// gets exactly what a wrong password gets, even with the right password, and
// the admin sees why in the audit log. With the key it signs in.
func TestStage195AutomatedSignInGetsTheWrongPasswordError(t *testing.T) {
	db.InitDB(testConnStr())
	setTestGuideKey(t)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	tenantID, schema := "s195a"+suffix, "tenant_s195a"+suffix
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
		db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantID)
		engines.InvalidateTenantLifecycleCache(tenantID)
	}()
	if _, err := engines.ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	const password = "Guide-Key-195.1"
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET password_hash = $1 WHERE id = 'admin'`, schema), string(hash)); err != nil {
		t.Fatal(err)
	}

	login := func(pass, addr, key string, automated bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"username": "admin", "password": pass})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(string(body)))
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.RemoteAddr = addr + ":5000"
		if key != "" {
			r.Header.Set(automationKeyHeader, key)
		}
		if automated {
			r.Header.Set(automatedBrowserHeader, "1")
		}
		rec := httptest.NewRecorder()
		handleLogin(rec, takeAutomationKey(r))
		return rec
	}
	errorOf := func(rec *httptest.ResponseRecorder) (string, string) {
		var b struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &b)
		return b.Code, b.Error
	}

	wrong := login("wrong", "203.0.113.195", "", false)
	bot := login(password, "203.0.113.196", "", true)
	wc, we := errorOf(wrong)
	bc, be := errorOf(bot)
	if bot.Code != wrong.Code || bc != wc || be != we || !strings.Contains(bot.Body.String(), "USERAC-0021") {
		t.Fatalf("a bot with the right password answers like a wrong password:\nbot   %d %s\nwrong %d %s", bot.Code, bot.Body, wrong.Code, wrong.Body)
	}
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.audit_logs WHERE action = 'LOGIN' AND status = 'AUTOMATED_SIGN_IN_REFUSED'`, schema)).Scan(&n)
	if n == 0 {
		t.Fatal("the refusal is written to the audit log for the admin")
	}
	if rec := login(password, "203.0.113.197", testGuideKey, true); rec.Code != http.StatusOK {
		t.Fatalf("with Guide's key the automated browser signs in: %d %s", rec.Code, rec.Body)
	}
	if rec := login(password, "203.0.113.198", "", false); rec.Code != http.StatusOK {
		t.Fatalf("an ordinary browser signs in as before: %d %s", rec.Code, rec.Body)
	}
}
