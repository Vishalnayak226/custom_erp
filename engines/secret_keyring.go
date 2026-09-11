package engines

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Stage 49.6.4/49.6.5 - key lifecycle: a reusable rotation-capable AES-256-GCM
// keyring, generalizing the JWT_SECRET_<n> pattern (Stage 29.8,
// engines/auth.go's loadJWTKeyring/jwtKey) to any at-rest secret this
// codebase encrypts. Before this, engines/channel_credentials.go had exactly
// one static key with no rotation path - risk register R-03: replacing
// CHANNEL_CREDENTIAL_KEY invalidates every stored connector credential at
// once, with no way to re-encrypt gradually, and a lost key/host loses the
// data with nothing recoverable. This gives every caller the same
// zero-downtime rotation JWT signing already had: NAME_<n> env vars, the
// highest number encrypts new data, every configured key (plus the legacy
// bare NAME) still decrypts what it wrote - and a tagged ciphertext format so
// a key can be identified without trying every key in the ring.
//
// CSPRNG generation, purpose separation (one keyring per envName - JWT
// signing and channel credentials never share a key or a failure domain),
// least-access storage (32 raw bytes, env var or a 0600 file under the OS
// per-user config dir, never logged - see loadOrGenerateLocalKey), and
// activation/rotation are all satisfied by the shape below. Revocation is
// "delete the old numbered var after the migration window" exactly as
// JWT_SECRET_<n> documents; destruction is deleting the persisted local file
// or unsetting the env var. Dual-control recovery and a live compromise
// drill are process, not code - see docs/security/README.md's key inventory
// for what remains a human decision.

// versionedKey is one AES-256 key in a keyring: an id ("" for the legacy
// single-key var, otherwise the numeric NAME_<n> suffix) and its 32 raw
// bytes.
type versionedKey struct {
	id  string
	key []byte
}

// loadVersionedKeyring resolves a rotation-capable 32-byte AES-256 key set
// for one purpose (envName, e.g. "CHANNEL_CREDENTIAL_KEY"):
//
//   - envName_<n> env vars, newest (highest n) first, are the rotation
//     keyring; the highest-numbered one is the active (encrypting) key.
//   - plain envName is always kept as a trailing legacy verify-only key, so
//     ciphertext written before rotation was configured keeps decrypting -
//     the "old-ciphertext migration" requirement is satisfied by construction
//     rather than needing a batch job before rotation can be turned on.
//   - if neither is set, a random key is generated once and persisted to
//     localFilename under the OS per-user config dir (dev/local convenience,
//     mirroring engines/auth.go's loadOrGenerateJWTSecret).
//
// Every key must be exactly 32 bytes; a wrong-length env value is a fatal
// startup error (matches the pre-existing CHANNEL_CREDENTIAL_KEY behavior)
// rather than a silently truncated/padded key.
func loadVersionedKeyring(envName, localFilename string) (keys []versionedKey, signing versionedKey) {
	legacyRaw := os.Getenv(envName)
	var legacy versionedKey
	if legacyRaw != "" {
		if len(legacyRaw) != 32 {
			log.Fatalf("%s must be exactly 32 bytes for AES-256, got %d", envName, len(legacyRaw))
		}
		legacy = versionedKey{id: "", key: []byte(legacyRaw)}
	} else {
		legacy = versionedKey{id: "", key: loadOrGenerateLocalKey(envName, localFilename)}
	}

	type numbered struct {
		n   int
		key versionedKey
	}
	var found []numbered
	prefix := envName + "_"
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name, val := kv[:eq], kv[eq+1:]
		if !strings.HasPrefix(name, prefix) || val == "" {
			continue
		}
		// Suffix must be a positive integer, exactly like loadJWTKeyring: a
		// typo'd or unrelated NAME_-prefixed var is ignored rather than
		// treated as a key.
		suffix := strings.TrimPrefix(name, prefix)
		n, err := strconv.Atoi(suffix)
		if err != nil || n <= 0 {
			continue
		}
		if len(val) != 32 {
			log.Fatalf("%s must be exactly 32 bytes for AES-256, got %d", name, len(val))
		}
		found = append(found, numbered{n: n, key: versionedKey{id: suffix, key: []byte(val)}})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].n > found[j].n })

	if len(found) == 0 {
		return []versionedKey{legacy}, legacy
	}
	keys = make([]versionedKey, 0, len(found)+1)
	for _, f := range found {
		keys = append(keys, f.key)
	}
	keys = append(keys, legacy)
	log.Printf("%s rotation active: encrypting with %s_%s, %d key(s) accepted for decryption", envName, envName, keys[0].id, len(keys))
	return keys, keys[0]
}

