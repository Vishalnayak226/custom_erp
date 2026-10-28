package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 47.11.8 (adversarial journey #14): with the network gone at a boundary, the
// operator gets a bounded answer that says what happened and what to do next,
// and nothing local flips to success. These run the real courier and channel
// HTTP path (doConnectorRequest, the outbound client) against servers that
// refuse the connection, drop it, hang, or answer with a server error.

// nd47118Courier is the real Delhivery adapter under its own provider code, so
// the test's credential cannot collide with a configured Delhivery one.
type nd47118Courier struct{ delhiveryAdapter }

func (nd47118Courier) Code() string { return "nd47118" }

func resetBreaker(keys ...string) {
	circuitBreakerMu.Lock()
	defer circuitBreakerMu.Unlock()
	for _, k := range keys {
		delete(circuitBreakerState, k)
	}
}

// refusedURL is an address nothing listens on.
func refusedURL(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr
}

// netDownServers starts the three failing answers; hang holds each request
// until the test ends.
func netDownServers(t *testing.T) (dropped, hanging, failing string) {
	drop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html><body>502 Bad Gateway from upstream proxy 10.0.0.7</body></html>"))
	}))
	t.Cleanup(drop.Close)
	t.Cleanup(hang.Close)
	t.Cleanup(fail.Close)
	t.Cleanup(func() { close(release) }) // runs first, so hang.Close does not wait
	return drop.URL, hang.URL, fail.URL
}

