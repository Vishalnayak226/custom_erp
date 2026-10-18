package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 38.2d over HTTP: the client_credentials grant issues a token at
// /api/public/v1/oauth/token, that token works on a data route within its
// scope and is refused outside it, the client secret is refused as a direct
// bearer, other grants are refused, and failures use RFC 6749's vocabulary.
func TestOAuthTokenEndpointAndBearerUse(t *testing.T) {
	handler, admin := moduleHTTPFixture(t)
	issueReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-credentials",
		strings.NewReader(`{"name":"OAuth HTTP test","scopes":["items:read","orders:read"],"auth_mode":"oauth_client"}`))
	issueReq.Header.Set("Authorization", "Bearer "+admin)
	issueReq.Header.Set("Content-Type", "application/json")
	issueReq.Header.Set("X-Tenant-ID", "default")
	issueRec := httptest.NewRecorder()
	handler.ServeHTTP(issueRec, issueReq)
	if issueRec.Code != http.StatusCreated {
		t.Fatalf("issue OAuth client: %d %s", issueRec.Code, issueRec.Body.String())
	}
	var client engines.IssuedOAuthClient
	if err := json.Unmarshal(issueRec.Body.Bytes(), &client); err != nil || client.ClientID == "" || client.TokenEndpoint != engines.OAuthTokenEndpointPath {
		t.Fatalf("issued client body: %s", issueRec.Body.String())
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.api_oauth_tokens WHERE credential_id = $1`, client.Credential.ID)
		db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1`, client.Credential.ID)
	})

	seq := 0
	token := func(form url.Values, basic bool) *httptest.ResponseRecorder {
		seq++
		req := httptest.NewRequest(http.MethodPost, engines.OAuthTokenEndpointPath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Tenant-ID", "default")
		req.RemoteAddr = "198.19.0." + string(rune('0'+seq%10)) + ":5000"
		if basic {
			req.SetBasicAuth(url.QueryEscape(client.ClientID), url.QueryEscape(client.ClientSecret))
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := token(url.Values{"grant_type": {"password"}, "username": {"a"}, "password": {"b"}}, true); rec.Code != 400 || !strings.Contains(rec.Body.String(), "unsupported_grant_type") {
		t.Fatalf("password grant must be refused: %d %s", rec.Code, rec.Body.String())
	}
	if rec := token(url.Values{"grant_type": {"client_credentials"}, "client_id": {client.ClientID}, "client_secret": {"erp_v1_bad"}}, false); rec.Code != 401 || !strings.Contains(rec.Body.String(), "invalid_client") {
		t.Fatalf("bad secret: %d %s", rec.Code, rec.Body.String())
	}
	rec := token(url.Values{"grant_type": {"client_credentials"}, "scope": {"items:read"}}, true)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	var issued engines.OAuthAccessToken
	json.Unmarshal(rec.Body.Bytes(), &issued)
	if issued.TokenType != "Bearer" || issued.Scope != "items:read" {
		t.Fatalf("token body: %s", rec.Body.String())
	}

	call := func(bearer, path string) int {
		seq++
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("X-Tenant-ID", "default")
		req.RemoteAddr = "198.19.1." + string(rune('0'+seq%10)) + ":5000"
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, req)
		return r.Code
	}
	if code := call(issued.AccessToken, "/api/public/v1/items?limit=1"); code != 200 {
		t.Fatalf("token within scope: %d", code)
	}
	if code := call(issued.AccessToken, "/api/public/v1/orders/NOPE/status"); code != 403 {
		t.Fatalf("token outside its narrowed scope: %d, want 403", code)
	}
	if code := call(client.ClientSecret, "/api/public/v1/items?limit=1"); code != 401 {
		t.Fatalf("a client secret is not a bearer: %d, want 401", code)
	}
}
