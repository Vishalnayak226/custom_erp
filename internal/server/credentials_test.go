package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"strings"
	"testing"
)

// Stage 49.8.5: a connector secret goes in once and never comes back out of
// any credentials read.
func TestStage49ConnectorSecretsAreNeverReadBack(t *testing.T) {
	db.InitDB(testConnStr())
	const secret = "S3cr3t-49-8-5-never-shown"
	// A shared development database may hold a real courier login: leave it alone.
	var existing int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.channel_credentials WHERE channel_code = 'courier:delhivery'`).Scan(&existing)
	if existing == 0 {
		if err := engines.SaveCourierCredential("default", "delhivery", map[string]string{"api_key": secret, "webhook_secret": secret}); err != nil {
			t.Fatalf("courier: %v", err)
		}
		defer db.DB.Exec(`DELETE FROM tenant_default.channel_credentials WHERE channel_code = 'courier:delhivery'`)
	}
	if err := engines.SaveUnicommerceCredential("default", "SEC4985", secret, secret, "https://uc.example.com"); err != nil {
		t.Fatalf("unicommerce: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.unicommerce_credentials WHERE store_code = 'SEC4985'`)
	if err := engines.SavePineLabsCredential("default", "SEC4985", secret, "M4985", "https://pl.example.com"); err != nil {
		t.Fatalf("pinelabs: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.pinelabs_credentials WHERE terminal_id = 'SEC4985'`)
	if err := engines.SaveCleverTapCredential("default", "SEC4985", secret, "in1"); err != nil {
		t.Fatalf("clevertap: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.clevertap_credentials WHERE account_id = 'SEC4985'`)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/marketplace/couriers/{provider}/credentials", apiMiddleware(handleCourierCredentials))
	mux.HandleFunc("GET /api/v1/unicommerce/credentials", apiMiddleware(handleGetUnicommerceCredentials))
	mux.HandleFunc("GET /api/v1/pinelabs/credentials", apiMiddleware(handleGetPineLabsCredentials))
	mux.HandleFunc("GET /api/v1/clevertap/credentials", apiMiddleware(handleGetCleverTapCredentials))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	for _, path := range []string{"/api/v1/marketplace/couriers/delhivery/credentials", "/api/v1/unicommerce/credentials", "/api/v1/pinelabs/credentials", "/api/v1/clevertap/credentials"} {
		rec := doRequestFromIP(t, mux.ServeHTTP, "GET", path, token, nil, mfaTestIP())
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
			continue
		}
		if strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Body.String(), "S3cr3t") {
			t.Errorf("%s returned the secret: %s", path, rec.Body.String())
		}
	}
}
