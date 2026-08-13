package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const publicAPIKeyMarker = "erp_v1_"

// PublicAPIScopeCatalog is deliberately small and stable. A later Stage 38.1
// route is only public when it names one of these scopes explicitly; an API
// key never inherits a human role or a wildcard session permission.
var PublicAPIScopeCatalog = []string{
	"inventory:read",
	"items:read",
	"orders:read",
	"orders:write",
	"pim:read",
	"pim:write",
	"webhooks:manage",
}

var ErrInvalidAPICredential = errors.New("invalid API credential")

type APICredential struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	KeyPrefix   string     `json:"key_prefix"`
	Scopes      []string   `json:"scopes"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	RotatedFrom string     `json:"rotated_from,omitempty"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
}

type IssuedAPICredential struct {
	Credential APICredential `json:"credential"`
	APIKey     string        `json:"api_key"`
	SecretNote string        `json:"secret_note"`
}

func PublicAPIScopes() []string {
	out := make([]string, len(PublicAPIScopeCatalog))
	copy(out, PublicAPIScopeCatalog)
	return out
}

func normalizePublicAPIScopes(scopes []string) ([]string, error) {
	allowed := make(map[string]bool, len(PublicAPIScopeCatalog))
	for _, scope := range PublicAPIScopeCatalog {
		allowed[scope] = true
	}
	seen := map[string]bool{}
	for _, raw := range scopes {
		scope := strings.ToLower(strings.TrimSpace(raw))
		if scope == "" {
			continue
		}
		if !allowed[scope] {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Scopes", Message: fmt.Sprintf("unsupported public API scope %q", raw)}
		}
		seen[scope] = true
	}
	if len(seen) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "Scopes", Message: "at least one public API scope is required"}
	}
	out := make([]string, 0, len(seen))
	for scope := range seen {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out, nil
}

func validateAPICredentialInput(name string, scopes []string, expiresAt *time.Time) (string, []string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "Name", Message: "API credential name is required"}
	}
	if len(name) > 120 {
		return "", nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Name", Message: "API credential name cannot exceed 120 characters"}
	}
	normalized, err := normalizePublicAPIScopes(scopes)
	if err != nil {
		return "", nil, err
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return "", nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Expires At", Message: "API credential expiry must be in the future"}
	}
	return name, normalized, nil
}

func generatePublicAPIKey() (rawKey, keyPrefix, secretHash string, err error) {
	prefixBytes := make([]byte, 8)
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(prefixBytes); err != nil {
		return "", "", "", err
	}
	if _, err = rand.Read(secretBytes); err != nil {
		return "", "", "", err
	}
	keyPrefix = hex.EncodeToString(prefixBytes)
	rawKey = publicAPIKeyMarker + keyPrefix + "_" + hex.EncodeToString(secretBytes)
	digest := sha256.Sum256([]byte(rawKey))
	return rawKey, keyPrefix, hex.EncodeToString(digest[:]), nil
}

func publicAPIKeyPrefix(rawKey string) (string, error) {
	if !strings.HasPrefix(rawKey, publicAPIKeyMarker) {
		return "", ErrInvalidAPICredential
	}
	parts := strings.Split(strings.TrimPrefix(rawKey, publicAPIKeyMarker), "_")
	if len(parts) != 2 || len(parts[0]) != 16 || len(parts[1]) != 64 {
		return "", ErrInvalidAPICredential
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return "", ErrInvalidAPICredential
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", ErrInvalidAPICredential
	}
	return parts[0], nil
}

func publicAPIKeyMatches(rawKey, storedHash string) bool {
	expected, err := hex.DecodeString(storedHash)
	if err != nil || len(expected) != sha256.Size {
		return false
	}
	actual := sha256.Sum256([]byte(rawKey))
	return subtle.ConstantTimeCompare(expected, actual[:]) == 1
}