func loadOrGenerateLocalKey(envName, localFilename string) []byte {
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("cannot determine user config dir for %s persistence: %v", envName, err)
	}
	keyPath := filepath.Join(configDir, "custom_erp", localFilename)

	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		return data
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		log.Fatalf("failed to generate %s: %v", envName, err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		log.Fatalf("failed to create config dir for %s: %v", envName, err)
	}
	if err := os.WriteFile(keyPath, raw, 0600); err != nil {
		log.Fatalf("failed to persist %s: %v", envName, err)
	}
	log.Printf("Generated new local %s at %s - set %s env var explicitly for production deployments", envName, keyPath, envName)
	return raw
}

// versionedCiphertextPrefix tags a versioned-format blob so a legacy
// (pre-rotation) ciphertext - which is just nonce||ciphertext under the one
// key that ever existed - is never misread as the new format. The literal is
// checked before any key lookup, and even a coincidental byte match on old
// ciphertext cannot succeed silently: GCM's authentication tag rejects
// decryption under the wrong key, so a misidentified blob just falls through
// to the legacy attempt below instead of producing wrong plaintext.
const versionedCiphertextPrefix = "kv1:"

// encryptVersioned seals plaintext under the keyring's active key (highest
// numbered, or legacy if no numbered key is configured) and tags the result
// with that key's id, so decryptVersioned can find the right key directly
// instead of trying every key in the ring.
func encryptVersioned(signing versionedKey, plaintext []byte) ([]byte, error) {
	sealed, err := sealGCM(signing.key, plaintext)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(versionedCiphertextPrefix)+1+len(signing.id)+len(sealed))
	out = append(out, versionedCiphertextPrefix...)
	out = append(out, byte(len(signing.id)))
	out = append(out, signing.id...)
	out = append(out, sealed...)
	return out, nil
}

// decryptVersioned opens ciphertext produced by encryptVersioned OR by the
// pre-rotation single-key format (bare nonce||ciphertext, no prefix), so
// every row written before this keyring existed keeps decrypting with no
// batch migration required first.
func decryptVersioned(keys []versionedKey, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) > len(versionedCiphertextPrefix)+1 && string(ciphertext[:len(versionedCiphertextPrefix)]) == versionedCiphertextPrefix {
		rest := ciphertext[len(versionedCiphertextPrefix):]
		idLen := int(rest[0])
		if len(rest) >= 1+idLen {
			id := string(rest[1 : 1+idLen])
			sealed := rest[1+idLen:]
			for _, k := range keys {
				if k.id == id {
					if pt, err := openGCM(k.key, sealed); err == nil {
						return pt, nil
					}
					break
				}
			}
		}
		// Falls through deliberately: an id that names no configured key, or
		// that fails to authenticate, might still (vanishingly rarely) be
		// legacy ciphertext that happened to start with the tag bytes -
		// never assumed, always re-tried against every key below.
	}
	for _, k := range keys {
		if pt, err := openGCM(k.key, ciphertext); err == nil {
			return pt, nil
		}
	}
	return nil, errors.New("ciphertext does not decrypt under any configured key (wrong key, or tampered data)")
}

func sealGCM(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func openGCM(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, sealed, nil)
}
