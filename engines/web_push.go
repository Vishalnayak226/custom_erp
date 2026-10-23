package engines

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Stage 102.1.3 - push alerts for the store app (Web Push).
//
// A phone that turns alerts on gives the server its push service address and
// two keys; the server then sends it short, encrypted messages through that
// push service (Google's, Mozilla's, Apple's or Microsoft's - whichever the
// phone's browser uses). Two standards, both implemented here with the Go
// standard library, no new dependency:
//
//   - RFC 8291 (message encryption, aes128gcm): an ECDH P-256 agreement with
//     the phone's key, HKDF-SHA-256 for the content key and nonce, AES-128-GCM.
//     The push service carries bytes it cannot read.
//   - RFC 8292 (VAPID): every request is signed (ES256 JWT) with the client's
//     own key pair, so the push service knows the messages come from the
//     server the phone subscribed to. The pair is made once per client and its
//     private half is kept only encrypted (WEB_PUSH_KEY keyring, the
//     secret_keyring.go rotation pattern).
//
// What is pushed is decided by StartWebPushAlertWorker, which asks the same
// questions the app's own screens ask: the task inbox's approvals (73.3.3) -
// so a person is alerted only for what they may decide, whichever path put the
// document into Pending Approval - and the Inventory Dashboard's low-stock rule
// (inventoryLowStock). Each alert is pushed once (web_push_seen); the first
// look at a person or store only takes note of what is already there, so
// turning alerts on never floods the phone. Sending rides the job runner
// (retries, backoff, dead letters) and, like notifications.go, makes no real
// call for a sandbox client or while external side effects are off.

var webPushKeyRing, webPushSigningKey = loadVersionedKeyring("WEB_PUSH_KEY", "web_push_key.local")

var webPushB64 = base64.RawURLEncoding

// webPushDecodeKey reads a browser key in base64url, with or without padding
// (browsers differ), or plain base64.
func webPushDecodeKey(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	if b, err := webPushB64.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// webPushKeyPair is the client's VAPID key pair.
type webPushKeyPair struct {
	priv   *ecdsa.PrivateKey
	public string // base64url uncompressed point: the phone's applicationServerKey
}

// webPushKeysForSchema reads the client's key pair, making it the first time.
// Two servers racing to make it both read back the one that won.
func webPushKeysForSchema(schema string) (*webPushKeyPair, error) {
	read := func() (string, []byte, error) {
		var pub string
		var sealed []byte
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT public_key, private_key_enc FROM %s.web_push_vapid WHERE id = 1`, schema)).Scan(&pub, &sealed)
		return pub, sealed, err
	}
	pub, sealed, err := read()
	if err == sql.ErrNoRows {
		priv, genErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if genErr != nil {
			return nil, genErr
		}
		raw, rawErr := priv.Bytes()
		if rawErr != nil {
			return nil, rawErr
		}
		pubBytes, pubErr := priv.PublicKey.Bytes()
		if pubErr != nil {
			return nil, pubErr
		}
		enc, encErr := encryptVersioned(webPushSigningKey, raw)
		if encErr != nil {
			return nil, encErr
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.web_push_vapid (id, public_key, private_key_enc) VALUES (1, $1, $2) ON CONFLICT (id) DO NOTHING`, schema),
			webPushB64.EncodeToString(pubBytes), enc); err != nil {
			return nil, err
		}
		pub, sealed, err = read()
	}
	if err != nil {
		return nil, err
	}
	raw, err := decryptVersioned(webPushKeyRing, sealed)
	if err != nil {
		return nil, fmt.Errorf("push key cannot be read (wrong WEB_PUSH_KEY?): %v", err)
	}
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return nil, err
	}
	return &webPushKeyPair{priv: priv, public: pub}, nil
}

// WebPushPublicKey is the key a phone subscribes with.
func WebPushPublicKey(tenantID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	k, err := webPushKeysForSchema(schema)
	if err != nil {
		return "", err
	}
	return k.public, nil
}

