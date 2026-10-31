// Command releasesign signs the release binary on the release machine and
// checks a signature (Stage 49.10.6b.3).
//
// deploy/deploy.ps1 builds the linux binary, signs it with the release key -
// a file on the release machine, never in the repository - and ships the
// signature beside it. On the server, deploy/remote_deploy.sh checks the
// signature with the server's own openssl against the public key the server
// already trusts, before the new binary runs at all. A binary built anywhere
// else, changed after it was built, or shipped without its signature stops
// the deploy before the swap.
//
// The formats are the ones openssl reads, so the server needs no tool of
// ours: the private key is PKCS#8 PEM, the public key is PKIX ("PUBLIC KEY")
// PEM, and a signature is the 64 raw bytes of a pure Ed25519 signature over
// the whole file (openssl pkeyutl -verify -rawin).
//
// Usage:
//
//	releasesign keygen -key <path>                    create a key; refuses to replace one
//	releasesign pub -key <path>                       print the public key (PEM)
//	releasesign sign -key <path> -in <file>           print the signature, base64, one line
//	releasesign verify -pub <path> -in <file> -sig <file>
//	releasesign fingerprint -pub <path>               the key's short name, as the server prints it
//
// The private key is never printed. sign prints base64 rather than writing a
// file so the caller writes it: on the release machine, Windows Controlled
// Folder Access stops a freshly built binary from writing under Documents.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usage = `usage:
  releasesign keygen -key <path>
  releasesign pub -key <path>
  releasesign sign -key <path> -in <file>
  releasesign verify -pub <path> -in <file> -sig <file>
  releasesign fingerprint -pub <path>`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	fs := flag.NewFlagSet("releasesign "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	keyPath := fs.String("key", "", "the release signing key (private, PKCS#8 PEM)")
	pubPath := fs.String("pub", "", "the release public key (PEM)")
	inPath := fs.String("in", "", "the file signed or checked")
	sigPath := fs.String("sig", "", "the signature file (64 raw bytes)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	need := func(names ...string) bool {
		for _, n := range names {
			if fs.Lookup(n).Value.String() == "" {
				fmt.Fprintf(stderr, "releasesign %s: -%s is required\n%s\n", args[0], n, usage)
				return false
			}
		}
		return true
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "releasesign %s: %v\n", args[0], err)
		return 1
	}

	switch args[0] {
	case "keygen":
		if !need("key") {
			return 2
		}
		pub, err := generateKey(*keyPath)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "created the release signing key %s (fingerprint %s)\n", *keyPath, fingerprint(pub))
	case "pub":
		if !need("key") {
			return 2
		}
		priv, err := loadPrivateKey(*keyPath)
		if err != nil {
			return fail(err)
		}
		b, err := publicKeyPEM(priv.Public().(ed25519.PublicKey))
		if err != nil {
			return fail(err)
		}
		stdout.Write(b)
	case "sign":
		if !need("key", "in") {
			return 2
		}
		priv, err := loadPrivateKey(*keyPath)
		if err != nil {
			return fail(err)
		}
		data, err := os.ReadFile(*inPath)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)))
	case "verify":
		if !need("pub", "in", "sig") {
			return 2
		}
		if err := verifyFile(*pubPath, *inPath, *sigPath); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "signature OK: %s\n", *inPath)
	case "fingerprint":
		if !need("pub") {
			return 2
		}
		pub, err := loadPublicKey(*pubPath)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, fingerprint(pub))
	default:
		fmt.Fprintf(stderr, "releasesign: unknown command %q\n%s\n", args[0], usage)
		return 2
	}
	return 0
}

// generateKey writes a new private key to path - its folder made if need be,
// readable by its owner only - and refuses to replace a key already there:
// the server keeps trusting the key it pinned, so a silently new key would
// stop every later deploy.
func generateKey(path string) (ed25519.PublicKey, error) {
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("%s already exists - a release key is never replaced by keygen", path)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	werr := pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return nil, werr
	}
	return pub, nil
}

func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("%s is not a PKCS#8 PEM private key", path)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s is not an ed25519 key", path)
	}
	return priv, nil
}

func loadPublicKey(path string) (ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("%s is not a PEM public key", path)
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%s is not an ed25519 key", path)
	}
	return pub, nil
}

func publicKeyPEM(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// fingerprint is the first 16 hex digits of the SHA-256 of the key's PKIX
// DER - what remote_deploy.sh prints with openssl pkey -outform DER | sha256sum.
func fingerprint(pub ed25519.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "unknown"
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])[:16]
}

var errBadSignature = errors.New("the signature does not match this file and key")

func verifyFile(pubPath, inPath, sigPath string) error {
	pub, err := loadPublicKey(pubPath)
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return err
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%s is %d bytes, not a %d-byte ed25519 signature", sigPath, len(sig), ed25519.SignatureSize)
	}
	data, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, data, sig) {
		return errBadSignature
	}
	return nil
}
