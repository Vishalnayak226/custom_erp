package engines

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// webPushDecryptForTest is the phone's side of RFC 8291: what a browser does
// with the bytes the push service hands it.
func webPushDecryptForTest(uaPriv *ecdh.PrivateKey, authSecret, body []byte) ([]byte, error) {
	if len(body) < 21 {
		return nil, errors.New("short body")
	}
	salt := body[:16]
	idLen := int(body[20])
	asPublic := body[21 : 21+idLen]
	ciphertext := body[21+idLen:]
	asKey, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		return nil, err
	}
	secret, err := uaPriv.ECDH(asKey)
	if err != nil {
		return nil, err
	}
	keyInfo := "WebPush: info\x00" + string(uaPriv.PublicKey().Bytes()) + string(asPublic)
	ikm, _ := hkdf.Key(sha256.New, secret, authSecret, keyInfo, 32)
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	if n := len(plain); n == 0 || plain[n-1] != 0x02 {
		return nil, errors.New("no last-record delimiter")
	}
	return plain[:len(plain)-1], nil
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := webPushDecodeKey(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return b
}

// RFC 8291 section 5 / appendix A: the published example, byte for byte.
func TestWebPushEncryptRFC8291Vector(t *testing.T) {
	asPriv, err := ecdh.P256().NewPrivateKey(mustB64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	if got := webPushB64.EncodeToString(asPriv.PublicKey().Bytes()); got != "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8" {
		t.Fatalf("application server public key = %s", got)
	}
	uaPublic := mustB64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4")
	auth := mustB64(t, "BTBZMqHH6r4Tts7J_aSIgg")
	salt := mustB64(t, "DGv6ra1nlYgDCS1FRnbzlw")
	body, err := webPushEncrypt(uaPublic, auth, []byte("When I grow up, I want to be a watermelon"), asPriv, salt)
	if err != nil {
		t.Fatal(err)
	}
	want := mustB64(t, "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN")
	if !bytes.Equal(body, want) {
		t.Fatalf("encrypted message differs from RFC 8291's example:\n got %s\nwant %s", webPushB64.EncodeToString(body), webPushB64.EncodeToString(want))
	}
	if rs := binary.BigEndian.Uint32(body[16:20]); rs != 4096 {
		t.Fatalf("record size = %d", rs)
	}
	uaPriv, err := ecdh.P256().NewPrivateKey(mustB64(t, "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := webPushDecryptForTest(uaPriv, auth, body)
	if err != nil || string(plain) != "When I grow up, I want to be a watermelon" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
}

func TestWebPushEncryptRoundTripAndLimits(t *testing.T) {
	uaPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	msg := []byte(`{"title":"Approvals","body":"Purchase Order PO-1 is waiting"}`)
	a, err := webPushEncrypt(uaPriv.PublicKey().Bytes(), auth, msg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := webPushEncrypt(uaPriv.PublicKey().Bytes(), auth, msg, nil, nil)
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of one message were identical - salt or key not fresh")
	}
	for _, body := range [][]byte{a, b} {
		if plain, err := webPushDecryptForTest(uaPriv, auth, body); err != nil || !bytes.Equal(plain, msg) {
			t.Fatalf("round trip = %q, %v", plain, err)
		}
	}
	if _, err := webPushEncrypt(uaPriv.PublicKey().Bytes(), auth[:8], msg, nil, nil); err == nil {
		t.Fatal("an 8-byte secret was accepted")
	}
	if _, err := webPushEncrypt(uaPriv.PublicKey().Bytes(), auth, bytes.Repeat([]byte("x"), webPushMaxPlaintext+1), nil, nil); err == nil {
		t.Fatal("an over-long message was accepted")
	}
}

func TestWebPushEndpointAllowList(t *testing.T) {
	for _, ok := range []string{"https://fcm.googleapis.com/fcm/send/abc", "https://updates.push.services.mozilla.com/wpush/v2/x", "https://web.push.apple.com/QG", "https://wns2-par02p.notify.windows.com/w/?token=1"} {
		if err := webPushEndpointAllowed(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://fcm.googleapis.com/x", "https://evil.example.com/fcm.googleapis.com", "https://127.0.0.1/x", "https://fcm.googleapis.com.evil.com/x", "ftp://x"} {
		if err := webPushEndpointAllowed(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// verifyVAPIDForTest checks an Authorization header the way a push service
// does: the JWT's ES256 signature against the k= key, and its audience.
func verifyVAPIDForTest(t *testing.T, header, wantAud string) {
	t.Helper()
	if !strings.HasPrefix(header, "vapid t=") {
		t.Fatalf("Authorization = %q", header)
	}
	parts := strings.SplitN(strings.TrimPrefix(header, "vapid t="), ", k=", 2)
	if len(parts) != 2 {
		t.Fatalf("Authorization = %q", header)
	}
	jwt, k := parts[0], parts[1]
	seg := strings.Split(jwt, ".")
	if len(seg) != 3 {
		t.Fatalf("jwt = %q", jwt)
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), mustB64(t, k))
	if err != nil {
		t.Fatalf("k: %v", err)
	}
	sig := mustB64(t, seg[2])
	digest := sha256.Sum256([]byte(seg[0] + "." + seg[1]))
	if len(sig) != 64 || !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("VAPID signature does not verify")
	}
	var claims map[string]interface{}
	_ = json.Unmarshal(mustB64(t, seg[1]), &claims)
	if claims["aud"] != wantAud || claims["sub"] == "" || claims["exp"] == nil {
		t.Fatalf("claims = %v", claims)
	}
}

// One alert end to end: subscribe, queue, deliver to a local push service
// that checks the signature and decrypts the message; then a 410 ends it.
func TestWebPushDeliveryEndToEnd(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1")
	t.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "")

	uaPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	var got WebPushMessage
	status := http.StatusCreated
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyVAPIDForTest(t, r.Header.Get("Authorization"), "http://"+r.Host)
		if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") == "" {
			t.Errorf("headers = %v", r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		plain, err := webPushDecryptForTest(uaPriv, auth, body)
		if err != nil {
			t.Errorf("decrypt: %v", err)
		}
		_ = json.Unmarshal(plain, &got)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	prev := webPushEndpointAllowed
	webPushEndpointAllowed = func(raw string) error {
		if strings.HasPrefix(raw, srv.URL) {
			return nil
		}
		return prev(raw)
	}
	defer func() { webPushEndpointAllowed = prev }()

	in := WebPushSubscribeInput{Endpoint: srv.URL + "/push/phone-1"}
	in.Keys.P256dh = webPushB64.EncodeToString(uaPriv.PublicKey().Bytes())
	in.Keys.Auth = webPushB64.EncodeToString(auth) + "==" // padded, as some browsers send it
	sub, err := SaveWebPushSubscription(tenantID, "u-push", "ST-P", "test", in)
	if err != nil || !sub.Subscribed || !sub.Approvals || !sub.LowStock || sub.Store != "ST-P" {
		t.Fatalf("subscribe = %+v, %v", sub, err)
	}
	off := false
	if sub, err = SaveWebPushSubscription(tenantID, "u-push", "ST-P", "test", WebPushSubscribeInput{Endpoint: in.Endpoint, Keys: in.Keys, LowStock: &off}); err != nil || sub.LowStock || !sub.Approvals {
		t.Fatalf("turning low stock off = %+v, %v", sub, err)
	}
	if other, _ := GetWebPushSubscription(tenantID, "someone-else", in.Endpoint); other.Subscribed {
		t.Fatal("another user sees this phone's alerts")
	}
	bad := in
	bad.Keys.Auth = "short"
	if _, err := SaveWebPushSubscription(tenantID, "u-push", "ST-P", "test", bad); err == nil {
		t.Fatal("a bad push secret was accepted")
	}

	var subID int64
	if err := db.DB.QueryRow(`SELECT id FROM `+schemaName+`.web_push_subscriptions WHERE endpoint = $1`, in.Endpoint).Scan(&subID); err != nil {
		t.Fatal(err)
	}
	job := Job{Payload: map[string]interface{}{"subscription_id": float64(subID), "title": "Approvals", "body": "PO-1 is waiting", "url": "/store/#/approvals", "tag": "approvals"}}
	res, err := deliverWebPush(t.Context(), schemaName, job)
	if err != nil || res["status"] != http.StatusCreated {
		t.Fatalf("deliver = %v, %v", res, err)
	}
	if got.Title != "Approvals" || got.Body != "PO-1 is waiting" || got.URL != "/store/#/approvals" {
		t.Fatalf("phone received %+v", got)
	}

	status = http.StatusServiceUnavailable
	if _, err := deliverWebPush(t.Context(), schemaName, job); err == nil {
		t.Fatal("a 503 was not retried")
	}
	status = http.StatusGone
	if res, err := deliverWebPush(t.Context(), schemaName, job); err != nil || res["gone"] != true {
		t.Fatalf("410 = %v, %v", res, err)
	}
	if after, _ := GetWebPushSubscription(tenantID, "u-push", in.Endpoint); after.Subscribed {
		t.Fatal("a 410 left the subscription in place")
	}
	if res, err := deliverWebPush(t.Context(), schemaName, job); err != nil || res["skipped"] == nil {
		t.Fatalf("after removal = %v, %v", res, err)
	}

	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "")
	if _, err := SaveWebPushSubscription(tenantID, "u-push", "ST-P", "test", in); err != nil {
		t.Fatal(err)
	}
	_ = db.DB.QueryRow(`SELECT id FROM `+schemaName+`.web_push_subscriptions WHERE endpoint = $1`, in.Endpoint).Scan(&subID)
	job.Payload["subscription_id"] = float64(subID)
	got = WebPushMessage{}
	if res, err := deliverWebPush(t.Context(), schemaName, job); err != nil || res["simulated"] != true || got.Title != "" {
		t.Fatalf("side effects off = %v, %v (received %+v)", res, err, got)
	}
}

// What is new since the last look; the first look only takes note.
func TestWebPushNewRefsBaselineAndRepeat(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	look := func(refs ...string) []string {
		t.Helper()
		got, err := webPushNewRefs(schemaName, "user:u@ST", refs)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := look("a", "b"); len(got) != 0 {
		t.Fatalf("first look alerted %v", got)
	}
	if got := look("a", "b", "c"); len(got) != 1 || got[0] != "c" {
		t.Fatalf("second look = %v", got)
	}
	if got := look("a", "b", "c"); len(got) != 0 {
		t.Fatalf("repeat look = %v", got)
	}
	look("a")                                                  // b and c decided
	if got := look("a", "c"); len(got) != 1 || got[0] != "c" { // c sent back for approval
		t.Fatalf("returning ref = %v", got)
	}
	if got := look(); len(got) != 0 {
		t.Fatalf("empty look = %v", got)
	}
}
