package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// VerifyWebhookHMAC generically checks an inbound webhook's signature
// against a shared secret, for platforms beyond the Shopify order webhook
// (internal/server/middleware.go's verifyShopifyWebhookSignature, Stage 14.21-14.24, already
// closes that specific gap with its own base64 HMAC-SHA256 check against
// SHOPIFY_WEBHOOK_SECRET - this is the generic version new inbound
// webhooks, e.g. BigCommerce's, Stage 16.3, should call instead of
// hand-rolling their own). encoding is "base64" (Shopify's convention) or
// "hex" (BigCommerce/most others' convention) - hmac.Equal is used for the
// actual comparison either way, so this is constant-time / safe against
// timing attacks regardless of encoding.
func VerifyWebhookHMAC(payload []byte, signatureHeader, secret, encoding string) bool {
	if signatureHeader == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	computed := mac.Sum(nil)

	if encoding == "base64" {
		expected, err := base64.StdEncoding.DecodeString(signatureHeader)
		if err != nil {
			return false
		}
		return hmac.Equal(computed, expected)
	}

	expected, err := hex.DecodeString(signatureHeader)
	if err != nil {
		return false
	}
	return hmac.Equal(computed, expected)
}
