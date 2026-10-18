package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 38.2d - OAuth2 client credentials (RFC 6749 section 4.4) beside the
// opaque API-key branch (38.2a-c), for an integration whose platform expects
// standards-based token exchange.
//
// An OAuth client IS an API credential with auth_mode 'oauth_client', so it
// shares the one inventory: owner, scopes, expiry, rotation, revocation,
// rate budget and traffic log (49.2.7's convergence). client_id is the
// credential's key prefix; client_secret is the full key. The difference is
// where the secret travels: an api_key is sent on every call, an OAuth
// client's secret only to the token endpoint, which returns a bearer token
// that lives 15 minutes, carries at most the client's scopes and dies with
// the client. Only the client_credentials grant exists - no password grant,
// no refresh tokens. Tokens are stored as SHA-256 digests only.

const (
	oauthAccessTokenMarker = "erp_at_"
	// OAuthAccessTokenTTL is short on purpose: a leaked token is useful for
	// minutes, and a client simply asks again.
	OAuthAccessTokenTTL = 15 * time.Minute
)

// ErrOAuthInvalidClient is the one answer for an unknown client, a wrong
// secret, a revoked or expired client, or an api_key used as a client - the
// endpoint cannot be used to learn which.
var ErrOAuthInvalidClient = errors.New("invalid_client")

// ErrOAuthInvalidScope means a requested scope is not one the client holds.
var ErrOAuthInvalidScope = errors.New("invalid_scope")

// IssuedOAuthClient is returned once, at creation or rotation.
type IssuedOAuthClient struct {
	Credential    APICredential `json:"credential"`
	ClientID      string        `json:"client_id"`
	ClientSecret  string        `json:"client_secret"`
	TokenEndpoint string        `json:"token_endpoint"`
	SecretNote    string        `json:"secret_note"`
}

// OAuthAccessToken is the token endpoint's success body.
type OAuthAccessToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// OAuthTokenEndpointPath is where clients exchange their secret.
const OAuthTokenEndpointPath = "/api/public/v1/oauth/token"

// IssueOAuthClient creates an API credential in oauth_client mode.
func IssueOAuthClient(tenantID, name string, scopes []string, expiresAt *time.Time, createdBy string) (*IssuedOAuthClient, error) {
	name, scopes, err := validateAPICredentialInput(name, scopes, expiresAt)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	issued, err := insertAPICredential(tx, schema, name, scopes, expiresAt, createdBy, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET auth_mode = 'oauth_client' WHERE id = $1`, schema), issued.Credential.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	issued.Credential.AuthMode = "oauth_client"
	return OAuthClientFromIssued(issued), nil
}

// OAuthClientFromIssued presents an issued (or rotated) oauth_client
// credential in OAuth vocabulary.
func OAuthClientFromIssued(issued *IssuedAPICredential) *IssuedOAuthClient {
	return &IssuedOAuthClient{
		Credential: issued.Credential, ClientID: issued.Credential.KeyPrefix, ClientSecret: issued.APIKey,
		TokenEndpoint: OAuthTokenEndpointPath,
		SecretNote:    "Shown once. Store the client secret securely now; only a one-way digest is retained.",
	}
}

// ExchangeClientCredentials is the client_credentials grant. requested may
// be empty (all of the client's scopes) or any subset of them.
func ExchangeClientCredentials(tenantID, clientID, clientSecret string, requested []string) (*OAuthAccessToken, *APICredential, error) {
	clientID = strings.TrimSpace(clientID)
	clientSecret = strings.TrimSpace(clientSecret)
	prefix, err := publicAPIKeyPrefix(clientSecret)
	if err != nil || prefix != clientID {
		return nil, nil, ErrOAuthInvalidClient
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, nil, ErrOAuthInvalidClient
	}
	var storedHash string
	row := db.DB.QueryRow(fmt.Sprintf(`SELECT %s, secret_hash FROM %s.api_credentials WHERE key_prefix = $1`, apiCredentialPublicColumns, schema), prefix)
	credential, err := scanAPICredentialWithHash(row, &storedHash)
	if err == sql.ErrNoRows {
		return nil, nil, ErrOAuthInvalidClient
	} else if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	if credential.AuthMode != "oauth_client" || credential.Status != "Active" ||
		(credential.ExpiresAt != nil && !credential.ExpiresAt.After(now)) || !publicAPIKeyMatches(clientSecret, storedHash) {
		return nil, &credential, ErrOAuthInvalidClient
	}

	granted := credential.Scopes
	if len(requested) > 0 {
		held := map[string]bool{}
		for _, s := range credential.Scopes {
			held[s] = true
		}
		granted = []string{}
		seen := map[string]bool{}
		for _, raw := range requested {
			s := strings.ToLower(strings.TrimSpace(raw))
			if s == "" || seen[s] {
				continue
			}
			if !held[s] {
				return nil, &credential, ErrOAuthInvalidScope
			}
			seen[s] = true
			granted = append(granted, s)
		}
		if len(granted) == 0 {
			granted = credential.Scopes
		}
	}

	expiresAt := now.Add(OAuthAccessTokenTTL)
	if credential.ExpiresAt != nil && credential.ExpiresAt.Before(expiresAt) {
		expiresAt = *credential.ExpiresAt // a token never outlives its client
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, nil, err
	}
	token := oauthAccessTokenMarker + hex.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	scopeJSON, _ := json.Marshal(granted)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.api_oauth_tokens (token_hash, credential_id, scopes, expires_at) VALUES ($1, $2, $3::jsonb, $4)`, schema),
		hex.EncodeToString(digest[:]), credential.ID, scopeJSON, expiresAt); err != nil {
		return nil, nil, err
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET last_used_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), credential.ID)
	return &OAuthAccessToken{
		AccessToken: token, TokenType: "Bearer", ExpiresIn: int(time.Until(expiresAt).Seconds()),
		Scope: strings.Join(granted, " "),
	}, &credential, nil
}

