package engines

import (
	"context"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Stage 75.2.5: the IRP sits behind IRPProvider (the CourierAdapter pattern),
// so the queue, the screens and the reports never see a provider's own JSON or
// encryption. Only the fake provider exists until sandbox access (75.2.11
// builds IRIS's adapter to its current spec).

// IRPToken is an IRP session.
type IRPToken struct {
	Value     string
	ExpiresAt time.Time
}

// IRPAck is a generated (or found) IRN.
type IRPAck struct {
	IRN           string    `json:"irn"`
	AckNo         string    `json:"ack_no"`
	AckDate       time.Time `json:"ack_date"`
	SignedInvoice string    `json:"signed_invoice"`
	SignedQR      string    `json:"signed_qr"`
}

// IRPErrorKind is how the queue must react to a provider error.
type IRPErrorKind string

const (
	IRPRejected       IRPErrorKind = "REJECTED"        // the IRP refused the document: fix it, never retry as is
	IRPDuplicate      IRPErrorKind = "DUPLICATE"       // already registered: look it up
	IRPMaybeAccepted  IRPErrorKind = "MAYBE_ACCEPTED"  // timeout after sending: UNKNOWN, look up before any retry
	IRPAuthFailed     IRPErrorKind = "AUTH_FAILED"     // 401: refresh the token
	IRPRetryLater     IRPErrorKind = "RETRY_LATER"     // 429 / 5xx before acceptance: back off
	IRPIPBlocked      IRPErrorKind = "IP_BLOCKED"      // this server's IP is not whitelisted
	IRPNotFound       IRPErrorKind = "NOT_FOUND"       // a lookup found nothing
	IRPWindowExpired  IRPErrorKind = "WINDOW_EXPIRED"  // cancel after 24 h
	IRPProviderFailed IRPErrorKind = "PROVIDER_FAILED" // anything else
)

// IRPError is a classified provider error. Message is safe to store and
// show: it never carries a token, a key or the payload.
type IRPError struct {
	Kind    IRPErrorKind
	Code    string
	Message string
}

func (e *IRPError) Error() string { return fmt.Sprintf("%s %s: %s", e.Kind, e.Code, e.Message) }

func irpErrorKind(err error) IRPErrorKind {
	var e *IRPError
	if errors.As(err, &e) {
		return e.Kind
	}
	return IRPProviderFailed
}

// IRPProvider is one IRP (or GSP). credentials come from channel_credentials.
type IRPProvider interface {
	Code() string
	Health(ctx context.Context) error
	Authenticate(ctx context.Context, gstin string, credentials map[string]string) (IRPToken, error)
	GenerateIRN(ctx context.Context, token IRPToken, gstin string, payload []byte) (IRPAck, error)
	GetByIRN(ctx context.Context, token IRPToken, gstin, irn string) (IRPAck, error)
	GetByDocument(ctx context.Context, token IRPToken, gstin, docType, docNumber, docDate string) (IRPAck, error)
	CancelIRN(ctx context.Context, token IRPToken, gstin, irn, reasonCode, remark string) (time.Time, error)
}

var (
	irpProvidersMu sync.RWMutex
	irpProviders   = map[string]IRPProvider{}
)

// RegisterIRPProvider adds a provider by its code.
func RegisterIRPProvider(p IRPProvider) {
	irpProvidersMu.Lock()
	defer irpProvidersMu.Unlock()
	irpProviders[strings.ToLower(p.Code())] = p
}

func irpProviderFor(code string) (IRPProvider, bool) {
	irpProvidersMu.RLock()
	defer irpProvidersMu.RUnlock()
	p, ok := irpProviders[strings.ToLower(strings.TrimSpace(code))]
	return p, ok
}

func init() { RegisterIRPProvider(FakeIRP) }

// ---------------------------------------------------------------------------
// Token cache (75.2.6): per (environment, GSTIN), refreshed under a per-GSTIN
// lock so two workers never authenticate the same GSTIN at once.
// ---------------------------------------------------------------------------

var (
	irpTokenMu    sync.Mutex
	irpTokens     = map[string]IRPToken{}
	irpTokenLocks = map[string]*sync.Mutex{}
)

func irpTokenKey(environment, gstin string) string { return environment + "|" + gstin }

func irpGSTINLock(key string) *sync.Mutex {
	irpTokenMu.Lock()
	defer irpTokenMu.Unlock()
	if irpTokenLocks[key] == nil {
		irpTokenLocks[key] = &sync.Mutex{}
	}
	return irpTokenLocks[key]
}

// irpForgetToken drops a cached token (new credentials, or a 401).
func irpForgetToken(environment, gstin string) {
	irpTokenMu.Lock()
	delete(irpTokens, irpTokenKey(environment, gstin))
	irpTokenMu.Unlock()
}

// irpTokenFor returns a live token, authenticating under the GSTIN's lock
// when there is none (or it is about to expire).
func irpTokenFor(ctx context.Context, tenantID string, p IRPProvider, environment, gstin string) (IRPToken, error) {
	key := irpTokenKey(environment, gstin)
	irpTokenMu.Lock()
	t, ok := irpTokens[key]
	irpTokenMu.Unlock()
	if ok && time.Until(t.ExpiresAt) > time.Minute {
		return t, nil
	}
	lock := irpGSTINLock(key)
	lock.Lock()
	defer lock.Unlock()
	irpTokenMu.Lock()
	t, ok = irpTokens[key]
	irpTokenMu.Unlock()
	if ok && time.Until(t.ExpiresAt) > time.Minute {
		return t, nil // another worker refreshed it while this one waited
	}
	creds, _ := getChannelCredential(tenantID, irpCredentialChannel(environment, gstin))
	t, err := p.Authenticate(ctx, gstin, creds)
	if err != nil {
		return IRPToken{}, err
	}
	irpTokenMu.Lock()
	irpTokens[key] = t
	irpTokenMu.Unlock()
	return t, nil
}

// ---------------------------------------------------------------------------
// The fake provider (75.2.5): plays back scripted outcomes. Default is
// success. Tests script a document number, or the next calls; the IRN is the
// SHA-256 of GSTIN + year + type + number, as a real IRN is.
// ---------------------------------------------------------------------------

// Fake outcomes.
const (
	FakeIRPSuccess         = "success"
	FakeIRPReject          = "reject"
	FakeIRPTimeoutAccepted = "timeout_after_accept"
	FakeIRPDuplicate       = "already_registered"
	FakeIRPUnauthorized    = "401"
	FakeIRPRateLimited     = "429"
	FakeIRPServerError     = "5xx"
	FakeIRPIPBlocked       = "ip_blocked"
)

type fakeIRPProvider struct {
	mu        sync.Mutex
	script    map[string][]string // document number -> outcomes, in order
	authFails int
	issued    map[string]IRPAck // "gstin|type|number" -> ack
	cancelled map[string]time.Time
	authCalls int
	genCalls  map[string]int
}

// FakeIRP is the test provider every server has.
var FakeIRP = &fakeIRPProvider{script: map[string][]string{}, issued: map[string]IRPAck{}, cancelled: map[string]time.Time{}, genCalls: map[string]int{}}

func (f *fakeIRPProvider) Code() string { return "fake" }

// Script queues outcomes for a document number's next generate calls.
func (f *fakeIRPProvider) Script(docNumber string, outcomes ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script[strings.ToUpper(docNumber)] = append(f.script[strings.ToUpper(docNumber)], outcomes...)
}

// FailAuth makes the next n Authenticate calls fail with 401.
func (f *fakeIRPProvider) FailAuth(n int) {
	f.mu.Lock()
	f.authFails = n
	f.mu.Unlock()
}

// GenerateCalls is how many generate calls reached the fake for a number.
func (f *fakeIRPProvider) GenerateCalls(docNumber string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.genCalls[strings.ToUpper(docNumber)]
}

// Reset forgets every script, issue and counter (tests).
func (f *fakeIRPProvider) Reset() {
	f.mu.Lock()
	f.script, f.issued, f.cancelled, f.genCalls, f.authFails, f.authCalls = map[string][]string{}, map[string]IRPAck{}, map[string]time.Time{}, map[string]int{}, 0, 0
	f.mu.Unlock()
}

func (f *fakeIRPProvider) Health(ctx context.Context) error { return nil }

func (f *fakeIRPProvider) Authenticate(ctx context.Context, gstin string, _ map[string]string) (IRPToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authCalls++
	if f.authFails > 0 {
		f.authFails--
		return IRPToken{}, &IRPError{Kind: IRPAuthFailed, Code: "401", Message: "invalid credentials"}
	}
	return IRPToken{Value: fmt.Sprintf("fake-token-%d", f.authCalls), ExpiresAt: time.Now().Add(6 * time.Hour)}, nil
}

type fakeIRPPayload struct {
	DocDtls IRPDocDtls `json:"DocDtls"`
}

func (f *fakeIRPProvider) next(number string) string {
	q := f.script[number]
	if len(q) == 0 {
		return FakeIRPSuccess
	}
	f.script[number] = q[1:]
	return q[0]
}

func fakeIRN(gstin, docType, number, date string) string {
	sum := sha256.Sum256([]byte(gstin + "|" + date[len(date)-4:] + "|" + docType + "|" + number))
	return hex.EncodeToString(sum[:])
}

func fakeSigned(claims map[string]interface{}) string {
	b, _ := json.Marshal(claims)
	return "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9." + base64.RawURLEncoding.EncodeToString(b) + ".FAKE-SIGNATURE-NOT-VALID-AT-ANY-IRP"
}

func (f *fakeIRPProvider) GenerateIRN(ctx context.Context, token IRPToken, gstin string, payload []byte) (IRPAck, error) {
	var p fakeIRPPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return IRPAck{}, &IRPError{Kind: IRPRejected, Code: "2172", Message: "the JSON is not an e-invoice"}
	}
	number := strings.ToUpper(p.DocDtls.No)
	key := gstin + "|" + p.DocDtls.Typ + "|" + number
	f.mu.Lock()
	defer f.mu.Unlock()
	f.genCalls[number]++
	outcome := f.next(number)
	issue := func() IRPAck {
		if ack, ok := f.issued[key]; ok {
			return ack
		}
		irn := fakeIRN(gstin, p.DocDtls.Typ, number, p.DocDtls.Dt)
		ack := IRPAck{IRN: irn, AckNo: fmt.Sprintf("1%014d", len(f.issued)+1), AckDate: time.Now().UTC().Truncate(time.Second)}
		ack.SignedQR = fakeSigned(map[string]interface{}{"SellerGstin": gstin, "DocNo": number, "DocTyp": p.DocDtls.Typ, "DocDt": p.DocDtls.Dt, "Irn": irn, "IrnDt": ack.AckDate.Format("2006-01-02 15:04:05")})
		ack.SignedInvoice = fakeSigned(map[string]interface{}{"Irn": irn, "AckNo": ack.AckNo, "data": json.RawMessage(payload)})
		f.issued[key] = ack
		return ack
	}
	switch outcome {
	case FakeIRPReject:
		return IRPAck{}, &IRPError{Kind: IRPRejected, Code: "2265", Message: "recipient GSTIN is not valid for this document (scripted)"}
	case FakeIRPTimeoutAccepted:
		issue() // the IRP registered it, the answer never arrived
		return IRPAck{}, &IRPError{Kind: IRPMaybeAccepted, Code: "TIMEOUT", Message: "no response after sending (scripted)"}
	case FakeIRPDuplicate:
		issue()
		return IRPAck{}, &IRPError{Kind: IRPDuplicate, Code: "2150", Message: "duplicate IRN (scripted)"}
	case FakeIRPUnauthorized:
		return IRPAck{}, &IRPError{Kind: IRPAuthFailed, Code: "401", Message: "token expired (scripted)"}
	case FakeIRPRateLimited:
		return IRPAck{}, &IRPError{Kind: IRPRetryLater, Code: "429", Message: "too many requests (scripted)"}
	case FakeIRPServerError:
		return IRPAck{}, &IRPError{Kind: IRPRetryLater, Code: "503", Message: "service unavailable (scripted)"}
	case FakeIRPIPBlocked:
		return IRPAck{}, &IRPError{Kind: IRPIPBlocked, Code: "403", Message: "IP address not whitelisted (scripted)"}
	}
	if _, done := f.issued[key]; done {
		return IRPAck{}, &IRPError{Kind: IRPDuplicate, Code: "2150", Message: "duplicate IRN"}
	}
	return issue(), nil
}

