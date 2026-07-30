package engines

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var jwtSecret = loadOrGenerateJWTSecret()

const defaultTokenTTL = 24 * time.Hour

// tokenTTL returns the session lifetime: JWT_EXPIRY_HOURS overrides the
// default if set to a valid positive integer, otherwise 24h - long enough
// to cover a normal shift/session without a refresh-token mechanism, short
// enough that a leaked token doesn't stay valid indefinitely.
func tokenTTL() time.Duration {
	if v := os.Getenv("JWT_EXPIRY_HOURS"); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return defaultTokenTTL
}

// loadOrGenerateJWTSecret resolves the HMAC signing key: an explicit JWT_SECRET
// env var always wins (the production path). Otherwise a random secret is
// generated once and persisted outside the repo, under the OS per-user config
// dir - never in source, never in the project working directory, and stable
// across restarts so existing sessions don't get invalidated every redeploy.
func loadOrGenerateJWTSecret() []byte {
	if v := os.Getenv("JWT_SECRET"); v != "" {
		return []byte(v)
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("cannot determine user config dir for JWT secret persistence: %v", err)
	}
	secretPath := filepath.Join(configDir, "custom_erp", "jwt_secret.local")

	if data, err := os.ReadFile(secretPath); err == nil && len(data) > 0 {
		return data
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		log.Fatalf("failed to generate JWT signing secret: %v", err)
	}
	secret := []byte(hex.EncodeToString(raw))

	if err := os.MkdirAll(filepath.Dir(secretPath), 0700); err != nil {
		log.Fatalf("failed to create config dir for JWT secret: %v", err)
	}
	if err := os.WriteFile(secretPath, secret, 0600); err != nil {
		log.Fatalf("failed to persist JWT signing secret: %v", err)
	}
	log.Printf("Generated new local JWT signing secret at %s - set JWT_SECRET env var explicitly for production deployments", secretPath)
	return secret
}

// --- Signing-key rotation (Stage 29.8) ------------------------------------
//
// Before this, JWT_SECRET was a single static key: the only way to retire a
// leaked key was to replace it, which invalidates every live session at once.
// That is a usable break-glass but not a rotation story. Numbered keys give
// one:
//
//	JWT_SECRET_1=<current key>    # keeps verifying tokens already issued
//	JWT_SECRET_2=<new key>        # highest number signs every NEW token
//
// Deploy with both set, wait out one token TTL (JWT_EXPIRY_HOURS, default
// 24h) so every live token has been reissued under the new key, then delete
// JWT_SECRET_1. Nobody is logged out at any point. Plain JWT_SECRET stays
// supported and unchanged as the legacy/default key - if no numbered key is
// set, signing and verification behave exactly as they did before, byte for
// byte, so this is inert until someone opts into it.
type jwtKey struct {
	kid    string // the token's "kid" claim; "" for the legacy JWT_SECRET
	secret []byte
}

// jwtKeys is every key a token may legitimately be signed with (verification
// set); jwtSigningKey is the one new tokens are signed with.
var jwtKeys, jwtSigningKey = loadJWTKeyring()

// loadJWTKeyring collects JWT_SECRET_<n> from the environment, newest (highest
// n) first, and always keeps plain JWT_SECRET as a trailing verify key so
// tokens issued before rotation was configured keep working.
func loadJWTKeyring() ([]jwtKey, jwtKey) {
	type numbered struct {
		n   int
		key jwtKey
	}
	var found []numbered
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name, val := kv[:eq], kv[eq+1:]
		if !strings.HasPrefix(name, "JWT_SECRET_") || val == "" {
			continue
		}
		// Suffix must be a positive integer: this deliberately ignores
		// unrelated JWT_SECRET_-prefixed names (and JWT_EXPIRY_HOURS, which
		// doesn't match the prefix at all) rather than treating a typo as a
		// signing key.
		suffix := strings.TrimPrefix(name, "JWT_SECRET_")
		n, err := strconv.Atoi(suffix)
		if err != nil || n <= 0 {
			continue
		}
		found = append(found, numbered{n: n, key: jwtKey{kid: suffix, secret: []byte(val)}})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].n > found[j].n })

	legacy := jwtKey{kid: "", secret: jwtSecret}
	if len(found) == 0 {
		return []jwtKey{legacy}, legacy
	}
	keys := make([]jwtKey, 0, len(found)+1)
	for _, f := range found {
		keys = append(keys, f.key)
	}
	keys = append(keys, legacy)
	log.Printf("JWT signing-key rotation active: signing with JWT_SECRET_%s, %d key(s) accepted for verification", keys[0].kid, len(keys))
	return keys, keys[0]
}