func TestNetworkDownCourierLeavesBookingUnchanged(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.doctype_meta WHERE name='NDRCase')`).Scan(&ready); err != nil || !ready {
		t.Skip("db/migrations_stage35_5_courier_integration.sql has not been applied")
	}
	registerCourierAdapter(nd47118Courier{})
	const bookingID = "TEST-NETDOWN-47118"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id=$1`, bookingID)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code='courier:nd47118'`)
	}
	cleanup()
	t.Cleanup(cleanup)
	if err := SaveCourierCredential("default", "nd47118", map[string]string{"api_token": "test-only", "pickup_name": "WH-47118"}); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]interface{}{"code": bookingID, "order_id": "TEST-ORDER-47118", "destination_pincode": "560001", "status": "AWB Assigned"})
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ($1,'LogisticsBooking',$2,'AWB Assigned','system')`, bookingID, data); err != nil {
		t.Fatal(err)
	}
	origURL := delhiveryBaseURL
	t.Cleanup(func() { delhiveryBaseURL = origURL; resetBreaker("courier:delhivery") })
	resetBreaker("courier:delhivery")

	// The booking is authoritative: still awaiting its AWB, no AWB recorded,
	// and no claim left behind that would block the retry.
	assertUnchanged := func(label string) {
		t.Helper()
		var status, awb, claim string
		if err := db.DB.QueryRow(`SELECT status, COALESCE(data->>'provider_awb',''), COALESCE(data->>'awb_allocation_claimed_at','') FROM `+schema+`.documents WHERE id=$1`, bookingID).Scan(&status, &awb, &claim); err != nil {
			t.Fatal(err)
		}
		if status != "AWB Assigned" || awb != "" || claim != "" {
			t.Fatalf("%s: booking changed (status=%q provider_awb=%q claim=%q)", label, status, awb, claim)
		}
	}

	dropped, hanging, failing := netDownServers(t)
	// 47.11.8a changed the last three: the request may have reached the
	// courier, so the claim is kept as "uncertain" (the retry asks the
	// courier first - courier_idempotent_47118a_test.go) and no AWB is
	// recorded. Each case starts from a fresh booking.
	assertUncertain := func(label string) {
		t.Helper()
		var status, awb, claim, state string
		if err := db.DB.QueryRow(`SELECT status, COALESCE(data->>'provider_awb',''), COALESCE(data->>'awb_allocation_claimed_at',''), COALESCE(data->>'awb_booking_state','') FROM `+schema+`.documents WHERE id=$1`, bookingID).Scan(&status, &awb, &claim, &state); err != nil {
			t.Fatal(err)
		}
		if status != "AWB Assigned" || awb != "" || claim == "" || state != "uncertain" {
			t.Fatalf("%s: want the claim kept as uncertain (status=%q provider_awb=%q claim=%q state=%q)", label, status, awb, claim, state)
		}
	}
	fresh := func() {
		if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET data = data - 'awb_allocation_claimed_at' - 'awb_booking_state' - 'awb_booking_provider' WHERE id=$1`, bookingID); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		label, url, want string
		mayHaveArrived   bool
	}{
		{"connection refused", refusedURL(t), "could not be reached - nothing was sent", false},
		{"connection dropped", dropped, "did not answer - no AWB was recorded here, but it may have received the request", true},
		{"no answer", hanging, "did not answer - no AWB was recorded here, but it may have received the request", true},
		{"server error", failing, "not working right now (HTTP 503) - no AWB was recorded here", true},
	} {
		fresh()
		delhiveryBaseURL = c.url
		// The request's own deadline stands in for the courier's 25-second
		// client timeout, which TestNetworkDownConnectorCallIsBounded covers.
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		start := time.Now()
		awb, err := AllocateCourierAWB(ctx, "default", "nd47118", bookingID, CourierShipmentRequest{RecipientName: "N D", WeightGrams: 500})
		elapsed := time.Since(start)
		cancel()
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != CourierUnreachableCode || !strings.Contains(verr.Message, c.want) {
			t.Fatalf("%s: want %s %q, got %#v (%v)", c.label, CourierUnreachableCode, c.want, err, err)
		}
		if strings.Contains(verr.Message, "127.0.0.1") || strings.Contains(verr.Message, "<html") || strings.Contains(verr.Message, "10.0.0.7") {
			t.Fatalf("%s: the operator's line leaks the raw answer: %q", c.label, verr.Message)
		}
		if awb.AWB != "" {
			t.Fatalf("%s: an AWB was reported without one being allocated: %#v", c.label, awb)
		}
		if elapsed > 5*time.Second {
			t.Fatalf("%s: took %v - the call must be bounded", c.label, elapsed)
		}
		if c.mayHaveArrived {
			assertUncertain(c.label)
		} else {
			assertUnchanged(c.label)
		}
	}
	fresh()

	// The emergency switch: nothing reaches the courier, the booking stays as
	// it was, and the refusal is the code pending work already waits on.
	var hits atomic.Int32
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "packages": []map[string]interface{}{{"waybill": "AWB-47118"}}})
	}))
	defer ok.Close()
	delhiveryBaseURL = ok.URL
	t.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "1")
	_, err = AllocateCourierAWB(context.Background(), "default", "nd47118", bookingID, CourierShipmentRequest{WeightGrams: 500})
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "CONN-0225" || !strings.Contains(verr.Message, "nothing was sent") || hits.Load() != 0 {
		t.Fatalf("switched off: want CONN-0225 with nothing sent, got %v (courier hit %d times)", err, hits.Load())
	}
	assertUnchanged("switched off")
	t.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "")

	// The safe next action works: once the courier answers, the same booking
	// allocates.
	awb, err := AllocateCourierAWB(context.Background(), "default", "nd47118", bookingID, CourierShipmentRequest{WeightGrams: 500})
	if err != nil || awb.AWB != "AWB-47118" || hits.Load() != 1 {
		t.Fatalf("retry after the outage: %#v %v (hits %d)", awb, err, hits.Load())
	}
}

func TestNetworkDownConnectorCallIsBounded(t *testing.T) {
	const key = "nd47118-channel"
	resetBreaker(key, "bigcommerce")
	t.Cleanup(func() { resetBreaker(key, "bigcommerce") })
	dropped, hanging, failing := netDownServers(t)

	// The client timeout itself, not only a caller's deadline, ends a hang.
	start := time.Now()
	_, _, err := doConnectorRequest(context.Background(), 300*time.Millisecond, http.MethodPost, hanging, nil, []byte(`{}`), key)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("a hung platform must fail within its timeout: err=%v after %v", err, time.Since(start))
	}
	if _, _, err := doConnectorRequest(context.Background(), 2*time.Second, http.MethodPost, dropped, nil, []byte(`{}`), key); err == nil {
		t.Fatal("a dropped connection must be an error, not an empty success")
	}
	if status, _, err := doConnectorRequest(context.Background(), 2*time.Second, http.MethodGet, failing, nil, nil, key); err != nil || status != http.StatusServiceUnavailable {
		t.Fatalf("a 5xx is handed to the caller as its status: %d %v", status, err)
	}
	resetBreaker(key)

	// Through a real connector: a publish to a platform that is down returns
	// no external id and a coded error, so the publish job cannot be marked
	// Published.
	origURL := bigCommerceBaseURL
	t.Cleanup(func() { bigCommerceBaseURL = origURL })
	cred := map[string]string{"access_token": "bc_test_token", "store_hash": "nd47118"}
	payload := ChannelProductPayload{ItemCode: "SKU-47118", Title: "Net down"}
	for label, base := range map[string]string{"refused": refusedURL(t), "dropped": dropped, "server error": failing} {
		bigCommerceBaseURL = func(storeHash string) string { return base + "/stores/" + storeHash + "/v3" }
		externalID, err := bigCommerceConnector{}.PublishProduct(context.Background(), cred, payload)
		var verr *ValidationError
		if externalID != "" || !errors.As(err, &verr) || verr.Code == "" {
			t.Fatalf("%s: want a coded failure and no external id, got %q %v", label, externalID, err)
		}
	}
	resetBreaker("bigcommerce")

	// The emergency switch stops the write before it leaves, with the code a
	// publish job stays Queued on; a read still goes, for reconciliation.
	var hits atomic.Int32
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"id": 47118}})
	}))
	defer ok.Close()
	bigCommerceBaseURL = func(storeHash string) string { return ok.URL + "/stores/" + storeHash + "/v3" }
	t.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "1")
	externalID, err := bigCommerceConnector{}.PublishProduct(context.Background(), cred, payload)
	var verr *ValidationError
	if externalID != "" || !errors.As(err, &verr) || verr.Code != "CONN-0225" || hits.Load() != 0 {
		t.Fatalf("switched off: want CONN-0225 and no call, got %q %v (hits %d)", externalID, err, hits.Load())
	}
	if status, _, err := doConnectorRequest(context.Background(), 2*time.Second, http.MethodGet, ok.URL, nil, nil, key); err != nil || status != http.StatusOK || hits.Load() != 1 {
		t.Fatalf("switched off: a read still goes: %d %v (hits %d)", status, err, hits.Load())
	}
}