// webPushHosts are the push services browsers use. The server only ever posts
// to one of these: an address a phone hands over is otherwise a way to make
// the server call anywhere. WEB_PUSH_EXTRA_HOSTS (comma-separated) adds more.
var webPushHosts = []string{"fcm.googleapis.com", "push.services.mozilla.com", "push.apple.com", "notify.windows.com"}

// webPushEndpointAllowed is swapped by tests for their local push service.
var webPushEndpointAllowed = func(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return errors.New("the push address is not an https address")
	}
	host := strings.ToLower(u.Hostname())
	hosts := append([]string{}, webPushHosts...)
	for _, h := range strings.Split(os.Getenv("WEB_PUSH_EXTRA_HOSTS"), ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			hosts = append(hosts, h)
		}
	}
	for _, h := range hosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return nil
		}
	}
	return fmt.Errorf("%s is not a push service this server sends to", host)
}

// WebPushSubscribeInput is what the browser's PushSubscription.toJSON() gives,
// plus which alerts the person wants (nil keeps the saved choice, else on).
type WebPushSubscribeInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Approvals *bool `json:"approvals"`
	LowStock  *bool `json:"low_stock"`
}

// WebPushSubscription is one phone's alerts, as the app shows them.
type WebPushSubscription struct {
	Subscribed bool   `json:"subscribed"`
	Store      string `json:"store,omitempty"`
	Approvals  bool   `json:"approvals"`
	LowStock   bool   `json:"low_stock"`
	LastSentAt string `json:"last_sent_at,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

func validWebPushKeys(p256dh, auth string) error {
	ua, err := webPushDecodeKey(p256dh)
	if err != nil {
		return errors.New("the phone's push key is not readable")
	}
	if _, err := ecdh.P256().NewPublicKey(ua); err != nil {
		return errors.New("the phone's push key is not a P-256 key")
	}
	if a, err := webPushDecodeKey(auth); err != nil || len(a) != 16 {
		return errors.New("the phone's push secret is not 16 bytes")
	}
	return nil
}

// SaveWebPushSubscription turns alerts on (or updates them) for this phone.
// A phone has one push address, so the last person to turn alerts on there is
// who it alerts; store is the working store at that moment.
func SaveWebPushSubscription(tenantID, userID, store, userAgent string, in WebPushSubscribeInput) (*WebPushSubscription, error) {
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	if err := webPushEndpointAllowed(in.Endpoint); err != nil {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "endpoint", Message: err.Error()}
	}
	if err := validWebPushKeys(in.Keys.P256dh, in.Keys.Auth); err != nil {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "keys", Message: err.Error()}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	approvals, lowStock := true, true
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT alert_approvals, alert_low_stock FROM %s.web_push_subscriptions WHERE endpoint = $1`, schema), in.Endpoint).Scan(&approvals, &lowStock)
	if in.Approvals != nil {
		approvals = *in.Approvals
	}
	if in.LowStock != nil {
		lowStock = *in.LowStock
	}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.web_push_subscriptions (user_id, endpoint, p256dh, auth, store, alert_approvals, alert_low_stock, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (endpoint) DO UPDATE SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth,
		  store = EXCLUDED.store, alert_approvals = EXCLUDED.alert_approvals, alert_low_stock = EXCLUDED.alert_low_stock,
		  user_agent = EXCLUDED.user_agent, failures = 0, last_error = NULL`, schema),
		userID, in.Endpoint, strings.TrimSpace(in.Keys.P256dh), strings.TrimSpace(in.Keys.Auth), strings.TrimSpace(store), approvals, lowStock, userAgent); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "STORE_APP_PUSH", "SUBSCRIBED", fmt.Sprintf("Push alerts on for store %s (approvals %v, low stock %v)", store, approvals, lowStock))
	return GetWebPushSubscription(tenantID, userID, in.Endpoint)
}

// GetWebPushSubscription reads this person's alerts on this phone.
func GetWebPushSubscription(tenantID, userID, endpoint string) (*WebPushSubscription, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := &WebPushSubscription{}
	var sent sql.NullString
	var lastErr sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT store, alert_approvals, alert_low_stock, to_char(last_sent_at, 'YYYY-MM-DD HH24:MI'), last_error
		FROM %s.web_push_subscriptions WHERE endpoint = $1 AND user_id = $2`, schema), strings.TrimSpace(endpoint), userID).
		Scan(&out.Store, &out.Approvals, &out.LowStock, &sent, &lastErr)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.Subscribed, out.LastSentAt, out.LastError = true, sent.String, lastErr.String
	return out, nil
}

