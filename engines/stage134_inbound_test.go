package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"
)

// TestStage134InboundWebhook (49.8.3): a signature under the current or the
// previous secret passes, anything else fails; a changed secret keeps the old
// one for the overlap; a repeat delivery is claimed once, and a released
// claim (processing failed) lets the platform's retry through.
func TestStage134InboundWebhook(t *testing.T) {
	body := []byte(`{"id":1}`)
	sign := func(secret, enc string) string {
		m := hmac.New(sha256.New, []byte(secret))
		m.Write(body)
		if enc == "base64" {
			return base64.StdEncoding.EncodeToString(m.Sum(nil))
		}
		return hex.EncodeToString(m.Sum(nil))
	}
	if !VerifyWebhookHMACAny(body, sign("new", "base64"), "", "base64", "new", "old") ||
		!VerifyWebhookHMACAny(body, sign("old", "base64"), "", "base64", "new", "old") ||
		!VerifyWebhookHMACAny(body, "sha256="+sign("new", "hex"), "sha256=", "hex", "new") {
		t.Fatal("a signature under the current or previous secret passes")
	}
	if VerifyWebhookHMACAny(body, sign("other", "base64"), "", "base64", "new", "old") ||
		VerifyWebhookHMACAny(body, "", "", "base64", "new") ||
		VerifyWebhookHMACAny(body, sign("new", "hex"), "sha256=", "hex", "new") ||
		VerifyWebhookHMACAny(body, sign("", "hex"), "", "hex", "") {
		t.Fatal("a wrong secret, a blank signature, a missing prefix or a blank secret fails")
	}

	old := map[string]string{"webhook_secret": "s1", "access_token": "t"}
	next := map[string]string{"webhook_secret": "s2", "access_token": "t"}
	keepWebhookSecretOverlap(old, next)
	if next["webhook_secret_previous"] != "s1" || next["webhook_secret_rotated_at"] == "" {
		t.Fatalf("a changed secret keeps the old one: %v", next)
	}
	again := map[string]string{"webhook_secret": "s2", "access_token": "t2"}
	keepWebhookSecretOverlap(next, again)
	if again["webhook_secret_previous"] != "s1" || again["webhook_secret_rotated_at"] != next["webhook_secret_rotated_at"] {
		t.Fatalf("a save that does not change the secret keeps the overlap running: %v", again)
	}

	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`CREATE TABLE IF NOT EXISTS ` + schema + `.inbound_webhook_receipts (source VARCHAR(120) NOT NULL, event_id VARCHAR(200) NOT NULL, received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (source, event_id))`); err != nil {
		t.Fatal(err)
	}
	if err := SaveChannelCredential(tenantID, "SHOP134", map[string]string{"webhook_secret": "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveChannelCredential(tenantID, "SHOP134", map[string]string{"webhook_secret": "s2"}); err != nil {
		t.Fatal(err)
	}
	if got := GetChannelWebhookSecrets(tenantID, "SHOP134"); len(got) != 2 || got[0] != "s2" || got[1] != "s1" {
		t.Fatalf("after a change both secrets are accepted, current first: %v", got)
	}
	f, _ := getChannelCredential(tenantID, "SHOP134")
	f["webhook_secret_rotated_at"] = time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	enc, _ := encryptChannelCredential(f)
	db.DB.Exec(`UPDATE `+schema+`.channel_credentials SET encrypted_payload = $1 WHERE channel_code = 'SHOP134'`, enc)
	if got := GetChannelWebhookSecrets(tenantID, "SHOP134"); len(got) != 1 || got[0] != "s2" {
		t.Fatalf("after the week only the current secret is accepted: %v", got)
	}

	for _, id := range []string{"evt-1", ""} {
		if dup, err := ClaimInboundWebhook(tenantID, "shopify:SHOP134", id, body); err != nil || dup {
			t.Fatalf("first delivery %q is new: %v %v", id, dup, err)
		}
		if dup, _ := ClaimInboundWebhook(tenantID, "shopify:SHOP134", id, body); !dup {
			t.Fatalf("a repeat of %q is a duplicate", id)
		}
		ReleaseInboundWebhook(tenantID, "shopify:SHOP134", id, body)
		if dup, _ := ClaimInboundWebhook(tenantID, "shopify:SHOP134", id, body); dup {
			t.Fatalf("after a failed attempt is released, the retry of %q is processed", id)
		}
	}
	if dup, _ := ClaimInboundWebhook(tenantID, "courier:delhivery", "evt-1", body); dup {
		t.Fatal("the same id from another source is its own delivery")
	}
}
