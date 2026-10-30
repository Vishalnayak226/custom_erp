package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// 49.8.5: who saved a connector credential, when it was last used, and an
// optional expiry - on channel_credentials (channels, couriers, Shopify,
// the image drive, IRP) and the Pine Labs / CleverTap / Unicommerce tables
// (db/migrations_stage49_8_5_credential_metadata.sql). The connector
// screens show the three as "Owner · Last used · Expires".
//
//   - Owner is the user who last saved it (Save...By), blank for a row saved
//     before this or by a job.
//   - Last used is stamped by the one place a connector reads the credential
//     before a call, at most once an hour (touchCredentialUse).
//   - Expires is optional; an expired credential is refused where it is
//     read, so the connector stops calling instead of calling with a secret
//     the provider would refuse anyway.

// CredentialInfo is what a connector screen may know about a credential:
// never the secret.
type CredentialInfo struct {
	Configured bool   `json:"configured"`
	Owner      string `json:"owner,omitempty"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	Expired    bool   `json:"expired,omitempty"`
}

// parseCredentialExpiry reads an expiry as the screens send it: blank (no
// expiry), a date (expires at the end of that day, UTC) or RFC3339.
func parseCredentialExpiry(s string) (sql.NullTime, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return sql.NullTime{}, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return sql.NullTime{Time: t.Add(24*time.Hour - time.Second), Valid: true}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return sql.NullTime{}, fmt.Errorf("expires_at must be a date (YYYY-MM-DD) or an RFC3339 time")
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}, nil
}

func credentialTimeString(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func credentialExpired(t sql.NullTime) bool {
	return t.Valid && t.Time.Before(time.Now())
}

// touchCredentialUse stamps last_used_at on one credential row, at most once
// an hour; a failure (an older schema) never fails the call.
func touchCredentialUse(schema, table, keyColumn, key string) {
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.%s SET last_used_at = CURRENT_TIMESTAMP
		WHERE %s = $1 AND (last_used_at IS NULL OR last_used_at < CURRENT_TIMESTAMP - interval '1 hour')`, schema, table, keyColumn), key)
}

// credentialInfoRow reads the three columns of one credential row.
func credentialInfoRow(schema, table, keyColumn, key string) (CredentialInfo, error) {
	var owner sql.NullString
	var lastUsed, expires sql.NullTime
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT saved_by, last_used_at, expires_at FROM %s.%s WHERE %s = $1`, schema, table, keyColumn), key).Scan(&owner, &lastUsed, &expires)
	if err == sql.ErrNoRows {
		return CredentialInfo{}, nil
	}
	if err != nil {
		return CredentialInfo{}, err
	}
	return CredentialInfo{Configured: true, Owner: owner.String, LastUsedAt: credentialTimeString(lastUsed), ExpiresAt: credentialTimeString(expires), Expired: credentialExpired(expires)}, nil
}

// GetChannelCredentialInfo is a channel / courier credential's owner, last
// use and expiry for its screen.
func GetChannelCredentialInfo(tenantID, channelCode string) (CredentialInfo, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return CredentialInfo{}, err
	}
	return credentialInfoRow(schema, "channel_credentials", "channel_code", channelCode)
}

// GetCourierCredentialInfo is GetChannelCredentialInfo for a courier provider.
func GetCourierCredentialInfo(tenantID, provider string) (CredentialInfo, error) {
	if _, err := courierAdapter(provider); err != nil {
		return CredentialInfo{}, err
	}
	return GetChannelCredentialInfo(tenantID, courierCredentialCode(provider))
}

// credentialMetaColumns is the SELECT fragment the three list readers share.
const credentialMetaColumns = `COALESCE(saved_by, ''), last_used_at, expires_at`

func credentialMetaFields(out map[string]interface{}, owner string, lastUsed, expires sql.NullTime) {
	out["saved_by"] = owner
	out["last_used_at"] = credentialTimeString(lastUsed)
	out["expires_at"] = credentialTimeString(expires)
	out["expired"] = credentialExpired(expires)
}