// DeleteWebPushSubscription turns alerts off for this person on this phone.
func DeleteWebPushSubscription(tenantID, userID, endpoint string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.web_push_subscriptions WHERE endpoint = $1 AND user_id = $2`, schema), strings.TrimSpace(endpoint), userID)
	return err
}

// webPushRecordSize is the one record's size (RFC 8188 rs). Push services
// accept at most 4096 bytes, header included, so a message is kept short.
const (
	webPushRecordSize   = 4096
	webPushMaxPlaintext = 3000
)

// webPushEncrypt is RFC 8291's aes128gcm encryption of one message for one
// phone. asPriv and salt are made fresh when nil; the test vector fixes them.
func webPushEncrypt(uaPublic, authSecret, plaintext []byte, asPriv *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(authSecret) != 16 {
		return nil, errors.New("push secret must be 16 bytes")
	}
	if len(plaintext) > webPushMaxPlaintext {
		return nil, errors.New("push message too long")
	}
	uaKey, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("phone push key: %v", err)
	}
	if asPriv == nil {
		if asPriv, err = ecdh.P256().GenerateKey(rand.Reader); err != nil {
			return nil, err
		}
	}
	if salt == nil {
		salt = make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
	}
	secret, err := asPriv.ECDH(uaKey)
	if err != nil {
		return nil, err
	}
	asPublic := asPriv.PublicKey().Bytes()
	keyInfo := "WebPush: info\x00" + string(uaPublic) + string(asPublic)
	ikm, err := hkdf.Key(sha256.New, secret, authSecret, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// The one (and last) record: the message, then the 0x02 delimiter.
	record := append(append(make([]byte, 0, len(plaintext)+1), plaintext...), 0x02)
	sealed := gcm.Seal(nil, nonce, record, nil)

	out := make([]byte, 0, 16+4+1+len(asPublic)+len(sealed))
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, webPushRecordSize)
	out = append(out, byte(len(asPublic)))
	out = append(out, asPublic...)
	return append(out, sealed...), nil
}

// webPushSubject is the contact the push service may use (RFC 8292 "sub").
func webPushSubject() string {
	if s := strings.TrimSpace(os.Getenv("WEB_PUSH_SUBJECT")); s != "" {
		return s
	}
	return "mailto:support@wholeops.in"
}

// webPushVAPIDAuth is the Authorization header for one push request: an
// ES256 JWT for the push service's origin, valid 12 hours, and the public key.
func webPushVAPIDAuth(k *webPushKeyPair, endpoint string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	header := webPushB64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]interface{}{"aud": u.Scheme + "://" + u.Host, "exp": now.Add(12 * time.Hour).Unix(), "sub": webPushSubject()})
	if err != nil {
		return "", err
	}
	signing := header + "." + webPushB64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, k.priv, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + signing + "." + webPushB64.EncodeToString(sig) + ", k=" + k.public, nil
}

// WebPushMessage is what the phone shows: the service worker draws it.
type WebPushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

const webPushJobType = "web_push_delivery"

func init() {
	RegisterJobHandler(webPushJobType, deliverWebPush)
}

var webPushHTTPClient = newOutboundClient(10*time.Second, false) // 134.2: the endpoint came from a browser

// queueWebPush queues one message for one phone; key makes a repeat a no-op.
func queueWebPush(schema string, subID int64, msg WebPushMessage, key string) error {
	_, err := enqueueJobInSchema(schema, webPushJobType, map[string]interface{}{
		"subscription_id": subID, "title": msg.Title, "body": msg.Body, "url": msg.URL, "tag": msg.Tag,
	}, fmt.Sprintf("push:%d:%s", subID, key), "")
	return err
}

func recordWebPushOutcome(schema string, subID int64, sent bool, detail string) {
	if sent {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.web_push_subscriptions SET last_sent_at = NOW(), failures = 0, last_error = NULL WHERE id = $1`, schema), subID)
		return
	}
	if len(detail) > 300 {
		detail = detail[:300]
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.web_push_subscriptions SET failures = failures + 1, last_error = $2 WHERE id = $1`, schema), subID, detail)
}

// deliverWebPush is the web_push_delivery job: encrypt, sign, post. A push
// service that says the address is gone (404 / 410) ends the subscription; a
// busy one (429 / 5xx) or no answer is retried by the job runner.
func deliverWebPush(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
	subID := int64(numFromInterface(job.Payload["subscription_id"]))
	var endpoint, p256dh, auth string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT endpoint, p256dh, auth FROM %s.web_push_subscriptions WHERE id = $1`, schema), subID).Scan(&endpoint, &p256dh, &auth)
	if err == sql.ErrNoRows {
		return map[string]interface{}{"skipped": "alerts were turned off on that phone"}, nil
	}
	if err != nil {
		return nil, err
	}
	if isSandbox, _ := IsSandboxSchema(schema); isSandbox {
		recordWebPushOutcome(schema, subID, false, "sandbox client - not sent")
		return map[string]interface{}{"simulated": true}, nil
	}
	if !ExternalSideEffectsEnabled() {
		recordWebPushOutcome(schema, subID, false, "external side effects are off on this server - not sent")
		return map[string]interface{}{"simulated": true}, nil
	}
	if err := webPushEndpointAllowed(endpoint); err != nil {
		recordWebPushOutcome(schema, subID, false, err.Error())
		return map[string]interface{}{"refused": err.Error()}, nil
	}
	keys, err := webPushKeysForSchema(schema)
	if err != nil {
		return nil, err
	}
	ua, err := webPushDecodeKey(p256dh)
	if err != nil {
		return nil, err
	}
	secret, err := webPushDecodeKey(auth)
	if err != nil {
		return nil, err
	}
	msg := WebPushMessage{Title: strField(job.Payload, "title"), Body: strField(job.Payload, "body"), URL: strField(job.Payload, "url"), Tag: strField(job.Payload, "tag")}
	plain, _ := json.Marshal(msg)
	body, err := webPushEncrypt(ua, secret, plain, nil, nil)
	if err != nil {
		recordWebPushOutcome(schema, subID, false, err.Error())
		return map[string]interface{}{"refused": err.Error()}, nil
	}
	authz, err := webPushVAPIDAuth(keys, endpoint, time.Now())
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authz)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "43200")
	urgency := "normal"
	if msg.Tag == "approvals" {
		urgency = "high"
	}
	req.Header.Set("Urgency", urgency)
	if msg.Tag != "" {
		// A newer alert of the same kind replaces one the phone has not
		// received yet, so a phone that was off gets one, not ten.
		req.Header.Set("Topic", msg.Tag)
	}
	resp, err := webPushHTTPClient.Do(req)
	if err != nil {
		recordWebPushOutcome(schema, subID, false, "push service not reached: "+err.Error())
		return nil, fmt.Errorf("push service not reached: %v", err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		recordWebPushOutcome(schema, subID, true, "")
		return map[string]interface{}{"status": resp.StatusCode}, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.web_push_subscriptions WHERE id = $1`, schema), subID)
		return map[string]interface{}{"gone": true}, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		recordWebPushOutcome(schema, subID, false, fmt.Sprintf("push service busy (HTTP %d)", resp.StatusCode))
		return nil, fmt.Errorf("push service answered HTTP %d", resp.StatusCode)
	}
	detail := fmt.Sprintf("push service refused it (HTTP %d) %s", resp.StatusCode, strings.TrimSpace(string(answer)))
	recordWebPushOutcome(schema, subID, false, detail)
	return map[string]interface{}{"refused": detail}, nil
}

// webPushNewRefs records what a scope is alerted about now and returns what
// is new since the last look. Refs no longer current are forgotten, so the
// same document coming back later alerts again. The first look at a scope
// only takes note (#baseline): turning alerts on never floods the phone.
func webPushNewRefs(schema, scope string, current []string) ([]string, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var baselined bool
	if err := tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.web_push_seen WHERE scope = $1 AND ref_key = '#baseline')`, schema), scope).Scan(&baselined); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.web_push_seen WHERE scope = $1 AND ref_key <> '#baseline' AND NOT (ref_key = ANY($2))`, schema), scope, pq.Array(current)); err != nil {
		return nil, err
	}
	var fresh []string
	for _, ref := range current {
		var got string
		err := tx.QueryRow(fmt.Sprintf(`INSERT INTO %s.web_push_seen (scope, ref_key) VALUES ($1, $2) ON CONFLICT DO NOTHING RETURNING ref_key`, schema), scope, ref).Scan(&got)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		fresh = append(fresh, got)
	}
	if !baselined {
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.web_push_seen (scope, ref_key) VALUES ($1, '#baseline') ON CONFLICT DO NOTHING`, schema), scope); err != nil {
			return nil, err
		}
		fresh = nil
	}
	return fresh, tx.Commit()
}

