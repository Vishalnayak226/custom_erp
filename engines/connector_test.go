package engines

import (
	"context"
	"testing"
	"time"
)

// TestChannelCredentialEncryptDecryptRoundTrip verifies the AES-256-GCM
// round trip (Stage 16.1) without touching the database - pure crypto,
// same spirit as the plan's "httptest.Server-backed unit test per
// connector" but for the credential-storage primitive every connector
// depends on.
func TestChannelCredentialEncryptDecryptRoundTrip(t *testing.T) {
	original := map[string]string{
		"access_token": "shpat_abcdef1234567890",
		"shop_domain":  "mystore.myshopify.com",
	}

	encrypted, err := encryptChannelCredential(original)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if len(encrypted) == 0 {
		t.Fatalf("expected non-empty ciphertext")
	}

	decrypted, err := decryptChannelCredential(encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if decrypted["access_token"] != original["access_token"] || decrypted["shop_domain"] != original["shop_domain"] {
		t.Fatalf("round-trip mismatch: got %v, want %v", decrypted, original)
	}

	// Tampering with even one byte must fail to decrypt (GCM's
	// authentication tag catches this) - proves this isn't just XOR
	// obfuscation, an attacker with DB access can't quietly flip bits.
	tampered := append([]byte{}, encrypted...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := decryptChannelCredential(tampered); err == nil {
		t.Fatalf("expected tampered ciphertext to fail decryption, it silently succeeded")
	}
}

// TestResolveConnectorFallsBackToStub confirms the registry's core safety
// property: any Channel without a recognized platform (blank, or a
// platform whose real connector hasn't been registered) resolves to the
// stub, not a nil pointer or a panic - this is what keeps every Stage 15.2
// workflow already verified working unchanged as real connectors land.
func TestResolveConnectorFallsBackToStub(t *testing.T) {
	for _, platform := range []string{"", "Generic", "SomethingNotRegistered"} {
		c := resolveConnector(platform)
		if c == nil {
			t.Fatalf("resolveConnector(%q) returned nil", platform)
		}
		id, err := c.PublishProduct(context.Background(), map[string]string{}, ChannelProductPayload{ItemCode: "TEST-ITEM"})
		if err != nil {
			t.Fatalf("stub PublishProduct returned an error: %v", err)
		}
		if id != "STUB-TEST-ITEM" {
			t.Fatalf("expected stub external id \"STUB-TEST-ITEM\", got %q", id)
		}
	}
}

// TestAllowConnectorCallTokenBucket verifies the outbound rate limiter
// grants exactly `capacity` calls per window and blocks the next one,
// using a channel code unique to this test so it can't collide with any
// other test or a real connector's bucket.
func TestAllowConnectorCallTokenBucket(t *testing.T) {
	channelCode := "TEST-RATE-LIMIT-CHANNEL"
	capacity := 3
	window := time.Hour // long enough that this test can't flake on a slow CI box

	for i := 0; i < capacity; i++ {
		if !allowConnectorCall(channelCode, capacity, window) {
			t.Fatalf("call %d should have been allowed within capacity %d", i+1, capacity)
		}
	}
	if allowConnectorCall(channelCode, capacity, window) {
		t.Fatalf("call beyond capacity should have been blocked")
	}
}