func (f *fakeIRPProvider) GetByIRN(ctx context.Context, token IRPToken, gstin, irn string) (IRPAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, ack := range f.issued {
		if strings.HasPrefix(k, gstin+"|") && ack.IRN == irn {
			return ack, nil
		}
	}
	return IRPAck{}, &IRPError{Kind: IRPNotFound, Code: "2283", Message: "IRN not found"}
}

func (f *fakeIRPProvider) GetByDocument(ctx context.Context, token IRPToken, gstin, docType, docNumber, docDate string) (IRPAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ack, ok := f.issued[gstin+"|"+docType+"|"+strings.ToUpper(docNumber)]; ok {
		return ack, nil
	}
	return IRPAck{}, &IRPError{Kind: IRPNotFound, Code: "2283", Message: "no IRN for this document"}
}

func (f *fakeIRPProvider) CancelIRN(ctx context.Context, token IRPToken, gstin, irn, reasonCode, remark string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ack := range f.issued {
		if ack.IRN != irn {
			continue
		}
		if time.Since(ack.AckDate) > 24*time.Hour {
			return time.Time{}, &IRPError{Kind: IRPWindowExpired, Code: "9999", Message: "the 24-hour cancellation window has passed"}
		}
		if at, done := f.cancelled[irn]; done {
			return at, nil
		}
		at := time.Now().UTC().Truncate(time.Second)
		f.cancelled[irn] = at
		return at, nil
	}
	return time.Time{}, &IRPError{Kind: IRPNotFound, Code: "2283", Message: "IRN not found"}
}

