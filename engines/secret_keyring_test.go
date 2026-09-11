package engines

import (
	"bytes"
	"testing"
)

func testKey(b byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = b
	}
	return k
}

// The core rotation promise (Stage 49.6.5): a ciphertext sealed under an
// older key must keep decrypting once a newer key becomes the signer, with
// no batch migration required first.
func TestVersionedKeyringRotationKeepsOldCiphertextReadable(t *testing.T) {
	keyV1 := versionedKey{id: "1", key: testKey(0x11)}
	keyV2 := versionedKey{id: "2", key: testKey(0x22)}

	sealedUnderV1, err := encryptVersioned(keyV1, []byte("secret payload from before rotation"))
	if err != nil {
		t.Fatalf("encrypt under v1: %v", err)
	}

	// Rotation: v2 is now the signer, but v1 is still in the verify ring.
	ring := []versionedKey{keyV2, keyV1}
	got, err := decryptVersioned(ring, sealedUnderV1)
	if err != nil {
		t.Fatalf("decrypt old ciphertext after rotation: %v", err)
	}
	if string(got) != "secret payload from before rotation" {
		t.Errorf("got %q", got)
	}

	sealedUnderV2, err := encryptVersioned(keyV2, []byte("secret payload after rotation"))
	if err != nil {
		t.Fatalf("encrypt under v2: %v", err)
	}
	got, err = decryptVersioned(ring, sealedUnderV2)
	if err != nil {
		t.Fatalf("decrypt new ciphertext: %v", err)
	}
	if string(got) != "secret payload after rotation" {
		t.Errorf("got %q", got)
	}
}

// Pre-rotation ciphertext has no version tag at all - just nonce||ciphertext
// under the one key that ever existed. This is what every row in
// channel_credentials looked like before Stage 49.6.5, and it must keep
// decrypting exactly as it did, forever, with no migration step required
// before rotation can be turned on.
func TestVersionedKeyringReadsPreRotationLegacyFormat(t *testing.T) {
	legacy := versionedKey{id: "", key: testKey(0x33)}
	sealed, err := sealGCM(legacy.key, []byte("pre-rotation plaintext"))
	if err != nil {
		t.Fatalf("sealGCM: %v", err)
	}

	ring := []versionedKey{{id: "1", key: testKey(0x44)}, legacy}
	got, err := decryptVersioned(ring, sealed)
	if err != nil {
		t.Fatalf("decrypt legacy-format ciphertext: %v", err)
	}
	if string(got) != "pre-rotation plaintext" {
		t.Errorf("got %q", got)
	}
}

func TestVersionedKeyringRejectsTamperedCiphertext(t *testing.T) {
	key := versionedKey{id: "1", key: testKey(0x55)}
	sealed, err := encryptVersioned(key, []byte("do not tamper with me"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	tampered := append([]byte{}, sealed...)
	tampered[len(tampered)-1] ^= 0xFF // flip a byte in the GCM tag/ciphertext

	if _, err := decryptVersioned([]versionedKey{key}, tampered); err == nil {
		t.Error("expected tampered ciphertext to fail authentication, decrypted successfully instead")
	}
}

func TestVersionedKeyringRejectsWrongKey(t *testing.T) {
	right := versionedKey{id: "1", key: testKey(0x66)}
	wrong := versionedKey{id: "1", key: testKey(0x77)}
	sealed, err := encryptVersioned(right, []byte("only the right key opens this"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := decryptVersioned([]versionedKey{wrong}, sealed); err == nil {
		t.Error("expected decryption under the wrong key to fail")
	}
}

// loadVersionedKeyring must pick the HIGHEST numbered suffix as the active
// signer and keep every configured key (plus the legacy bare var) able to
// decrypt - the same contract engines/auth.go's loadJWTKeyring already
// proves for session tokens (Stage 29.8), now shared by any at-rest secret.
func TestLoadVersionedKeyringPicksHighestNumberedKeyToSign(t *testing.T) {
	t.Setenv("TESTKEYRING", "")
	t.Setenv("TESTKEYRING_1", string(testKey('a')))
	t.Setenv("TESTKEYRING_2", string(testKey('b')))
	t.Setenv("TESTKEYRING_10", string(testKey('c')))

	keys, signing := loadVersionedKeyring("TESTKEYRING", "test_keyring_unused.local")
	if signing.id != "10" {
		t.Errorf("expected key id 10 (highest suffix) to sign, got %q", signing.id)
	}
	// 3 numbered keys + the legacy trailing key.
	if len(keys) != 4 {
		t.Fatalf("expected 4 keys in the verify ring (3 numbered + legacy), got %d", len(keys))
	}
	if keys[len(keys)-1].id != "" {
		t.Errorf("legacy key must be last in the verify ring, got id %q last", keys[len(keys)-1].id)
	}
}

func TestLoadVersionedKeyringIgnoresNonNumericSuffix(t *testing.T) {
	t.Setenv("TESTKEYRING", "")
	t.Setenv("TESTKEYRING_TYPO", string(testKey('x')))
	t.Setenv("TESTKEYRING_1", string(testKey('y')))

	_, signing := loadVersionedKeyring("TESTKEYRING", "test_keyring_unused2.local")
	if signing.id != "1" {
		t.Errorf("expected only the valid numeric suffix to be picked up, got signer id %q", signing.id)
	}
}

func TestEncryptVersionedTagsCiphertextDifferentlyFromLegacyFormat(t *testing.T) {
	key := versionedKey{id: "1", key: testKey(0x88)}
	versioned, err := encryptVersioned(key, []byte("payload"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !bytes.HasPrefix(versioned, []byte(versionedCiphertextPrefix)) {
		t.Errorf("expected versioned ciphertext to start with %q, got % x", versionedCiphertextPrefix, versioned[:8])
	}
}
