package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"errors"
	"testing"
)

// Stage 38.2d: an OAuth client's secret is only good at the token endpoint;
// tokens carry at most the client's scopes, are stored as digests, are bound
// to one tenant, and die with the client on rotation or revocation.
func TestOAuthClientCredentialsLifecycle(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	client, err := IssueOAuthClient(tenantID, "Shop sync", []string{"items:read", "orders:read"}, nil, "admin")
	if err != nil {
		t.Fatalf("IssueOAuthClient: %v", err)
	}
	if client.ClientID == "" || client.ClientSecret == "" || client.Credential.AuthMode != "oauth_client" {
		t.Fatalf("issued client incomplete: %+v", client)
	}
	if _, err := AuthenticateAPICredential(tenantID, client.ClientSecret); !errors.Is(err, ErrInvalidAPICredential) {
		t.Fatalf("a client secret must not work as an API key, got %v", err)
	}
	if _, _, err := ExchangeClientCredentials(tenantID, client.ClientID, client.ClientSecret+"0", nil); !errors.Is(err, ErrOAuthInvalidClient) {
		t.Fatalf("wrong secret: %v", err)
	}
	if _, _, err := ExchangeClientCredentials(tenantID, "0000000000000000", client.ClientSecret, nil); !errors.Is(err, ErrOAuthInvalidClient) {
		t.Fatalf("mismatched client id: %v", err)
	}
	if _, _, err := ExchangeClientCredentials(tenantID, client.ClientID, client.ClientSecret, []string{"pim:write"}); !errors.Is(err, ErrOAuthInvalidScope) {
		t.Fatalf("a scope the client lacks: %v", err)
	}

	token, _, err := ExchangeClientCredentials(tenantID, client.ClientID, client.ClientSecret, []string{"items:read"})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if token.TokenType != "Bearer" || token.Scope != "items:read" || token.ExpiresIn <= 0 || token.ExpiresIn > int(OAuthAccessTokenTTL.Seconds()) {
		t.Fatalf("token shape: %+v", token)
	}
	cred, err := AuthenticateOAuthAccessToken(tenantID, token.AccessToken)
	if err != nil || len(cred.Scopes) != 1 || cred.Scopes[0] != "items:read" {
		t.Fatalf("token must carry only its narrowed scope: %+v %v", cred, err)
	}
	if _, err := AuthenticateOAuthAccessToken("default", token.AccessToken); err == nil {
		t.Fatal("a token is bound to the tenant that issued it")
	}
	digest := sha256.Sum256([]byte(token.AccessToken))
	var stored int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".api_oauth_tokens WHERE token_hash = $1", hex.EncodeToString(digest[:])).Scan(&stored)
	if stored != 1 {
		t.Fatal("only the token's digest is stored")
	}

	key, err := IssueAPICredential(tenantID, "Plain key", []string{"items:read"}, nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExchangeClientCredentials(tenantID, key.Credential.KeyPrefix, key.APIKey, nil); !errors.Is(err, ErrOAuthInvalidClient) {
		t.Fatalf("an api_key credential is not an OAuth client: %v", err)
	}

	rotated, err := RotateAPICredential(tenantID, client.Credential.ID, "admin")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.Credential.AuthMode != "oauth_client" {
		t.Fatalf("a rotated client stays an OAuth client, got %q", rotated.Credential.AuthMode)
	}
	if _, err := AuthenticateOAuthAccessToken(tenantID, token.AccessToken); err == nil {
		t.Fatal("tokens from the old secret must die with it")
	}
	if _, _, err := ExchangeClientCredentials(tenantID, client.ClientID, client.ClientSecret, nil); !errors.Is(err, ErrOAuthInvalidClient) {
		t.Fatal("the old secret must stop working at rotation")
	}
	fresh, _, err := ExchangeClientCredentials(tenantID, rotated.Credential.KeyPrefix, rotated.APIKey, nil)
	if err != nil || fresh.Scope != "items:read orders:read" {
		t.Fatalf("new secret, all client scopes by default: %+v %v", fresh, err)
	}
	if err := RevokeAPICredential(tenantID, rotated.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthenticateOAuthAccessToken(tenantID, fresh.AccessToken); err == nil {
		t.Fatal("revoking the client ends its tokens")
	}
}