// signClaims base64s and HMACs a claims string with the active signing key -
// the one place the signature is produced, shared by all three token issuers
// so a rotation can never apply to some token kinds and not others.
func signClaims(claims string) string {
	encodedClaims := base64.URLEncoding.EncodeToString([]byte(claims))
	h := hmac.New(sha256.New, jwtSigningKey.secret)
	h.Write([]byte(encodedClaims))
	return encodedClaims + "." + hex.EncodeToString(h.Sum(nil))
}

// kidSuffix returns the "&kid=<n>" claim fragment to append when a numbered
// key is signing. Empty when running on the legacy single-key setup, which
// keeps the emitted token byte-identical to the pre-rotation format.
func kidSuffix() string {
	if jwtSigningKey.kid == "" {
		return ""
	}
	return "&kid=" + jwtSigningKey.kid
}

// verifyClaimSignature checks encodedClaims/signature against every key in the
// ring, returning true on the first match.
//
// It deliberately tries all keys rather than selecting one by the token's own
// kid claim: kid lives inside the payload, so it is attacker-controlled until
// the HMAC has already been verified, and keying off it would mean trusting
// unverified input to pick the trust anchor. With a handful of keys the extra
// comparisons cost single-digit microseconds. kid is still emitted, so ops can
// tell which key signed a given token; it is just never load-bearing.
func verifyClaimSignature(encodedClaims, signature string) bool {
	for _, k := range jwtKeys {
		h := hmac.New(sha256.New, k.secret)
		h.Write([]byte(encodedClaims))
		if hmac.Equal([]byte(signature), []byte(hex.EncodeToString(h.Sum(nil)))) {
			return true
		}
	}
	return false
}

// newJTI (24.19) mints a random per-token identifier - cheap, stdlib-only
// crypto/rand, giving each issued token a unique ID a future revocation-list
// check could key off, without adopting a JWT library for it.
func newJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// SignToken generates a secure, tamper-proof signature for user claims.
// 24.19: carries iat (issued-at) and jti (unique token ID) alongside the
// existing claims - two more fields in the same stdlib HMAC scheme, not a
// switch to golang-jwt (this repo's lightweight-first principle) - so a
// token has a verifiable issue time and a per-token identity a future
// revocation hook could check, without full RFC 7519 header compliance.
func SignToken(userID, username, role, tenantID, locationCode string) string {
	now := time.Now()
	exp := now.Add(tokenTTL()).Unix()
	claims := fmt.Sprintf("id=%s&user=%s&role=%s&tenant=%s&loc=%s&iat=%d&jti=%s%s&exp=%d", userID, username, role, tenantID, locationCode, now.Unix(), newJTI(), kidSuffix(), exp)
	return signClaims(claims)
}

// SignPurposeToken issues a short-lived, narrowly-scoped token for a single
// step of a multi-step flow (MFA enrollment/challenge). Deliberately excludes
// role/loc so it can never be mistaken for - or misused as - a full session
// token; apiMiddleware surfaces its "purpose" claim via the Resolved-Purpose
// header for handlers to check explicitly. tenantID IS included (unlike
// role/loc) because apiMiddleware unconditionally re-resolves tenantID from
// claims["tenant"] for any bearer token - omitting it would silently blank
// out tenant scoping for the rest of the MFA flow.
func SignPurposeToken(userID, username, tenantID, purpose string, ttl time.Duration) string {
	now := time.Now()
	exp := now.Add(ttl).Unix()
	claims := fmt.Sprintf("id=%s&user=%s&tenant=%s&purpose=%s&iat=%d&jti=%s%s&exp=%d", userID, username, tenantID, purpose, now.Unix(), newJTI(), kidSuffix(), exp)
	return signClaims(claims)
}