func webPushKey(refs []string) string {
	sorted := append([]string{}, refs...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(sum[:8])
}

type webPushSubRow struct {
	id                  int64
	userID, store       string
	approvals, lowStock bool
}

// runWebPushAlerts looks once at one client: new approvals per person and
// store, new low stock per store, each queued to the phones that want it.
func runWebPushAlerts(schema string) error {
	if !webPushTableExists(schema) {
		return nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, user_id, store, alert_approvals, alert_low_stock FROM %s.web_push_subscriptions`, schema))
	if err != nil {
		return err
	}
	var subs []webPushSubRow
	for rows.Next() {
		var s webPushSubRow
		if err := rows.Scan(&s.id, &s.userID, &s.store, &s.approvals, &s.lowStock); err != nil {
			rows.Close()
			return err
		}
		subs = append(subs, s)
	}
	rows.Close()
	if len(subs) == 0 {
		return nil
	}
	tenantID, err := tenantIDForSchema(schema)
	if err != nil {
		return err
	}

	type who struct{ user, store string }
	approvalSubs := map[who][]int64{}
	lowSubs := map[string][]int64{}
	for _, s := range subs {
		if s.approvals {
			approvalSubs[who{s.userID, s.store}] = append(approvalSubs[who{s.userID, s.store}], s.id)
		}
		if s.lowStock && s.store != "" {
			lowSubs[s.store] = append(lowSubs[s.store], s.id)
		}
	}
	for w, ids := range approvalSubs {
		if err := webPushApprovalAlerts(schema, tenantID, w.user, w.store, ids); err != nil {
			log.Printf("[WEBPUSH] approvals for %s at %s in %s: %v", w.user, w.store, schema, err)
		}
	}
	for store, ids := range lowSubs {
		if err := webPushLowStockAlerts(schema, store, ids); err != nil {
			log.Printf("[WEBPUSH] low stock at %s in %s: %v", store, schema, err)
		}
	}
	return nil
}

func webPushApprovalAlerts(schema, tenantID, userID, store string, subIDs []int64) error {
	live, err := ResolveLiveUserState(tenantID, userID)
	if err != nil {
		return nil // no longer an active user: nothing is theirs to decide
	}
	location := firstNonEmpty(store, live.LocationCode)
	inbox, err := ListTaskInbox(tenantID, TaskInboxScope{UserID: userID, Role: live.Role, Location: location})
	if err != nil {
		return err
	}
	var current []string
	titles := map[string]string{}
	for _, it := range inbox.Items {
		if it.Source != "Approval" {
			continue
		}
		ref := "appr:" + it.DocType + ":" + it.DocID
		current = append(current, ref)
		titles[ref] = humanizeDoctype(it.DocType) + " " + strings.TrimPrefix(it.Title, it.DocType+" ")
	}
	fresh, err := webPushNewRefs(schema, "user:"+userID+"@"+location, current)
	if err != nil || len(fresh) == 0 {
		return err
	}
	msg := WebPushMessage{Title: "Approvals", URL: "/store/#/approvals", Tag: "approvals"}
	if len(fresh) == 1 {
		msg.Body = titles[fresh[0]] + " is waiting for your approval."
	} else {
		msg.Body = fmt.Sprintf("%d new approvals are waiting for you.", len(fresh))
	}
	key := "appr:" + webPushKey(fresh)
	for _, id := range subIDs {
		if err := queueWebPush(schema, id, msg, key); err != nil {
			return err
		}
	}
	return nil
}

func webPushLowStockAlerts(schema, store string, subIDs []int64) error {
	low, err := inventoryLowStock(schema, store, 0)
	if err != nil {
		return err
	}
	var current []string
	free := map[string]float64{}
	for _, r := range low {
		sku := fmt.Sprint(r["sku"])
		ref := "low:" + sku
		current = append(current, ref)
		free[ref] = numFromInterface(r["free"])
	}
	fresh, err := webPushNewRefs(schema, "store:"+store, current)
	if err != nil || len(fresh) == 0 {
		return err
	}
	storeName := store
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'name', ''), id) FROM %s.documents WHERE doctype = 'Location' AND id = $1`, schema), store).Scan(&storeName)
	msg := WebPushMessage{Title: "Low stock", URL: "/store/#/low-stock", Tag: "low-stock"}
	if len(fresh) == 1 {
		sku := strings.TrimPrefix(fresh[0], "low:")
		name := sku
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'item_name', ''), NULLIF(data->>'name', ''), $1) FROM %s.documents WHERE doctype = 'Item' AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), sku).Scan(&name)
		if n := free[fresh[0]]; n > 0 {
			msg.Body = fmt.Sprintf("%s is running low at %s: %g left.", name, storeName, n)
		} else {
			msg.Body = fmt.Sprintf("%s has run out at %s.", name, storeName)
		}
		msg.URL = "/store/#/scan?code=" + url.QueryEscape(sku)
	} else {
		msg.Body = fmt.Sprintf("%d items are running low at %s.", len(fresh), storeName)
	}
	key := "low:" + webPushKey(fresh)
	for _, id := range subIDs {
		if err := queueWebPush(schema, id, msg, key); err != nil {
			return err
		}
	}
	return nil
}

// StartWebPushAlertWorker looks for new alerts on every client each interval.
func StartWebPushAlertWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() { // 134.7: one server runs each sweep
					continue
				}
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[WEBPUSH] could not list clients: %v", err)
					continue
				}
				for _, schema := range schemas {
					if err := runWebPushAlerts(schema); err != nil {
						log.Printf("[WEBPUSH] %s: %v", schema, err)
					}
				}
			}
		}
	}()
}

// webPushTableExists keeps the worker quiet on a client the Stage 102
// migration has not reached yet.
func webPushTableExists(schema string) bool {
	var exists bool
	err := db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+".web_push_subscriptions").Scan(&exists)
	return err == nil && exists
}
