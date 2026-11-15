package engines

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"testing"
)

// A software authenticator: builds what a browser hands the server (49.2.3).

func cborUint(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	default:
		return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
	}
}
func cborBytes(b []byte) []byte { return append(cborUint(2, uint64(len(b))), b...) }
func cborText(s string) []byte  { return append(cborUint(3, uint64(len(s))), s...) }
func cborInt(n int64) []byte {
	if n >= 0 {
		return cborUint(0, uint64(n))
	}
	return cborUint(1, uint64(-1-n))
}

func softKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	x, y := k.X.FillBytes(make([]byte, 32)), k.Y.FillBytes(make([]byte, 32))
	cose := []byte{0xa5}
	cose = append(cose, cborInt(1)...)
	cose = append(cose, cborInt(2)...)
	cose = append(cose, cborInt(3)...)
	cose = append(cose, cborInt(-7)...)
	cose = append(cose, cborInt(-1)...)
	cose = append(cose, cborInt(1)...)
	cose = append(cose, cborInt(-2)...)
	cose = append(cose, cborBytes(x)...)
	cose = append(cose, cborInt(-3)...)
	cose = append(cose, cborBytes(y)...)
	return k, cose
}

func clientDataFor(typ, challenge, origin string) []byte {
	b, _ := json.Marshal(map[string]string{"type": typ, "challenge": challenge, "origin": origin})
	return b
}

func authDataFor(rpID string, flags byte, count uint32, credID, cose []byte) []byte {
	h := sha256.Sum256([]byte(rpID))
	b := append([]byte(nil), h[:]...)
	b = append(b, flags)
	c := make([]byte, 4)
	binary.BigEndian.PutUint32(c, count)
	b = append(b, c...)
	if credID != nil {
		b = append(b, make([]byte, 16)...)
		b = append(b, byte(len(credID)>>8), byte(len(credID)))
		b = append(b, credID...)
		b = append(b, cose...)
	}
	return b
}

func attestation(authData []byte) []byte {
	b := []byte{0xa3}
	b = append(b, cborText("fmt")...)
	b = append(b, cborText("none")...)
	b = append(b, cborText("attStmt")...)
	b = append(b, 0xa0)
	b = append(b, cborText("authData")...)
	b = append(b, cborBytes(authData)...)
	return b
}

func signAssertion(t *testing.T, k *ecdsa.PrivateKey, ad, cd []byte) []byte {
	h := sha256.Sum256(cd)
	d := sha256.Sum256(append(append([]byte(nil), ad...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k, d[:])
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func TestStage4923PasskeyRegistrationAndAssertion(t *testing.T) {
	o := PasskeyOriginFromHost("erp.example.com", true)
	k, cose := softKey(t)
	credID := []byte("credential-id-0123456789")

	// Registration.
	cd := clientDataFor("webauthn.create", "chal-1", o.Origin)
	ad := authDataFor(o.RPID, flagUserPresent|flagUserVerified|flagAttested, 0, credID, cose)
	reg, err := VerifyPasskeyRegistration(cd, attestation(ad), "chal-1", o)
	if err != nil {
		t.Fatalf("registration refused: %v", err)
	}
	if string(reg.CredentialID) != string(credID) {
		t.Fatal("credential id not read")
	}
	// Wrong challenge, wrong site, no user verification are refused.
	if _, err := VerifyPasskeyRegistration(cd, attestation(ad), "other", o); err == nil {
		t.Fatal("wrong challenge accepted")
	}
	evil := clientDataFor("webauthn.create", "chal-1", "https://erp-example.evil.com")
	if _, err := VerifyPasskeyRegistration(evil, attestation(ad), "chal-1", o); err == nil {
		t.Fatal("a different origin was accepted")
	}
	noUV := authDataFor(o.RPID, flagUserPresent|flagAttested, 0, credID, cose)
	if _, err := VerifyPasskeyRegistration(cd, attestation(noUV), "chal-1", o); err == nil {
		t.Fatal("registration without user verification accepted")
	}
	otherRP := authDataFor("evil.com", flagUserPresent|flagUserVerified|flagAttested, 0, credID, cose)
	if _, err := VerifyPasskeyRegistration(cd, attestation(otherRP), "chal-1", o); err == nil {
		t.Fatal("registration for another relying party accepted")
	}

	// Assertion.
	acd := clientDataFor("webauthn.get", "chal-2", o.Origin)
	aad := authDataFor(o.RPID, flagUserPresent|flagUserVerified, 5, nil, nil)
	sig := signAssertion(t, k, aad, acd)
	n, err := VerifyPasskeyAssertion(acd, aad, sig, "chal-2", o, reg.PublicKey, 4)
	if err != nil || n != 5 {
		t.Fatalf("assertion refused: %v (%d)", err, n)
	}
	// Replayed counter, tampered data, a stranger's key, wrong type.
	if _, err := VerifyPasskeyAssertion(acd, aad, sig, "chal-2", o, reg.PublicKey, 5); err == nil {
		t.Fatal("a counter that did not advance was accepted (cloned authenticator)")
	}
	bad := append([]byte(nil), aad...)
	bad[36] = 9
	if _, err := VerifyPasskeyAssertion(acd, bad, sig, "chal-2", o, reg.PublicKey, 4); err == nil {
		t.Fatal("tampered authenticator data accepted")
	}
	_, otherCose := softKey(t)
	if _, err := VerifyPasskeyAssertion(acd, aad, sig, "chal-2", o, otherCose, 4); err == nil {
		t.Fatal("signature verified against another key")
	}
	create := clientDataFor("webauthn.create", "chal-2", o.Origin)
	if _, err := VerifyPasskeyAssertion(create, aad, signAssertion(t, k, aad, create), "chal-2", o, reg.PublicKey, 4); err == nil {
		t.Fatal("a registration answer was accepted as a sign-in")
	}
	// Counter-less authenticators (always 0) are allowed.
	zad := authDataFor(o.RPID, flagUserPresent|flagUserVerified, 0, nil, nil)
	if _, err := VerifyPasskeyAssertion(acd, zad, signAssertion(t, k, zad, acd), "chal-2", o, reg.PublicKey, 0); err != nil {
		t.Fatalf("counter-less authenticator refused: %v", err)
	}
}

func TestStage4923CBORRefusesHostileInput(t *testing.T) {
	for name, in := range map[string][]byte{
		"empty":      {},
		"truncated":  {0x58, 0x40, 1, 2},
		"indefinite": {0x5f, 0xff},
		"deep":       {0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x00},
		"huge array": {0x99, 0xff, 0xff},
		"float":      {0xfb, 0, 0, 0, 0, 0, 0, 0, 0},
	} {
		if _, _, err := cborDecode(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := parseCOSEPublicKey([]byte{0xa0}); err == nil {
		t.Error("empty COSE key accepted")
	}
}