// SignExtensionToken (Stage 14.17-14.20) issues a short-lived token scoped to
// exactly one tenant and one doctype, for handing to a 3rd-party developer's
// extension hook so it can read back the data it needs without ever
// receiving core source or another tenant's credentials. Same purpose-token
// shape as SignPurposeToken (no role/loc, purpose="extension" surfaced via
// Resolved-Purpose) plus one extra claim, scope_doctype, that
// handleGenericDoc enforces explicitly: read-only, and only for the exact
// doctype named here.
func SignExtensionToken(tenantID, scopeDoctype string, ttl time.Duration) string {
	now := time.Now()
	exp := now.Add(ttl).Unix()
	claims := fmt.Sprintf("tenant=%s&purpose=extension&scope_doctype=%s&iat=%d&jti=%s%s&exp=%d", tenantID, scopeDoctype, now.Unix(), newJTI(), kidSuffix(), exp)
	return signClaims(claims)
}

// errInvalidToken (24.22) is the single generic error ParseToken returns for
// every failure mode - malformed, bad signature, missing/malformed expiry,
// or genuinely expired. The source-doc finding: returning distinct text per
// case is a minor information leak (a caller could distinguish "this token
// is garbage" from "this token was once valid but expired"). Low
// timing/enumeration risk in practice for this codebase's threat model, but
// the fix is nearly free.
var errInvalidToken = errors.New("invalid token")

// ParseToken validates the signature and extracts claims
func ParseToken(tokenStr string) (map[string]string, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 2 {
		return nil, errInvalidToken
	}

	encodedClaims := parts[0]
	signature := parts[1]

	// Verify signature against every key in the rotation ring (see
	// verifyClaimSignature on why kid is not used to pick one).
	if !verifyClaimSignature(encodedClaims, signature) {
		return nil, errInvalidToken
	}

	// Decode claims
	decodedBytes, err := base64.URLEncoding.DecodeString(encodedClaims)
	if err != nil {
		return nil, errInvalidToken
	}

	claimsStr := string(decodedBytes)
	claims := make(map[string]string)
	pairs := strings.Split(claimsStr, "&")

	for _, pair := range pairs {
		kv := strings.Split(pair, "=")
		if len(kv) == 2 {
			claims[kv[0]] = kv[1]
		}
	}

	// Enforce expiry
	expStr, ok := claims["exp"]
	if !ok {
		return nil, errInvalidToken
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return nil, errInvalidToken
	}
	if time.Now().Unix() > expUnix {
		return nil, errInvalidToken
	}

	return claims, nil
}

// seedAdminPasswordHash is the literal bcrypt hash db/migration.sql seeds
// tenant_default.users.admin with (loophole #27, checklist 24.27) - a known
// default credential, harmless for this project's current single-machine
// dev setup (real dev logins already use DEV_CREDENTIALS.local.txt's
// separate rotated credentials) but unsafe to ship into a real production
// deployment unrotated.
const seedAdminPasswordHash = "$2a$10$8IqlLMaxVylUfYsKtF2bxOsN8udFN3XKEeSVbHWuRmMToWCvHuv6W"

// EnforceNoDefaultAdminCredentialInProduction (24.27) checks whether
// tenant_default's seed admin account still carries the exact hash
// db/migration.sql ships, and refuses to start when ENV=production - a
// migration script can't safely generate its own random credential in
// plain SQL without a new Postgres extension (pgcrypto's crypt()/gen_salt()
// aren't already in use anywhere in this codebase), so the fix is a
// startup-time guard at the one place every deployment already passes
// through (Run(), right after InitDB) rather than a smarter migration.
// Outside production this only logs a warning - the dev-bootstrap seed
// staying in place is expected and matches the current single-machine dev
// workflow (ai_handover.md §1).
func EnforceNoDefaultAdminCredentialInProduction() error {
	var hash string
	err := db.DB.QueryRow(`SELECT password_hash FROM tenant_default.users WHERE username = 'admin'`).Scan(&hash)
	if err != nil {
		// No seed admin row (already rotated/removed, or a fresh schema this
		// migration hasn't been applied to yet) - nothing to enforce.
		return nil
	}
	if hash != seedAdminPasswordHash {
		return nil
	}
	if os.Getenv("ENV") == "production" {
		return fmt.Errorf("tenant_default's 'admin' account still has the default seed password from db/migration.sql - rotate it (e.g. via POST /api/v1/me/change-password after a one-time login, or UPDATE tenant_default.users directly) before running with ENV=production")
	}
	log.Println("[SECURITY] tenant_default's 'admin' account still has the default seed password from db/migration.sql - fine for local dev, but rotate it before any real deployment (ENV=production refuses to start with this credential still active).")
	return nil
}
