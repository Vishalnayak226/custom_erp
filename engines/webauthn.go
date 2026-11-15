package engines

// WebAuthn passkeys, standard library only (49.2.3).
//
// A passkey is a phishing-resistant second factor: the browser signs a server
// challenge with a private key that never leaves the device, and the browser
// binds the signature to the site's origin, so a look-alike site cannot relay
// it. Article 1 forbids a new dependency where the standard library will do,
// and the part of WebAuthn this needs is small: a CBOR reader for the
// attestation object and the COSE key, and a signature check. Built here:
//
//   - registration with attestation "none" (the browser's answer is taken as
//     the key; no vendor trust chain is evaluated, see
//     docs/security/mfa-assurance-tiers.md);
//   - assertion with ES256 (ECDSA P-256 / SHA-256) and RS256 (RSASSA-PKCS1-v1_5
//     / SHA-256), the two algorithms every platform and security key offers;
//   - user verification (PIN or biometric) required, sign-count rollback
//     refused (a cloned authenticator).
//
// The challenge store, tables and routes are in webauthn_store.go and
// internal/server/handlers_passkeys.go.

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
)

// ---- minimal CBOR reader (RFC 8949), only what WebAuthn sends ----

const cborMaxDepth = 8

// cborDecode reads one data item. Maps decode to map[interface{}]interface{}
// with int64 / string keys, byte strings to []byte, text to string.
func cborDecode(b []byte) (interface{}, []byte, error) { return cborItem(b, 0) }

func cborItem(b []byte, depth int) (interface{}, []byte, error) {
	if depth > cborMaxDepth {
		return nil, nil, errors.New("cbor: nested too deep")
	}
	if len(b) == 0 {
		return nil, nil, errors.New("cbor: truncated")
	}
	major, info := b[0]>>5, b[0]&0x1f
	b = b[1:]
	var arg uint64
	switch {
	case info < 24:
		arg = uint64(info)
	case info == 24, info == 25, info == 26, info == 27:
		n := 1 << (info - 24)
		if len(b) < n {
			return nil, nil, errors.New("cbor: truncated")
		}
		for _, c := range b[:n] {
			arg = arg<<8 | uint64(c)
		}
		b = b[n:]
	default:
		return nil, nil, errors.New("cbor: indefinite length or reserved value not supported")
	}
	switch major {
	case 0:
		if arg > 1<<62 {
			return nil, nil, errors.New("cbor: integer too large")
		}
		return int64(arg), b, nil
	case 1:
		if arg > 1<<62 {
			return nil, nil, errors.New("cbor: integer too large")
		}
		return -1 - int64(arg), b, nil
	case 2, 3:
		if arg > uint64(len(b)) {
			return nil, nil, errors.New("cbor: truncated")
		}
		s := b[:arg]
		if major == 3 {
			return string(s), b[arg:], nil
		}
		return append([]byte(nil), s...), b[arg:], nil
	case 4:
		if arg > 256 {
			return nil, nil, errors.New("cbor: array too long")
		}
		out := make([]interface{}, 0, arg)
		for i := uint64(0); i < arg; i++ {
			var v interface{}
			var err error
			if v, b, err = cborItem(b, depth+1); err != nil {
				return nil, nil, err
			}
			out = append(out, v)
		}
		return out, b, nil
	case 5:
		if arg > 64 {
			return nil, nil, errors.New("cbor: map too large")
		}
		out := make(map[interface{}]interface{}, arg)
		for i := uint64(0); i < arg; i++ {
			var k, v interface{}
			var err error
			if k, b, err = cborItem(b, depth+1); err != nil {
				return nil, nil, err
			}
			if v, b, err = cborItem(b, depth+1); err != nil {
				return nil, nil, err
			}
			out[k] = v
		}
		return out, b, nil
	case 6: // tag: ignore the tag, read the item
		return cborItem(b, depth+1)
	case 7:
		switch info {
		case 20:
			return false, b, nil
		case 21:
			return true, b, nil
		case 22, 23:
			return nil, b, nil
		}
		return nil, nil, errors.New("cbor: floats are not used by WebAuthn")
	}
	return nil, nil, errors.New("cbor: unknown type")
}

