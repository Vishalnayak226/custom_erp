package engines

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Stage 49.8.8 - the integration abuse lab. Every partner here is a local
// httptest server (loopback only; TestMain allows loopback for this package,
// and nothing below depends on outside calls being switched on). What the
// rest of the suite already proves is cited, not repeated:
//
//   - forged / wrong-secret / blank signature refused, previous secret
//     honoured for the overlap, duplicate delivery claimed once and a failed
//     one released for the retry: TestStage134InboundWebhook (49.8.3)
//   - a client-typed address that is or resolves to loopback / private /
//     link-local refused at dial time, https->http and >3 redirects refused,
//     an answer over the cap refused: TestStage134OutboundGuard (49.8.4)
//   - a failing job retried then dead-lettered: TestStage386JobRunner
//   - an extension hook that times out or answers non-2xx blocks the save:
//     TestInvokeBeforeSaveHooksBlocksOnTimeout / ...BlocksOnNon2xx
//
// Added here: a provider that hangs or drips its answer, an answer too large
// to read, an untrusted TLS certificate and the breaker it trips, a webhook
// endpoint that redirects the signed delivery elsewhere, a courier tracking
// feed that repeats and re-orders events, and a poison job whose handler
// panics.

// abuseServer is a loopback server whose handler stops as soon as the
// client goes away or the test ends, so Close never waits on a sleeping
// handler.
func abuseServer(t *testing.T, tls bool, h func(w http.ResponseWriter, r *http.Request, done <-chan struct{})) *httptest.Server {
	t.Helper()
	done := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h(w, r, done) })
	var srv *httptest.Server
	if tls {
		srv = httptest.NewTLSServer(handler)
	} else {
		srv = httptest.NewServer(handler)
	}
	t.Cleanup(func() { close(done); srv.Close() })
	return srv
}

// A provider that never answers, or answers one byte at a time (slowloris in
// reverse), costs a connector call its timeout and no more.
func TestStage4988SlowProviderIsCutOffAtTheTimeout(t *testing.T) {
	hang := abuseServer(t, false, func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
		select {
		case <-r.Context().Done():
		case <-done:
		}
	})
	drip := abuseServer(t, false, func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
		w.WriteHeader(http.StatusOK)
		for {
			if _, err := w.Write([]byte("x")); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-done:
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	})
	for name, url := range map[string]string{"no answer": hang.URL, "dripped answer": drip.URL} {
		key := "abuse4988-slow-" + name
		resetBreaker(key)
		start := time.Now()
		_, _, err := doConnectorRequest(context.Background(), 400*time.Millisecond, http.MethodGet, url, nil, nil, key)
		if err == nil {
			t.Errorf("%s: a provider that does not finish answering must fail the call", name)
		}
		if took := time.Since(start); took > 3*time.Second {
			t.Errorf("%s: the call took %v - it must end at its 400ms timeout, not wait on the provider", name, took)
		}
		resetBreaker(key)
	}

	// A webhook delivery is bounded by the job's own context the same way.
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := sendWebhookHTTPContext(ctx, hang.URL, "lab-secret", "t4988.slow", map[string]interface{}{"n": 1}, "JOB-4988-SLOW"); err == nil {
		t.Error("a webhook endpoint that never answers must fail the delivery")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the webhook delivery took %v - it must end with its context", took)
	}
}

// An answer larger than the cap is refused rather than read into memory; a
// webhook endpoint that streams an endless body does not hold the delivery.
func TestStage4988OversizedAnswerIsBounded(t *testing.T) {
	big := abuseServer(t, false, func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
		w.WriteHeader(http.StatusOK)
		chunk := []byte(strings.Repeat("y", 1<<20))
		for written := 0; written <= connectorMaxResponseBytes; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	key := "abuse4988-big"
	resetBreaker(key)
	defer resetBreaker(key)
	_, body, err := doConnectorRequest(context.Background(), 20*time.Second, http.MethodGet, big.URL, nil, nil, key)
	if err == nil || !strings.Contains(err.Error(), "larger than") || body != nil {
		t.Fatalf("an answer over %d bytes must be refused, not read: body=%d bytes err=%v", connectorMaxResponseBytes, len(body), err)
	}

	endless := abuseServer(t, false, func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
		w.WriteHeader(http.StatusOK)
		chunk := []byte(strings.Repeat("z", 32<<10))
		for {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-done:
				return
			default:
			}
		}
	})
	start := time.Now()
	res, err := sendWebhookHTTP(endless.URL, "lab-secret", "t4988.big", map[string]interface{}{"n": 1}, "JOB-4988-BIG")
	if err != nil || res["status_code"] != http.StatusOK {
		t.Fatalf("a 200 with an endless body is a delivered webhook: %v %v", res, err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("the delivery read for %v - it must stop after %d bytes of the answer", took, webhookMaxResponseBodyBytes)
	}
}

