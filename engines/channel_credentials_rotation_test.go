package engines

import (
	"bytes"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// Stage 49.6.5 - "old-ciphertext migration" and the operator rotation path
// (ReencryptChannelCredentials, wired to `tenantctl reencrypt-channel-credentials`).
// This proves the full lifecycle against a real row, not just the in-memory
// crypto primitives secret_keyring_test.go already covers: a credential saved
// before this keyring existed (bare nonce||ciphertext, no version tag) must
// keep decrypting untouched, and after the operator runs the migration it
// must carry the versioned tag and still decrypt to the same plaintext.
func TestReencryptChannelCredentialsMigratesLegacyFormatToVersioned(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("GetTenantSchema: %v", err)
	}

	channelCode := "test_stage49_6_5_reencrypt"
	defer db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.channel_credentials WHERE channel_code = $1`, schema), channelCode)

	fields := map[string]string{"access_token": "tok_legacy_12345", "shop_domain": "legacy.example.com"}
	plaintext, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Simulate a row written before Stage 49.6.5: sealed with the currently
	// active key, but with none of encryptVersioned's tagging - exactly what
	// every real channel_credentials row looked like before this stage.
	legacySealed, err := sealGCM(channelCredSigningKey.key, plaintext)
	if err != nil {
		t.Fatalf("sealGCM: %v", err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.channel_credentials (channel_code, encrypted_payload) VALUES ($1, $2)
		 ON CONFLICT (channel_code) DO UPDATE SET encrypted_payload = EXCLUDED.encrypted_payload`, schema),
		channelCode, legacySealed); err != nil {
		t.Fatalf("insert legacy-format row: %v", err)
	}

	before, err := getChannelCredential(tenantID, channelCode)
	if err != nil {
		t.Fatalf("legacy-format row must decrypt before migration: %v", err)
	}
	if before["access_token"] != "tok_legacy_12345" {
		t.Fatalf("unexpected pre-migration plaintext: %+v", before)
	}

	migrated, err := ReencryptChannelCredentials(tenantID)
	if err != nil {
		t.Fatalf("ReencryptChannelCredentials: %v", err)
	}
	if migrated < 1 {
		t.Error("expected at least the one seeded credential to be migrated")
	}

	var afterRaw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT encrypted_payload FROM %s.channel_credentials WHERE channel_code = $1`, schema), channelCode).Scan(&afterRaw); err != nil {
		t.Fatalf("re-read migrated row: %v", err)
	}
	if !bytes.HasPrefix(afterRaw, []byte(versionedCiphertextPrefix)) {
		t.Error("expected the re-encrypted row to carry the versioned ciphertext tag")
	}

	after, err := getChannelCredential(tenantID, channelCode)
	if err != nil {
		t.Fatalf("row must still decrypt after migration: %v", err)
	}
	if after["access_token"] != "tok_legacy_12345" || after["shop_domain"] != "legacy.example.com" {
		t.Errorf("plaintext changed across migration: %+v", after)
	}
}
