package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var jwtSecret = []byte("custom_erp_super_secure_secret_key_123!")

// SignToken generates a secure, tamper-proof signature for user claims
func SignToken(userID, username, role, tenantID, locationCode string) string {
	claims := fmt.Sprintf("id=%s&user=%s&role=%s&tenant=%s&loc=%s", userID, username, role, tenantID, locationCode)
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

	return claims, nil
}
