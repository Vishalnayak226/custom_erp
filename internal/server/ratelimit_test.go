package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 141.4: a signed-in session gets 300 "default" calls a minute (one
// setup-screen load is 16, and 60 ran out after a few refreshes), signed-out
// callers keep 60, and the shell's own reads have their own bucket.
func TestStage141SignedInSessionsGetTheLargerDefaultBudget(t *testing.T) {
	if category, limit := rateLimitCategory("/api/v1/branding", http.MethodGet); category != "shell" || limit != 300 {
		t.Fatalf("branding = %s/%d, want shell/300", category, limit)
	}
	if category, _ := rateLimitCategory("/api/v1/branding/logo", http.MethodPost); category == "shell" {
		t.Fatal("only reads belong to the shell bucket")
	}

	db.InitDB(testConnStr())
	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	send := func(addr, auth string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/environment", nil)
		req.RemoteAddr = addr
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		rec := httptest.NewRecorder()
		apiMiddleware(ok)(rec, req)
		return rec.Code
	}
	stamp := time.Now().UnixNano() % 200

	signedIn := fmt.Sprintf("10.141.1.%d:1234", stamp)
	for i := 1; i <= 100; i++ {
		if code := send(signedIn, token); code == http.StatusTooManyRequests {
			t.Fatalf("signed-in call %d was rate limited; the session budget is %d", i, signedInDefaultLimit)
		}
	}

	signedOut := fmt.Sprintf("10.141.2.%d:1234", stamp)
	last := 0
	for i := 1; i <= 61; i++ {
		last = send(signedOut, "")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("signed-out call 61 = %d, want 429 (60 a minute)", last)
	}
}