// IsOAuthAccessToken tells the public middleware which check a bearer needs.
func IsOAuthAccessToken(bearer string) bool {
	return strings.HasPrefix(strings.TrimSpace(bearer), oauthAccessTokenMarker)
}

// AuthenticateOAuthAccessToken resolves a bearer token to its client, with the
// token's own (possibly narrower) scopes. Same single failure as API keys.
func AuthenticateOAuthAccessToken(tenantID, bearer string) (*APICredential, error) {
	bearer = strings.TrimSpace(bearer)
	if !IsOAuthAccessToken(bearer) || len(bearer) != len(oauthAccessTokenMarker)+64 {
		return nil, ErrInvalidAPICredential
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(bearer))
	var credentialID string
	var tokenScopes []byte
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT credential_id::text, scopes::text FROM %s.api_oauth_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > CURRENT_TIMESTAMP`, schema),
		hex.EncodeToString(digest[:])).Scan(&credentialID, &tokenScopes)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidAPICredential
	} else if err != nil {
		return nil, err
	}
	var storedHash string
	row := db.DB.QueryRow(fmt.Sprintf(`SELECT %s, secret_hash FROM %s.api_credentials WHERE id = $1`, apiCredentialPublicColumns, schema), credentialID)
	credential, err := scanAPICredentialWithHash(row, &storedHash)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidAPICredential
	} else if err != nil {
		return nil, err
	}
	if credential.Status != "Active" || credential.AuthMode != "oauth_client" ||
		(credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now())) {
		return nil, ErrInvalidAPICredential
	}
	if err := json.Unmarshal(tokenScopes, &credential.Scopes); err != nil {
		return nil, ErrInvalidAPICredential
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET last_used_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), credential.ID)
	return &credential, nil
}

type oauthTokenExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// revokeOAuthTokensTx ends every live token a client holds - on revocation
// and on rotation, so the old secret's tokens cannot outlive it.
func revokeOAuthTokensTx(q oauthTokenExecer, schema, credentialID string) error {
	// Checked first, not caught after: inside a transaction a failed statement
	// would abort the caller's whole rotation.
	var present bool
	if err := q.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+".api_oauth_tokens").Scan(&present); err != nil {
		return err
	}
	if !present {
		return nil // binary ahead of its migration: there are no tokens to end
	}
	_, err := q.Exec(fmt.Sprintf(`UPDATE %s.api_oauth_tokens SET revoked_at = CURRENT_TIMESTAMP
		WHERE credential_id = $1::uuid AND revoked_at IS NULL`, schema), credentialID)
	return err
}

// SweepExpiredOAuthTokens deletes tokens that expired more than a day ago;
// the digest is useless after expiry and the table would otherwise only grow.
func SweepExpiredOAuthTokens(tenantID string) (int64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.api_oauth_tokens WHERE expires_at < CURRENT_TIMESTAMP - INTERVAL '1 day'`, schema))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