// A certificate the system does not trust fails the call - there is no
// skip-verify path - and five such failures in a row open the breaker, so the
// sixth call does not reach the network at all.
func TestStage4988UntrustedTLSFailsAndTripsTheBreaker(t *testing.T) {
	var served int32
	srv := abuseServer(t, true, func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
		atomic.AddInt32(&served, 1)
		w.WriteHeader(http.StatusOK)
	})
	key := "abuse4988-tls"
	resetBreaker(key)
	defer resetBreaker(key)
	for i := 0; i < circuitBreakerFailureThreshold; i++ {
		_, _, err := doConnectorRequest(context.Background(), 5*time.Second, http.MethodGet, srv.URL, nil, nil, key)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "certificate") {
			t.Fatalf("call %d to a self-signed server must fail on the certificate: %v", i+1, err)
		}
	}
	_, _, err := doConnectorRequest(context.Background(), 5*time.Second, http.MethodGet, srv.URL, nil, nil, key)
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "CONN-0225" {
		t.Fatalf("after %d failures the breaker must refuse the next call without trying it: %v", circuitBreakerFailureThreshold, err)
	}
	if n := atomic.LoadInt32(&served); n != 0 {
		t.Fatalf("the untrusted server answered %d request(s) - no request may be sent over an unverified connection", n)
	}
}

// A webhook endpoint that answers with a redirect does not get the signed
// body re-sent to wherever it points (a 307 would replay the POST): the
// delivery fails on the 3xx and the redirect target is never contacted.
func TestStage4988WebhookRedirectIsNotFollowed(t *testing.T) {
	var stolen int32
	target := abuseServer(t, false, func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
		atomic.AddInt32(&stolen, 1)
		w.WriteHeader(http.StatusOK)
	})
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		code := code
		hook := abuseServer(t, false, func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
			http.Redirect(w, r, target.URL+"/latest/meta-data/", code)
		})
		_, err := sendWebhookHTTP(hook.URL, "lab-secret", "t4988.redirect", map[string]interface{}{"n": 1}, "JOB-4988-REDIR")
		if err == nil || !strings.Contains(err.Error(), "HTTP") {
			t.Errorf("a %d answer is a failed delivery, retried by the job runner: %v", code, err)
		}
	}
	if n := atomic.LoadInt32(&stolen); n != 0 {
		t.Fatalf("the redirect target received %d signed deliveries - redirects must not be followed", n)
	}
}

type abuseCourierStub struct{ stage35CourierStub }

func (abuseCourierStub) Code() string { return "abuse4988stub" }

