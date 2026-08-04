package engines

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"strings"
	"testing"
)

// The whole QZ feature rests on producing a signature QZ Tray accepts, and a
// rejected signature fails silently - the tray just falls back to prompting
// the operator on every label, which looks like "QZ is being flaky" rather
// than like a bug here. These tests pin the contract to QZ's own source so a
// future edit cannot quietly break it.
//
// Reproduced from qz/auth/Certificate.java:
//
//	Signature verifier = Signature.getInstance("SHA512withRSA");
//	verifier.initVerify(theCertificate.getPublicKey());
//	verifier.update(StringUtils.getBytesUtf8(DigestUtils.sha256Hex(data)));
//	return verifier.verify(Base64.decodeBase64(signature));
//
// and qz/ws/PrintSocketClient.java, which builds `data` as:
//
//	new JSONObject(message, new String[] {"call", "params", "timestamp"}).toString()
func verifyLikeQZTray(t *testing.T, certPEM, toSign, signatureB64 string) error {
	t.Helper()

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatalf("certificate is not valid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("could not parse certificate: %v", err)
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("certificate public key is %T, QZ requires RSA", cert.PublicKey)
	}
	if pub.N.BitLen() != 2048 {
		t.Fatalf("QZ requires a 2048-bit RSA key for an override certificate, got %d", pub.N.BitLen())
	}

	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		t.Fatalf("signature is not base64: %v", err)
	}

	// sha256Hex(data) -> UTF-8 bytes of the hex string -> SHA512withRSA.
	sum := sha256.Sum256([]byte(toSign))
	hashHex := hex.EncodeToString(sum[:])
	digest := sha512.Sum512([]byte(hashHex))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA512, digest[:], sig)
}

func TestQZSignatureVerifiesTheWayQZTrayVerifies(t *testing.T) {
	certPEM, err := QZCertificatePEM()
	if err != nil {
		t.Fatalf("QZCertificatePEM: %v", err)
	}

	// A realistic signed payload: exactly the three keys, in the order
	// public/qz-print.js emits them.
	toSign := `{"call":"print","params":{"printer":{"name":"ZDesigner ZD220-203dpi ZPL"},` +
		`"options":{"copies":1,"units":"in"},"data":[{"type":"raw","format":"command",` +
		`"flavor":"plain","data":"^XA^FO30,30^A0N,40,40^FDtest^FS^XZ"}]},"timestamp":1754200000000}`

	sum := sha256.Sum256([]byte(toSign))
	hashHex := hex.EncodeToString(sum[:])

	sig, err := QZSignRequest(hashHex)
	if err != nil {
		t.Fatalf("QZSignRequest: %v", err)
	}
	if err := verifyLikeQZTray(t, certPEM, toSign, sig); err != nil {
		t.Fatalf("signature would be REJECTED by QZ Tray: %v", err)
	}
}

// Guards the single most likely regression: signing the raw 32 digest bytes
// instead of the UTF-8 bytes of the hex string. Both "look right" in code
// review; only one validates.
func TestQZSignatureIsOverHexStringNotRawDigest(t *testing.T) {
	certPEM, err := QZCertificatePEM()
	if err != nil {
		t.Fatalf("QZCertificatePEM: %v", err)
	}
	toSign := `{"call":"printers.find","params":{},"timestamp":1754200000000}`
	sum := sha256.Sum256([]byte(toSign))
	hashHex := hex.EncodeToString(sum[:])

	sig, err := QZSignRequest(hashHex)
	if err != nil {
		t.Fatalf("QZSignRequest: %v", err)
	}

	block, _ := pem.Decode([]byte(certPEM))
	cert, _ := x509.ParseCertificate(block.Bytes)
	pub := cert.PublicKey.(*rsa.PublicKey)
	raw, _ := base64.StdEncoding.DecodeString(sig)

	// Signing the raw digest bytes must NOT validate - if it does, the
	// implementation drifted to the wrong side of the contract.
	wrong := sha512.Sum512(sum[:])
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA512, wrong[:], raw); err == nil {
		t.Fatal("signature validated over the raw digest bytes - QZ Tray hashes to a hex STRING first")
	}
}

