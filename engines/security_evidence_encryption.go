package engines

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"unicode/utf8"
)

// Stage 49.12.4 (owner's answer: encrypt the evidence pack with a passphrase
// the administrator types at download). The pack leaves the server as an
// OpenSSL "enc" file, so whoever receives it opens it with a tool they already
// have (Git for Windows, macOS and Linux ship openssl) and nothing of ours -
// EvidenceDecryptCommand is the command.
//
// The file is "Salted__", an 8-byte random salt, then the zip in AES-256-CBC
// with PKCS#7 padding; key and IV are PBKDF2-HMAC-SHA256 of the passphrase and
// the salt. CBC carries no tag of its own: a changed file opens to a broken
// zip, and the signed manifest inside still proves every file.

// EvidencePassphraseMinLen is the shortest passphrase the pack takes.
const EvidencePassphraseMinLen = 12

const evidencePBKDF2Iter = 600000

// EvidenceDecryptCommand opens a pack saved as evidence.zip.enc.
var EvidenceDecryptCommand = fmt.Sprintf("openssl enc -d -aes-256-cbc -pbkdf2 -iter %d -md sha256 -in evidence.zip.enc -out evidence.zip", evidencePBKDF2Iter)

// ErrEvidencePassphrase is returned for a passphrase that is too short.
var ErrEvidencePassphrase = fmt.Errorf("type a passphrase of at least %d characters", EvidencePassphraseMinLen)

// EncryptEvidencePack encrypts a pack BuildForensicPack made.
func EncryptEvidencePack(pack []byte, passphrase string) ([]byte, error) {
	if utf8.RuneCountInString(passphrase) < EvidencePassphraseMinLen {
		return nil, ErrEvidencePassphrase
	}
	salt := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	keyIV, err := pbkdf2.Key(sha256.New, passphrase, salt, evidencePBKDF2Iter, 48)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(keyIV[:32])
	if err != nil {
		return nil, err
	}
	pad := aes.BlockSize - len(pack)%aes.BlockSize
	plain := make([]byte, 0, len(pack)+pad)
	plain = append(append(plain, pack...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, 16+len(plain))
	copy(out, "Salted__")
	copy(out[8:16], salt)
	cipher.NewCBCEncrypter(block, keyIV[32:48]).CryptBlocks(out[16:], plain)
	return out, nil
}
