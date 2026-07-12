package engines

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

// SignToken generates a secure, tamper-proof signature for user claims
func SignToken(userID, username, role, tenantID, locationCode string) string {
	exp := time.Now().Add(tokenTTL()).Unix()
	claims := fmt.Sprintf("id=%s&user=%s&role=%s&tenant=%s&loc=%s&exp=%d", userID, username, role, tenantID, locationCode, exp)
	encodedClaims := base64.URLEncoding.EncodeToString([]byte(claims))

	// Create HMAC signature
	h := hmac.New(sha256.New, jwtSecret)
	h.Write([]byte(encodedClaims))
	signature := hex.EncodeToString(h.Sum(nil))

	return encodedClaims + "." + signature
}

// ParseToken validates the signature and extracts claims
func ParseToken(tokenStr string) (map[string]string, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid token format")
	}

	encodedClaims := parts[0]
	signature := parts[1]

	// Verify signature
	h := hmac.New(sha256.New, jwtSecret)
	h.Write([]byte(encodedClaims))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return nil, errors.New("invalid signature")
	}

	// Decode claims
	decodedBytes, err := base64.URLEncoding.DecodeString(encodedClaims)
	if err != nil {
		return nil, err
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
		return nil, errors.New("token missing expiry claim")
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return nil, errors.New("token has malformed expiry claim")
	}
	if time.Now().Unix() > expUnix {
		return nil, errors.New("token expired")
	}

	return claims, nil
}