// ---- COSE public keys ----

const (
	coseAlgES256 = -7
	coseAlgRS256 = -257
)

// parseCOSEPublicKey turns a COSE_Key (CBOR) into a verifying key. The bytes
// are what the credentials table stores.
func parseCOSEPublicKey(raw []byte) (interface{}, error) {
	v, _, err := cborDecode(raw)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[interface{}]interface{})
	if !ok {
		return nil, errors.New("webauthn: public key is not a map")
	}
	kty, _ := m[int64(1)].(int64)
	alg, _ := m[int64(3)].(int64)
	switch {
	case kty == 2 && alg == coseAlgES256:
		crv, _ := m[int64(-1)].(int64)
		x, _ := m[int64(-2)].([]byte)
		y, _ := m[int64(-3)].([]byte)
		if crv != 1 || len(x) != 32 || len(y) != 32 {
			return nil, errors.New("webauthn: unsupported EC key")
		}
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
			return nil, errors.New("webauthn: EC point is not on the curve")
		}
		return pub, nil
	case kty == 3 && alg == coseAlgRS256:
		n, _ := m[int64(-1)].([]byte)
		e, _ := m[int64(-2)].([]byte)
		if len(n) < 256 || len(e) == 0 || len(e) > 4 {
			return nil, errors.New("webauthn: unsupported RSA key (2048 bits or more)")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}, nil
	}
	return nil, fmt.Errorf("webauthn: key type %d / algorithm %d is not supported (ES256 and RS256 are)", kty, alg)
}

func verifyWebAuthnSignature(key interface{}, signed, sig []byte) bool {
	h := sha256.Sum256(signed)
	switch k := key.(type) {
	case *ecdsa.PublicKey:
		return ecdsa.VerifyASN1(k, h[:], sig)
	case *rsa.PublicKey:
		return rsa.VerifyPKCS1v15(k, crypto.SHA256, h[:], sig) == nil
	}
	return false
}

// ---- authenticator data ----

const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
	flagAttested     = 0x40
)

type authData struct {
	rpIDHash  []byte
	flags     byte
	signCount uint32
	credID    []byte
	pubKey    []byte // COSE key bytes exactly as sent
	aaguid    []byte
}

func parseAuthData(b []byte, wantCredential bool) (*authData, error) {
	if len(b) < 37 {
		return nil, errors.New("webauthn: authenticator data too short")
	}
	a := &authData{rpIDHash: b[:32], flags: b[32], signCount: binary.BigEndian.Uint32(b[33:37])}
	if !wantCredential {
		return a, nil
	}
	if a.flags&flagAttested == 0 || len(b) < 55 {
		return nil, errors.New("webauthn: no credential in the registration")
	}
	a.aaguid = b[37:53]
	idLen := int(binary.BigEndian.Uint16(b[53:55]))
	if idLen == 0 || idLen > 1023 || len(b) < 55+idLen {
		return nil, errors.New("webauthn: bad credential id")
	}
	a.credID = b[55 : 55+idLen]
	rest := b[55+idLen:]
	_, tail, err := cborDecode(rest)
	if err != nil {
		return nil, err
	}
	a.pubKey = rest[:len(rest)-len(tail)]
	return a, nil
}

// ---- verification ----

// PasskeyOrigin is what the browser said it was on and the server expects:
// the page's origin and the relying-party id (its host name).
type PasskeyOrigin struct {
	Origin string // https://erp.example.com (http only for localhost)
	RPID   string // erp.example.com
}

// PasskeyOriginFromHost builds the expected origin from the request's own
// Host. A passkey is bound to that exact host, so it follows the tenant's
// domain with no setting; the browser's claimed origin must match it.
func PasskeyOriginFromHost(host string, secure bool) PasskeyOrigin {
	h := host
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	scheme := "https"
	if !secure {
		scheme = "http"
	}
	return PasskeyOrigin{Origin: scheme + "://" + host, RPID: strings.ToLower(h)}
}

type clientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

func checkClientData(raw []byte, wantType, wantChallenge string, o PasskeyOrigin) error {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return errors.New("webauthn: client data is not JSON")
	}
	if cd.Type != wantType {
		return errors.New("webauthn: wrong ceremony type")
	}
	if subtle.ConstantTimeCompare([]byte(cd.Challenge), []byte(wantChallenge)) != 1 {
		return errors.New("webauthn: challenge does not match")
	}
	want, err := url.Parse(o.Origin)
	got, err2 := url.Parse(cd.Origin)
	if err != nil || err2 != nil || !strings.EqualFold(want.Scheme+"://"+want.Host, got.Scheme+"://"+got.Host) {
		return errors.New("webauthn: the sign-in came from a different site")
	}
	return nil
}

func checkRPAndFlags(a *authData, o PasskeyOrigin) error {
	want := sha256.Sum256([]byte(o.RPID))
	if !bytes.Equal(a.rpIDHash, want[:]) {
		return errors.New("webauthn: the sign-in was made for a different site")
	}
	if a.flags&flagUserPresent == 0 {
		return errors.New("webauthn: user presence missing")
	}
	if a.flags&flagUserVerified == 0 {
		return errors.New("webauthn: user verification (PIN or biometric) required")
	}
	return nil
}

// PasskeyRegistration is a verified new credential, ready to store.
type PasskeyRegistration struct {
	CredentialID []byte
	PublicKey    []byte
	SignCount    uint32
	AAGUID       []byte
}

// VerifyPasskeyRegistration checks a navigator.credentials.create() answer.
func VerifyPasskeyRegistration(clientDataJSON, attestationObject []byte, challenge string, o PasskeyOrigin) (*PasskeyRegistration, error) {
	if err := checkClientData(clientDataJSON, "webauthn.create", challenge, o); err != nil {
		return nil, err
	}
	v, _, err := cborDecode(attestationObject)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[interface{}]interface{})
	if !ok {
		return nil, errors.New("webauthn: attestation is not a map")
	}
	raw, _ := m["authData"].([]byte)
	a, err := parseAuthData(raw, true)
	if err != nil {
		return nil, err
	}
	if err := checkRPAndFlags(a, o); err != nil {
		return nil, err
	}
	if _, err := parseCOSEPublicKey(a.pubKey); err != nil {
		return nil, err
	}
	return &PasskeyRegistration{CredentialID: a.credID, PublicKey: a.pubKey, SignCount: a.signCount, AAGUID: a.aaguid}, nil
}

// VerifyPasskeyAssertion checks a navigator.credentials.get() answer against
// the stored key and returns the new sign count. A counter that did not go up
// (when either side uses counters) is a cloned authenticator and is refused.
func VerifyPasskeyAssertion(clientDataJSON, authenticatorData, signature []byte, challenge string, o PasskeyOrigin, publicKey []byte, storedCount uint32) (uint32, error) {
	if err := checkClientData(clientDataJSON, "webauthn.get", challenge, o); err != nil {
		return 0, err
	}
	a, err := parseAuthData(authenticatorData, false)
	if err != nil {
		return 0, err
	}
	if err := checkRPAndFlags(a, o); err != nil {
		return 0, err
	}
	key, err := parseCOSEPublicKey(publicKey)
	if err != nil {
		return 0, err
	}
	h := sha256.Sum256(clientDataJSON)
	if !verifyWebAuthnSignature(key, append(append([]byte(nil), authenticatorData...), h[:]...), signature) {
		return 0, errors.New("webauthn: signature does not verify")
	}
	if (a.signCount != 0 || storedCount != 0) && a.signCount <= storedCount {
		return 0, errors.New("webauthn: sign counter went backwards (cloned authenticator?)")
	}
	return a.signCount, nil
}
