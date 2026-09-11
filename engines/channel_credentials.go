package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
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
//
// Stage 49.6.4/49.6.5: this used to be exactly one static key (risk register
// R-03 - no rotation path, and a key that exists only on one host's config
// dir with nothing in any backup). It now goes through the shared
// engines/secret_keyring.go rotation keyring: CHANNEL_CREDENTIAL_KEY_<n> env
// vars, newest wins for new writes, every configured key (plus the legacy
// bare CHANNEL_CREDENTIAL_KEY) still decrypts what it wrote. Unset, this
// behaves byte-for-byte as before - a deployment that never rotates sees no
// change at all.
var channelCredKeys, channelCredSigningKey = loadVersionedKeyring("CHANNEL_CREDENTIAL_KEY", "channel_cred_key.local")

func encryptChannelCredential(fields map[string]string) ([]byte, error) {
	plaintext, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return encryptVersioned(channelCredSigningKey, plaintext)
}

func decryptChannelCredential(ciphertext []byte) (map[string]string, error) {
	plaintext, err := decryptVersioned(channelCredKeys, ciphertext)
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

// ReencryptChannelCredentials (Stage 49.6.5 - "old-ciphertext migration") is
// the operator step that actually completes a key rotation: every stored
// credential is decrypted under whichever key wrote it (legacy or any
// numbered key still configured) and re-saved under the CURRENT signing key.
// Without this, an old key can never be retired - decryptVersioned's
// backward compatibility means old ciphertext keeps working forever, which
// is the safe default but not itself a rotation. Exposed via
// `tenantctl reencrypt-channel-credentials`; no HTTP route, matching the
// rest of this codebase's platform-level-authority-has-no-route rule
// (cmd/tenantctl's file header).
//
// Idempotent and safe to re-run: a credential already sealed under the
// current signing key is decrypted and re-sealed (a fresh nonce, same
// plaintext) rather than skipped, so a partially-completed run can simply be
// repeated.
func ReencryptChannelCredentials(tenantID string) (migrated int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT channel_code, encrypted_payload FROM %s.channel_credentials`, schema))
	if err != nil {
		return 0, err
	}
	type row struct {
		code      string
		encrypted []byte
	}
	var all []row
	for rows.Next() {
		var r row
		if scanErr := rows.Scan(&r.code, &r.encrypted); scanErr != nil {
			rows.Close()
			return 0, scanErr
		}
		all = append(all, r)
	}
	rows.Close()

	for _, r := range all {
		fields, decErr := decryptChannelCredential(r.encrypted)
		if decErr != nil {
			return migrated, fmt.Errorf("channel %q: %v", r.code, decErr)
		}
		if err := SaveChannelCredential(tenantID, r.code, fields); err != nil {
			return migrated, fmt.Errorf("channel %q: %v", r.code, err)
		}
		migrated++
	}
	return migrated, nil
}
