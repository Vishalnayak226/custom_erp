package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Stage 47.16.3: a sign-in records the client address the server resolved - the
// socket address when no proxy is trusted, never a header the client wrote.
func TestStage47163SignInRecordsResolvedClientAddress(t *testing.T) {
	db.InitDB(testConnStr())
	const user = "t47163-signin"
	pw := randomTestPassword()
	hash, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, user)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status)
		VALUES ($1, $1, $2, $3, $4, 'Active')`, user, string(hash), user+"@erp.com", engines.RoleCashier); err != nil {
		t.Fatalf("seed: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, user)

	was := trustProxy
	trustProxy = false
	defer func() { trustProxy = was }()

	// A fixed, valid address: mfaTestIP counts past .255 in a full package run,
	// and an address that does not parse is rightly not recorded.
	const ip = "198.51.100.163"
	body, _ := json.Marshal(map[string]string{"username": user, "password": pw})
	req := httptest.NewRequest("POST", "/api/v1/login", bytes.NewReader(body))
	req.RemoteAddr = ip + ":54321"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "6.6.6.6") // a client-written header, never trusted here
	rec := httptest.NewRecorder()
	apiMiddleware(handleLogin)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	var got string
	db.DB.QueryRow(`SELECT COALESCE(last_sign_in_ip, '') FROM tenant_default.users WHERE id = $1`, user).Scan(&got)
	if got != ip {
		t.Fatalf("sign-in address = %q, want the socket address %q (never the forwarded header)", got, ip)
	}

	// The admin Users list shows it.
	list := doRequestFromIP(t, apiMiddleware(handleListUsers), "GET", "/api/v1/admin/users",
		engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin")), nil, mfaTestIP())
	var users []map[string]interface{}
	json.Unmarshal(list.Body.Bytes(), &users)
	shown := ""
	for _, u := range users {
		if u["id"] == user {
			shown, _ = u["last_sign_in_ip"].(string)
		}
	}
	if shown != ip {
		t.Errorf("users list shows address %q, want %q", shown, ip)
	}
}
