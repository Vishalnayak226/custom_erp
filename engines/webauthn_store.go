package engines

import (
	"crypto/rand"
	"custom_erp/db"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Passkey storage and one-time challenges (49.2.3). Crypto is in webauthn.go.

const passkeyChallengeTTL = 5 * time.Minute

// MaxPasskeysPerUser keeps the list readable and the lookup bounded.
const MaxPasskeysPerUser = 10

var b64url = base64.RawURLEncoding

// Passkey is what the profile lists; the key itself never leaves the server.
type Passkey struct {
	ID         int        `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// NewPasskeyChallenge stores and returns a one-time challenge (base64url).
func NewPasskeyChallenge(tenantID, userID, purpose string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	ch := b64url.EncodeToString(raw)
	_, _ = db.DB.Exec(fmt.Sprintf("DELETE FROM %s.passkey_challenges WHERE expires_at < now()", schema))
	_, err = db.DB.Exec(fmt.Sprintf(
		"INSERT INTO %s.passkey_challenges (challenge, user_id, purpose, expires_at) VALUES ($1,$2,$3,$4)", schema),
		ch, userID, purpose, time.Now().Add(passkeyChallengeTTL))
	return ch, err
}

// ConsumePasskeyChallenge deletes the challenge and reports whether it was
// this user's, for this purpose, and still fresh. Second use is false.
func ConsumePasskeyChallenge(tenantID, userID, purpose, challenge string) bool {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	res, err := db.DB.Exec(fmt.Sprintf(
		"DELETE FROM %s.passkey_challenges WHERE challenge=$1 AND user_id=$2 AND purpose=$3 AND expires_at > now()", schema),
		challenge, userID, purpose)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// SavePasskey stores a verified registration.
func SavePasskey(tenantID, userID, label string, reg *PasskeyRegistration) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Passkey"
	}
	if r := []rune(label); len(r) > 80 {
		label = string(r[:80])
	}
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s.user_passkeys WHERE user_id=$1", schema), userID).Scan(&n); err != nil {
		return err
	}
	if n >= MaxPasskeysPerUser {
		return errors.New("passkey limit reached")
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		"INSERT INTO %s.user_passkeys (user_id, credential_id, public_key, sign_count, aaguid, label) VALUES ($1,$2,$3,$4,$5,$6)", schema),
		userID, reg.CredentialID, reg.PublicKey, int64(reg.SignCount), reg.AAGUID, label)
	return err
}

// ListPasskeys lists one user's passkeys.
func ListPasskeys(tenantID, userID string) ([]Passkey, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		"SELECT id, label, created_at, last_used_at FROM %s.user_passkeys WHERE user_id=$1 ORDER BY id", schema), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var p Passkey
		var last sql.NullTime
		if err := rows.Scan(&p.ID, &p.Label, &p.CreatedAt, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			t := last.Time
			p.LastUsedAt = &t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PasskeyCredentialIDs lists a user's credential ids (base64url) for the
// browser's allowCredentials; empty means the user has no passkey.
func PasskeyCredentialIDs(tenantID, userID string) ([]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf("SELECT credential_id FROM %s.user_passkeys WHERE user_id=$1 ORDER BY id", schema), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id []byte
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, b64url.EncodeToString(id))
	}
	return out, rows.Err()
}

// DeletePasskey removes one of the user's own passkeys.
func DeletePasskey(tenantID, userID string, id int) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	res, err := db.DB.Exec(fmt.Sprintf("DELETE FROM %s.user_passkeys WHERE id=$1 AND user_id=$2", schema), id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// UsePasskeyAssertion finds the user's credential, verifies the answer and
// advances the sign counter in one statement, so two racing answers cannot
// both pass the counter check.
func UsePasskeyAssertion(tenantID, userID string, credentialID, clientDataJSON, authData, sig []byte, challenge string, o PasskeyOrigin) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var pub []byte
	var count int64
	err = db.DB.QueryRow(fmt.Sprintf(
		"SELECT public_key, sign_count FROM %s.user_passkeys WHERE credential_id=$1 AND user_id=$2", schema),
		credentialID, userID).Scan(&pub, &count)
	if err != nil {
		return errors.New("passkey not recognised")
	}
	newCount, err := VerifyPasskeyAssertion(clientDataJSON, authData, sig, challenge, o, pub, uint32(count))
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(
		"UPDATE %s.user_passkeys SET sign_count=$1, last_used_at=now() WHERE credential_id=$2 AND user_id=$3 AND sign_count=$4", schema),
		int64(newCount), credentialID, userID, count)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("passkey used at the same moment")
	}
	return nil
}

// DecodeB64URL reads base64url with or without padding.
func DecodeB64URL(s string) ([]byte, error) {
	return b64url.DecodeString(strings.TrimRight(s, "="))
}

// EncodeB64URL is base64url without padding, the form WebAuthn uses.
func EncodeB64URL(b []byte) string { return b64url.EncodeToString(b) }
