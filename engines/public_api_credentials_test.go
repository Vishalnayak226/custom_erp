package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPublicAPICredentialPrimitives(t *testing.T) {
	t.Run("scopes are validated deduplicated and sorted", func(t *testing.T) {
		scopes, err := normalizePublicAPIScopes([]string{" PIM:READ ", "items:read", "pim:read"})
		if err != nil {
			t.Fatalf("normalize valid scopes: %v", err)
		}
		if got := strings.Join(scopes, ","); got != "items:read,pim:read" {
			t.Fatalf("normalized scopes = %q", got)
		}
		if _, err := normalizePublicAPIScopes([]string{"admin:*"}); err == nil {
			t.Fatal("unsupported wildcard scope was accepted")
		}
		if _, err := normalizePublicAPIScopes(nil); err == nil {
			t.Fatal("empty scope set was accepted")
		}
	})

	t.Run("generated key is parseable and only its digest matches", func(t *testing.T) {
		rawKey, prefix, hash, err := generatePublicAPIKey()
		if err != nil {
			t.Fatalf("generate API key: %v", err)
		}
		parsedPrefix, err := publicAPIKeyPrefix(rawKey)
		if err != nil || parsedPrefix != prefix {
			t.Fatalf("parsed prefix = %q, %v; want %q", parsedPrefix, err, prefix)
		}
		if !publicAPIKeyMatches(rawKey, hash) {
			t.Fatal("generated API key did not match its digest")
		}
		if publicAPIKeyMatches(rawKey+"0", hash) {
			t.Fatal("tampered API key matched stored digest")
		}
		if _, err := publicAPIKeyPrefix("not-an-api-key"); !errors.Is(err, ErrInvalidAPICredential) {
			t.Fatalf("malformed key error = %v", err)
		}
	})

	t.Run("expiry must be future", func(t *testing.T) {
		past := time.Now().Add(-time.Minute)
		if _, _, err := validateAPICredentialInput("integration", []string{"items:read"}, &past); err == nil {
			t.Fatal("past expiry was accepted")
		}
	})

	t.Run("scope checks never imply a wildcard", func(t *testing.T) {
		credential := &APICredential{Scopes: []string{"items:read"}}
		if !APICredentialHasScope(credential, "items:read") {
			t.Fatal("assigned scope was not recognized")
		}
		if APICredentialHasScope(credential, "orders:read") || APICredentialHasScope(nil, "items:read") {
			t.Fatal("unassigned scope was granted")
		}
	})
}

func TestAPICredentialLifecycle(t *testing.T) {
	db.InitDB(testConnStr())
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "stage38_api_test_" + suffix
	schema := "tenant_stage38_api_test_" + suffix
	if _, err := db.DB.Exec("INSERT INTO public.tenants (tenant_id, name, schema_name) VALUES ($1,$1,$2)", tenantID, schema); err != nil {
		t.Fatalf("register scratch tenant: %v", err)
	}
	if _, err := db.DB.Exec("CREATE SCHEMA " + schema); err != nil {
		_, _ = db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", tenantID)
		t.Fatalf("create scratch schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DROP SCHEMA " + schema + " CASCADE")
		_, _ = db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", tenantID)
	}
	defer cleanup()
	if _, err := db.DB.Exec(`CREATE TABLE ` + schema + `.api_credentials (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name VARCHAR(120) NOT NULL,
		key_prefix VARCHAR(32) NOT NULL UNIQUE, secret_hash CHAR(64) NOT NULL,
		scopes JSONB NOT NULL, status VARCHAR(20) NOT NULL DEFAULT 'Active',
		expires_at TIMESTAMPTZ, last_used_at TIMESTAMPTZ, revoked_at TIMESTAMPTZ,
		rotated_from UUID, created_by VARCHAR(255) NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create scratch credential table: %v", err)
	}

	expires := time.Now().Add(24 * time.Hour).UTC()
	issued, err := IssueAPICredential(tenantID, "Warehouse feed", []string{"items:read", "inventory:read"}, &expires, "stage38-test")
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	if issued.APIKey == "" || !strings.HasPrefix(issued.APIKey, publicAPIKeyMarker+issued.Credential.KeyPrefix+"_") {
		t.Fatalf("issued credential has invalid secret/prefix shape: %#v", issued)
	}
	var storedHash string
	if err := db.DB.QueryRow("SELECT secret_hash FROM "+schema+".api_credentials WHERE id = $1", issued.Credential.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored digest: %v", err)
	}
	if storedHash == issued.APIKey || !publicAPIKeyMatches(issued.APIKey, storedHash) {
		t.Fatal("plaintext key was stored or digest did not verify")
	}

	authenticated, err := AuthenticateAPICredential(tenantID, issued.APIKey)
	if err != nil {
		t.Fatalf("authenticate issued key: %v", err)
	}
	if authenticated.ID != issued.Credential.ID || authenticated.LastUsedAt == nil || !APICredentialHasScope(authenticated, "items:read") {
		t.Fatalf("authenticated credential = %#v", authenticated)
	}
	listed, err := ListAPICredentials(tenantID)
	if err != nil || len(listed) != 1 || listed[0].ID != issued.Credential.ID {
		t.Fatalf("listed credentials = %#v, %v", listed, err)
	}

	rotated, err := RotateAPICredential(tenantID, issued.Credential.ID, "stage38-test")
	if err != nil {
		t.Fatalf("rotate credential: %v", err)
	}
	if rotated.Credential.RotatedFrom != issued.Credential.ID || rotated.APIKey == issued.APIKey {
		t.Fatalf("rotated credential = %#v", rotated)
	}
	if _, err := AuthenticateAPICredential(tenantID, issued.APIKey); !errors.Is(err, ErrInvalidAPICredential) {
		t.Fatalf("old key remained valid after rotation: %v", err)
	}
	if _, err := AuthenticateAPICredential(tenantID, rotated.APIKey); err != nil {
		t.Fatalf("rotated key did not authenticate: %v", err)
	}
	if err := RevokeAPICredential(tenantID, rotated.Credential.ID); err != nil {
		t.Fatalf("revoke rotated key: %v", err)
	}
	if _, err := AuthenticateAPICredential(tenantID, rotated.APIKey); !errors.Is(err, ErrInvalidAPICredential) {
		t.Fatalf("revoked key remained valid: %v", err)
	}
}
