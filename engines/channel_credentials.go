package engines

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Channel credential encryption (Stage 16.1). Mirrors engines/auth.go's
// loadOrGenerateJWTSecret pattern exactly: CHANNEL_CREDENTIAL_KEY env var
// wins (production path), else a random key is generated once and persisted
// outside the repo under the OS per-user config dir, stable across
// restarts. This key encrypts channel_credentials.encrypted_payload
// (AES-256-GCM) - the only place a Shopify/BigCommerce/Magento API token
// ever exists in this system outside the operator's own head. No HTTP
// handler in internal/server ever returns a decrypted credential - getChannelCredential
// is package-private by design.
var channelCredKey = loadOrGenerateChannelCredentialKey()

func loadOrGenerateChannelCredentialKey() []byte {
	if v := os.Getenv("CHANNEL_CREDENTIAL_KEY"); v != "" {
		key := []byte(v)
		if len(key) != 32 {
			log.Fatalf("CHANNEL_CREDENTIAL_KEY must be exactly 32 bytes for AES-256, got %d", len(key))
		}
		return key
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("cannot determine user config dir for channel credential key persistence: %v", err)
	}
	keyPath := filepath.Join(configDir, "custom_erp", "channel_cred_key.local")

	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		return data
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		log.Fatalf("failed to generate channel credential key: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		log.Fatalf("failed to create config dir for channel credential key: %v", err)
	}
	if err := os.WriteFile(keyPath, raw, 0600); err != nil {
		log.Fatalf("failed to persist channel credential key: %v", err)
	}
	log.Printf("Generated new local channel credential encryption key at %s - set CHANNEL_CREDENTIAL_KEY env var explicitly for production deployments", keyPath)
	return raw
}

func encryptChannelCredential(fields map[string]string) ([]byte, error) {
	plaintext, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(channelCredKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptChannelCredential(ciphertext []byte) (map[string]string, error) {
	block, err := aes.NewCipher(channelCredKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("stored credential ciphertext is too short")
	}
	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt channel credential (wrong key or tampered data): %v", err)
	}
	var fields map[string]string
	if err := json.Unmarshal(plaintext, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// SaveChannelCredential encrypts and upserts a channel's credential fields
// (e.g. {"access_token": "...", "shop_domain": "mystore.myshopify.com"}).
// Exported since it's called from an HR/Admin-only handler - but that
// handler never reads the value back, only ever writes it.
func SaveChannelCredential(tenantID, channelCode string, fields map[string]string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	encrypted, err := encryptChannelCredential(fields)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.channel_credentials (channel_code, encrypted_payload, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (channel_code) DO UPDATE SET encrypted_payload = EXCLUDED.encrypted_payload, updated_at = CURRENT_TIMESTAMP`, schema),
		channelCode, encrypted)
	return err
}

// getChannelCredential decrypts a channel's stored credential fields.
// Package-private by design (lowercase) - only connector code in this same
// package can ever see a decrypted token.
func getChannelCredential(tenantID, channelCode string) (map[string]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var encrypted []byte
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT encrypted_payload FROM %s.channel_credentials WHERE channel_code = $1`, schema), channelCode).Scan(&encrypted)
	if err != nil {
		return nil, fmt.Errorf("no credentials configured for channel %q", channelCode)
	}
	return decryptChannelCredential(encrypted)
}

// HasChannelCredential reports whether a channel has any credential
// configured, without decrypting it - safe to expose via a status-check
// endpoint (e.g. "Shopify: configured" vs "not configured").
func HasChannelCredential(tenantID, channelCode string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var exists bool
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.channel_credentials WHERE channel_code = $1)`, schema), channelCode).Scan(&exists)
	return exists, err
}

// GetChannelWebhookSecret returns a channel's stored "webhook_secret"
// credential field, for inbound webhook signature verification (Stage
// 16.3 onward). Unlike getChannelCredential (package-private, full
// payload), this is intentionally exported but returns only the one field
// a webhook handler legitimately needs - never the full credential map,
// and never the outbound API access token.
func GetChannelWebhookSecret(tenantID, channelCode string) (string, error) {
	fields, err := getChannelCredential(tenantID, channelCode)
	if err != nil {
		return "", err
	}
	return fields["webhook_secret"], nil
}