// A courier feed that repeats an event, or delivers an older one after a
// newer one, never moves a shipment backwards or opens a case on a delivered
// parcel. The receipt store forgets a delivery after its 30-day window; the
// event-id check below it is permanent, so a much later replay is still a
// no-op.
func TestStage4988CourierFeedRepeatsAndReordersCannotRegress(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.doctype_meta WHERE name='NDRCase')`).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Skip("db/migrations_stage35_5_courier_integration.sql has not been applied")
	}
	registerCourierAdapter(abuseCourierStub{})
	if err := SaveCourierCredential("default", "abuse4988stub", map[string]string{"api_key": "test-only", "webhook_secret": "lab-secret"}); err != nil {
		t.Fatal(err)
	}
	const bookingID, awb = "TEST-ABUSE-4988", "LAB-AWB-4988"
	wipe := func() {
		db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id=$1 OR (doctype IN ('NDRCase','CourierTrackingEvent') AND data->>'booking_id'=$1)`, bookingID)
	}
	wipe()
	t.Cleanup(func() {
		wipe()
		db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code='courier:abuse4988stub'`)
	})
	data, _ := json.Marshal(map[string]interface{}{"code": bookingID, "awb_number": awb, "tracking_number": awb, "status": "Handed Over"})
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ($1,'LogisticsBooking',$2,'Handed Over','system')`, bookingID, data); err != nil {
		t.Fatal(err)
	}

	event := func(id, status string) []byte {
		b, _ := json.Marshal(CourierTrackingEvent{AWB: awb, Status: status, EventID: id, Reason: "lab"})
		return b
	}
	sign := func(secret string, body []byte) string {
		m := hmac.New(sha256.New, []byte(secret))
		m.Write(body)
		return hex.EncodeToString(m.Sum(nil))
	}
	delivered := event("E-2", "Delivered")
	if err := VerifyCourierWebhook("default", "abuse4988stub", sign("not-the-secret", delivered), delivered); err == nil {
		t.Fatal("an event signed with another secret is refused")
	}
	if err := VerifyCourierWebhook("default", "abuse4988stub", sign("lab-secret", delivered), append(delivered, ' ')); err == nil {
		t.Fatal("a body changed after signing is refused")
	}
	if err := VerifyCourierWebhook("default", "abuse4988stub", sign("lab-secret", delivered), delivered); err != nil {
		t.Fatalf("the genuine event verifies: %v", err)
	}

	status := func() string {
		var s string
		db.DB.QueryRow(`SELECT status FROM `+schema+`.documents WHERE id=$1`, bookingID).Scan(&s)
		return s
	}
	count := func(doctype string) (n int) {
		db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.documents WHERE doctype=$1 AND data->>'booking_id'=$2`, doctype, bookingID).Scan(&n)
		return
	}
	if _, err := IngestCourierTrackingWebhook("default", "abuse4988stub", delivered); err != nil || status() != "Delivered" {
		t.Fatalf("the delivered event moves the shipment: status=%q err=%v", status(), err)
	}
	for i := 0; i < 2; i++ { // a repeat, and a replay long after the receipt store let it go
		if _, err := IngestCourierTrackingWebhook("default", "abuse4988stub", delivered); err != nil {
			t.Fatalf("a repeated event is acknowledged as already seen: %v", err)
		}
	}
	if n := count("CourierTrackingEvent"); n != 1 {
		t.Fatalf("a repeated event is recorded once, got %d records", n)
	}

	// Older events arriving after the delivery.
	for _, late := range []struct{ id, status string }{{"E-1", "In Transit"}, {"E-0", "Undelivered - consignee unavailable"}, {"E-3", "RTO Initiated"}} {
		_, err := IngestCourierTrackingWebhook("default", "abuse4988stub", event(late.id, late.status))
		if got := status(); got != "Delivered" {
			t.Fatalf("a late %q event moved a delivered shipment back to %q", late.status, got)
		}
		t.Logf("late %q after Delivered: shipment stays Delivered; ingest answered err=%v", late.status, err)
	}
	if n := count("NDRCase"); n != 0 {
		t.Fatalf("a late failed-attempt event opened %d NDR case(s) on a delivered parcel", n)
	}
}

// A poison job - a payload that makes its handler panic - is a failed attempt
// like any other: the worker survives it, the next job in the same batch
// still runs, and the poison job is retried and then dead-lettered.
func TestStage4988PoisonJobIsDeadLetteredNotFatal(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	for name, h := range map[string]JobHandlerFunc{
		"t4988-poison": func(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
			var m map[string]int
			m["boom"]++ // a nil map write: the shape of a real handler bug
			return nil, nil
		},
		"t4988-healthy": func(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
			return map[string]interface{}{"ok": true}, nil
		},
	} {
		if _, exists := jobHandlers[name]; !exists {
			RegisterJobHandler(name, h)
		}
	}
	poisonID, err := EnqueueJob(tenantID, "t4988-poison", map[string]interface{}{"n": 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	healthyID, err := EnqueueJob(tenantID, "t4988-healthy", map[string]interface{}{"n": 2}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE `+schema+`.async_jobs SET max_attempts = 2, next_attempt_at = next_attempt_at - interval '1 minute' WHERE id = $1`, poisonID); err != nil {
		t.Fatal(err)
	}
	job := func(id string) (status, lastError string, attempts int) {
		db.DB.QueryRow(`SELECT status, COALESCE(last_error,''), attempts FROM `+schema+`.async_jobs WHERE id = $1`, id).Scan(&status, &lastError, &attempts)
		return
	}

	runClaimedJobs(schema) // panics without the recover in runClaimedJobsContext
	if s, e, a := job(poisonID); s != "Pending" || a != 1 || !strings.Contains(e, "panicked") {
		t.Fatalf("after one panic the job waits for a retry: status=%s attempts=%d last_error=%q", s, a, e)
	}
	if s, _, _ := job(healthyID); s != "Succeeded" {
		t.Fatalf("the job after the poison one in the same batch still runs: %s", s)
	}

	db.DB.Exec(`UPDATE `+schema+`.async_jobs SET next_attempt_at = CURRENT_TIMESTAMP - interval '1 second' WHERE id = $1`, poisonID)
	runClaimedJobs(schema)
	if s, e, a := job(poisonID); s != "DeadLettered" || a != 2 || !strings.Contains(e, "panicked") {
		t.Fatalf("at max_attempts the poison job is dead-lettered: status=%s attempts=%d last_error=%q", s, a, e)
	}
}
