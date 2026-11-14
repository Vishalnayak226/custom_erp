package engines

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 49.12.4: the evidence pack leaves encrypted with the typed passphrase, in
// the OpenSSL "enc" layout EvidenceDecryptCommand opens; a short passphrase is
// refused, and two packs of the same bytes differ (a fresh salt each time).
func TestStage49124EvidencePackOpensWithThePassphrase(t *testing.T) {
	pack := []byte("PK\x03\x04 a stand-in zip, 37 bytes long....")
	const pass = "correct horse battery"
	if _, err := EncryptEvidencePack(pack, "short"); err != ErrEvidencePassphrase {
		t.Fatalf("a short passphrase: %v, want ErrEvidencePassphrase", err)
	}
	sealed, err := EncryptEvidencePack(pack, pass)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(sealed, []byte("Salted__")) || (len(sealed)-16)%aes.BlockSize != 0 || bytes.Contains(sealed, pack[:10]) {
		t.Fatalf("not an OpenSSL enc file: % x", sealed[:16])
	}
	if again, _ := EncryptEvidencePack(pack, pass); bytes.Equal(again, sealed) {
		t.Fatal("two packs encrypted alike - the salt is not fresh")
	}
	open := func(passphrase string) []byte {
		keyIV, err := pbkdf2.Key(sha256.New, passphrase, sealed[8:16], evidencePBKDF2Iter, 48)
		if err != nil {
			t.Fatal(err)
		}
		block, _ := aes.NewCipher(keyIV[:32])
		plain := make([]byte, len(sealed)-16)
		cipher.NewCBCDecrypter(block, keyIV[32:48]).CryptBlocks(plain, sealed[16:])
		pad := int(plain[len(plain)-1])
		if pad < 1 || pad > aes.BlockSize {
			return nil
		}
		return plain[:len(plain)-pad]
	}
	if got := open(pass); !bytes.Equal(got, pack) {
		t.Fatalf("decrypted %q, want the pack", got)
	}
	if got := open("wrong horse battery"); bytes.Equal(got, pack) {
		t.Fatal("a wrong passphrase opened the pack")
	}

	// The command the guide gives, where openssl is installed.
	bin, err := exec.LookPath("openssl")
	if err != nil {
		t.Log("openssl not on PATH; the OpenSSL check is skipped")
		return
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "evidence.zip.enc"), filepath.Join(dir, "evidence.zip")
	if err := os.WriteFile(in, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	args := strings.Fields(EvidenceDecryptCommand)[1:]
	args = append(args, "-pass", "pass:"+pass)
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("openssl could not open it: %v %s", err, msg)
	}
	if got, err := os.ReadFile(out); err != nil || !bytes.Equal(got, pack) {
		t.Fatalf("openssl opened %q (%v), want the pack", got, err)
	}
}