type apiCredentialQueryRower interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func insertAPICredential(q apiCredentialQueryRower, schema, name string, scopes []string, expiresAt *time.Time, createdBy string, rotatedFrom *string) (*IssuedAPICredential, error) {
	scopeJSON, err := json.Marshal(scopes)
	if err != nil {
		return nil, err
	}
	var rotated interface{}
	if rotatedFrom != nil && strings.TrimSpace(*rotatedFrom) != "" {
		rotated = strings.TrimSpace(*rotatedFrom)
	}
	for attempt := 0; attempt < 3; attempt++ {
		rawKey, prefix, hash, generateErr := generatePublicAPIKey()
		if generateErr != nil {
			return nil, generateErr
		}
		credential := APICredential{
			Name: name, KeyPrefix: prefix, Scopes: scopes, Status: "Active",
			ExpiresAt: expiresAt, CreatedBy: createdBy,
		}
		err = q.QueryRow(fmt.Sprintf(`INSERT INTO %s.api_credentials
			(name, key_prefix, secret_hash, scopes, expires_at, rotated_from, created_by)
			VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7)
			RETURNING id, created_at`, schema),
			name, prefix, hash, scopeJSON, expiresAt, rotated, createdBy,
		).Scan(&credential.ID, &credential.CreatedAt)
		if err == nil {
			if rotatedFrom != nil {
				credential.RotatedFrom = *rotatedFrom
			}
			return &IssuedAPICredential{
				Credential: credential,
				APIKey:     rawKey,
				SecretNote: "Shown once. Store it securely now; only a one-way digest is retained.",
			}, nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "key_prefix") {
			return nil, err
		}
	}
	return nil, fmt.Errorf("could not allocate a unique API key prefix")
}

func IssueAPICredential(tenantID, name string, scopes []string, expiresAt *time.Time, createdBy string) (*IssuedAPICredential, error) {
	name, scopes, err := validateAPICredentialInput(name, scopes, expiresAt)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return insertAPICredential(db.DB, schema, name, scopes, expiresAt, createdBy, nil)
}

func scanAPICredential(scanner interface{ Scan(...interface{}) error }) (APICredential, error) {
	var credential APICredential
	var scopeJSON []byte
	var expiresAt, lastUsedAt, revokedAt sql.NullTime
	var rotatedFrom sql.NullString
	err := scanner.Scan(
		&credential.ID, &credential.Name, &credential.KeyPrefix, &scopeJSON,
		&credential.Status, &expiresAt, &lastUsedAt, &revokedAt, &rotatedFrom,
		&credential.CreatedBy, &credential.CreatedAt,
	)
	if err != nil {
		return APICredential{}, err
	}
	if err := json.Unmarshal(scopeJSON, &credential.Scopes); err != nil {
		return APICredential{}, fmt.Errorf("API credential %s has invalid stored scopes: %w", credential.ID, err)
	}
	if expiresAt.Valid {
		credential.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		credential.LastUsedAt = &lastUsedAt.Time
	}
	if revokedAt.Valid {
		credential.RevokedAt = &revokedAt.Time
	}
	if rotatedFrom.Valid {
		credential.RotatedFrom = rotatedFrom.String
	}
	return credential, nil
}

const apiCredentialPublicColumns = `id, name, key_prefix, scopes::text, status,
	expires_at, last_used_at, revoked_at, rotated_from::text, created_by, created_at`