// TestIRPConnection (75.2.9) checks the provider answers and the GSTIN's
// credentials sign in, and records the result on the profile.
func TestIRPConnection(ctx context.Context, tenantID, gstin, userID string) (IRPProfile, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return IRPProfile{}, err
	}
	gstin = strings.ToUpper(strings.TrimSpace(gstin))
	p, err := irpProfileFor(schema, gstin)
	if err != nil {
		return IRPProfile{}, err
	}
	if p == nil {
		return IRPProfile{}, einvoiceError("EINV-0980", "GSTIN", "set up GSTIN "+gstin+" first")
	}
	status := "Connected"
	provider, ok := irpProviderFor(p.Provider)
	if !ok {
		status = "Provider not available on this server"
	} else if err := provider.Health(ctx); err != nil {
		status = "Provider not answering: " + redactAndBoundLogField(err.Error(), 200)
	} else {
		irpForgetToken(p.Environment, gstin)
		if _, err := irpTokenFor(ctx, tenantID, provider, p.Environment, gstin); err != nil {
			status = "Sign-in failed: " + redactAndBoundLogField(err.Error(), 200)
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.irp_profiles SET test_status = $3, tested_at = NOW() WHERE environment = $1 AND gstin = $2`, schema), p.Environment, gstin, status); err != nil {
		return *p, err
	}
	irpEvent(schema, 0, gstin, "CONNECTION_TESTED", "", "", status, userID)
	out, err := irpProfileFor(schema, gstin)
	if err != nil || out == nil {
		return *p, err
	}
	out.CredentialSet, _ = HasChannelCredential(tenantID, irpCredentialChannel(out.Environment, gstin))
	return *out, nil
}