func TestQZSignRequestRejectsAnythingButAHash(t *testing.T) {
	// The endpoint is a signing oracle; it must only ever emit signatures
	// over hash-shaped input, never over an attacker-chosen document.
	for _, bad := range []string{
		"",
		"not-a-hash",
		`{"call":"print","params":{},"timestamp":1}`,
		strings.Repeat("a", 63),
		strings.Repeat("a", 65),
		strings.Repeat("A", 64) + "extra",
		strings.Repeat("g", 64), // right length, not hex
	} {
		if _, err := QZSignRequest(bad); err == nil {
			t.Fatalf("QZSignRequest accepted non-hash input %q", bad)
		}
	}

	// Uppercase hex is normalised rather than rejected, since it is the same
	// digest and a caller has no reason to fail over letter case.
	if _, err := QZSignRequest(strings.Repeat("AB", 32)); err != nil {
		t.Fatalf("QZSignRequest rejected valid uppercase hex: %v", err)
	}
}

func TestQZSignatureIsStableAcrossCalls(t *testing.T) {
	// PKCS#1 v1.5 is deterministic, so the same request must sign identically.
	// A change here means the key is being regenerated per call, which would
	// invalidate every packing PC's override.crt.
	hashHex := strings.Repeat("ab", 32)
	first, err := QZSignRequest(hashHex)
	if err != nil {
		t.Fatalf("QZSignRequest: %v", err)
	}
	second, err := QZSignRequest(hashHex)
	if err != nil {
		t.Fatalf("QZSignRequest: %v", err)
	}
	if first != second {
		t.Fatal("signing key is not stable across calls")
	}
}

func TestZPLEscapingNeutralisesControlCharacters(t *testing.T) {
	// A "^" in a customer address would otherwise be read as the start of a
	// ZPL command and silently truncate the label.
	got := zplEscape("Flat ^12 ~ Block\\C")
	for _, c := range []string{"^", "~", "\\"} {
		if strings.Contains(got, c) {
			t.Fatalf("zplEscape left %q in %q", c, got)
		}
	}
}

func TestBuildStickerPayloadRawVersusFallback(t *testing.T) {
	labels := []StickerLabel{{SKU: "SKU-1", Name: "Test Item", Barcode: "8901234567890"}}

	zpl := BuildStickerPayload(labels, 2, "ZPL")
	if zpl == nil {
		t.Fatal("ZPL printer should get a raw payload")
	}
	if len(zpl.Items) != 1 || zpl.Items[0].Type != "raw" || zpl.Items[0].Format != "command" {
		t.Fatalf("unexpected ZPL item shape: %+v", zpl.Items)
	}
	if n := strings.Count(zpl.Items[0].Data, "^XA"); n != 2 {
		t.Fatalf("copies=2 should emit 2 labels, got %d", n)
	}
	if !strings.Contains(zpl.Items[0].Data, "8901234567890") {
		t.Fatal("barcode value missing from ZPL")
	}

	// A PDF/driver printer has no raw form; nil tells the caller to fall back
	// to the existing browser print sheet rather than printing nothing.
	if BuildStickerPayload(labels, 1, "PDF") != nil {
		t.Fatal("non-raw printer should return nil so the caller falls back")
	}
}

func TestBuildPassThroughPayloadSizesLabelsAndKeepsPDFIntact(t *testing.T) {
	const data = "JVBERi0xLjQK" // arbitrary base64
	p := BuildPassThroughPayload(data, "pdf", QZPrinter{WidthMM: "101.6", HeightMM: "152.4"})
	if len(p.Items) != 1 {
		t.Fatalf("expected one item, got %d", len(p.Items))
	}
	it := p.Items[0]
	if it.Type != "pixel" || it.Format != "pdf" || it.Flavor != "base64" {
		t.Fatalf("marketplace PDF must pass through as a base64 pixel/pdf item, got %+v", it)
	}
	if it.Data != data {
		t.Fatal("marketplace PDF payload was altered - carrier barcodes must print exactly as issued")
	}
	// 101.6mm x 152.4mm is the standard 4x6 label.
	if it.Opts["pageWidth"] != "4.000" || it.Opts["pageHeight"] != "6.000" {
		t.Fatalf("4x6 label sizing wrong: %+v", it.Opts)
	}

	// With no dimensions configured, no sizing is forced on the driver.
	if p2 := BuildPassThroughPayload(data, "pdf", QZPrinter{}); p2.Items[0].Opts != nil {
		t.Fatalf("expected no page sizing when the printer has no dimensions, got %+v", p2.Items[0].Opts)
	}
}