func ListAPICredentials(tenantID string) ([]APICredential, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s FROM %s.api_credentials ORDER BY created_at DESC`, apiCredentialPublicColumns, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APICredential{}
	for rows.Next() {
		credential, scanErr := scanAPICredential(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, credential)
	}
	return out, rows.Err()
}

// AuthenticateAPICredential always returns the same public error for an
// unknown prefix, malformed key, revoked key, expired key, or hash mismatch.
// Callers cannot use the endpoint to enumerate which credential prefixes are
// real. This is intentionally separate from user/extension-token parsing.
func AuthenticateAPICredential(tenantID, rawKey string) (*APICredential, error) {
	rawKey = strings.TrimSpace(rawKey)
	prefix, err := publicAPIKeyPrefix(rawKey)
	if err != nil {
		return nil, ErrInvalidAPICredential
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var storedHash string
	row := db.DB.QueryRow(fmt.Sprintf(`SELECT %s, secret_hash FROM %s.api_credentials WHERE key_prefix = $1`, apiCredentialPublicColumns, schema), prefix)
	credential, err := scanAPICredentialWithHash(row, &storedHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrInvalidAPICredential
		}
		return nil, err
	}
	if credential.Status != "Active" || (credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now())) || !publicAPIKeyMatches(rawKey, storedHash) {
		return nil, ErrInvalidAPICredential
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET last_used_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), credential.ID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	credential.LastUsedAt = &now
	return &credential, nil
}

func scanAPICredentialWithHash(scanner interface{ Scan(...interface{}) error }, storedHash *string) (APICredential, error) {
	var credential APICredential
	var scopeJSON []byte
	var expiresAt, lastUsedAt, revokedAt sql.NullTime
	var rotatedFrom sql.NullString
	err := scanner.Scan(
		&credential.ID, &credential.Name, &credential.KeyPrefix, &scopeJSON,
		&credential.Status, &expiresAt, &lastUsedAt, &revokedAt, &rotatedFrom,
		&credential.CreatedBy, &credential.CreatedAt, storedHash,
	)
	if err != nil {
		return APICredential{}, err
	}
	if err := json.Unmarshal(scopeJSON, &credential.Scopes); err != nil {
		return APICredential{}, err
	}
	if expiresAt.Valid {
		credential.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		credential.LastUsedAt = &lastUsedAt.Time
	}
	if revokedAt.Valid {
		credential.RevokedAt = &revokedAt.Time
	}
	if rotatedFrom.Valid {
		credential.RotatedFrom = rotatedFrom.String
	}
	return credential, nil
}

func RevokeAPICredential(tenantID, credentialID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	result, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials
		SET status = 'Revoked', revoked_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'Active'`, schema), credentialID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return &ValidationError{Code: "GLOBAL-0004", Message: "active API credential not found"}
	}
	// Stage 38.3: drop this key's in-process burst window. A revoked key can no
	// longer authenticate anyway; this just stops its spent budget lingering in
	// memory until the window ages out.
	ResetPublicAPICredentialBudget(credentialID)
	publicAPIDailyUsage.forget(credentialID)
	return nil
}

func RotateAPICredential(tenantID, credentialID, rotatedBy string) (*IssuedAPICredential, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var name, scopeRaw, status string
	var expiresAt sql.NullTime
	err = tx.QueryRow(fmt.Sprintf(`SELECT name, scopes::text, status, expires_at
		FROM %s.api_credentials WHERE id = $1 FOR UPDATE`, schema), credentialID).Scan(&name, &scopeRaw, &status, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: "active API credential not found"}
	}
	if err != nil {
		return nil, err
	}
	if status != "Active" {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: "active API credential not found"}
	}
	var scopes []string
	if err := json.Unmarshal([]byte(scopeRaw), &scopes); err != nil {
		return nil, err
	}
	var expiry *time.Time
	if expiresAt.Valid {
		expiry = &expiresAt.Time
		if !expiry.After(time.Now()) {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Expires At", Message: "an expired API credential cannot be rotated; issue a new credential with a new expiry"}
		}
	}
	issued, err := insertAPICredential(tx, schema, name, scopes, expiry, rotatedBy, &credentialID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.api_credentials
		SET status = 'Revoked', revoked_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), credentialID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// The replacement key is a new credential id, so it starts with its own
	// clean budget; the retired one's window is released here rather than left
	// behind. Note the daily quota is deliberately per credential id, so a
	// rotation does grant a fresh daily allowance - rotation is an operator
	// action, not a caller-reachable one, so it cannot be used to evade a quota.
	ResetPublicAPICredentialBudget(credentialID)
	publicAPIDailyUsage.forget(credentialID)
	return issued, nil
}

func APICredentialHasScope(credential *APICredential, required string) bool {
	if credential == nil {
		return false
	}
	for _, scope := range credential.Scopes {
		if subtle.ConstantTimeCompare([]byte(scope), []byte(required)) == 1 {
			return true
		}
	}
	return false
}
