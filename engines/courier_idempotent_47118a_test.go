package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 47.11.8a: nothing is booked twice with a courier. These run the real
// Delhivery adapter (and a courier with no lookup) through the real outbound
// path against a loopback courier that counts its booking and lookup calls.

// nd47118aCourier is the real Delhivery adapter, lookup included, under its
// own provider code so its test credential cannot collide with a real one.
type nd47118aCourier struct{ delhiveryAdapter }

func (nd47118aCourier) Code() string { return "nd47118a" }

const (
	fakeBookOK          = iota // book and answer
	fakeBookThenHang           // book, then never answer (the timeout after sending)
	fakeBookFailNoTrace        // answer 503 without booking
)

// fakeDelhivery holds what the courier has booked, by the order id sent.
type fakeDelhivery struct {
	mu          sync.Mutex
	booked      map[string]string
	orders      []string
	creates     atomic.Int32
	lookups     atomic.Int32
	mode        atomic.Int32
	lookupDelay time.Duration
	createDelay time.Duration
}

func newFakeDelhivery(t *testing.T) (*fakeDelhivery, string) {
	f := &fakeDelhivery{booked: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/cmu/create.json"):
			n := f.creates.Add(1)
			_ = r.ParseForm()
			var payload struct {
				Shipments []struct {
					Order string `json:"order"`
				} `json:"shipments"`
			}
			_ = json.Unmarshal([]byte(r.Form.Get("data")), &payload)
			order := ""
			if len(payload.Shipments) > 0 {
				order = payload.Shipments[0].Order
			}
			if f.createDelay > 0 {
				time.Sleep(f.createDelay)
			}
			mode := f.mode.Load()
			if mode == fakeBookFailNoTrace {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			awb := "FAKE-AWB-" + strconv.Itoa(int(n))
			f.mu.Lock()
			f.booked[order] = awb
			f.orders = append(f.orders, order)
			f.mu.Unlock()
			if mode == fakeBookThenHang {
				<-r.Context().Done()
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "packages": []map[string]interface{}{{"waybill": awb}}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/api/v1/packages/json/"):
			f.lookups.Add(1)
			if f.lookupDelay > 0 {
				time.Sleep(f.lookupDelay)
			}
			ref := r.URL.Query().Get("ref_ids")
			f.mu.Lock()
			awb, ok := f.booked[ref]
			f.mu.Unlock()
			if !ok {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"ShipmentData": []interface{}{}, "Error": "No such waybill or Order Id found"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ShipmentData": []map[string]interface{}{{"Shipment": map[string]interface{}{"AWB": awb, "ReferenceNo": ref}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv.URL
}

// noLookup47118a books over HTTP but cannot ask the courier for a booking.
type noLookup47118a struct{}

var (
	noLookupURL   string
	noLookupCalls atomic.Int32
	noLookupHang  atomic.Bool
)

func (noLookup47118a) Code() string { return "nolookup47118a" }
func (noLookup47118a) AllocateAWB(ctx context.Context, _ map[string]string, req CourierShipmentRequest) (CourierAWB, error) {
	status, body, err := doConnectorRequest(ctx, 3*time.Second, http.MethodPost, noLookupURL+"/book", map[string]string{"Content-Type": "application/json"}, []byte(`{"ref":"`+req.Reference+`"}`), "courier:nolookup47118a")
	if err != nil {
		return CourierAWB{}, err
	}
	if status != http.StatusOK {
		return CourierAWB{}, &ConnectorHTTPError{Platform: "nolookup47118a", Status: status}
	}
	var out struct {
		AWB string `json:"awb"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return CourierAWB{}, err
	}
	return CourierAWB{AWB: out.AWB}, nil
}
func (noLookup47118a) SchedulePickup(context.Context, map[string]string, CourierPickupRequest) (string, error) {
	return "", errors.New("not used")
}
func (noLookup47118a) CancelShipment(context.Context, map[string]string, string) error { return nil }
func (noLookup47118a) Rates(context.Context, map[string]string, CourierRateRequest) ([]CourierRate, error) {
	return nil, nil
}
func (noLookup47118a) ParseTrackingWebhook([]byte) (CourierTrackingEvent, error) {
	return CourierTrackingEvent{}, errors.New("not used")
}

// courier47118aSetup returns the schema and a booking maker; everything it
// makes is deleted afterwards.
func courier47118aSetup(t *testing.T) (string, func(id string, extra map[string]interface{})) {
	t.Helper()
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.doctype_meta WHERE name='NDRCase')`).Scan(&ready); err != nil || !ready {
		t.Skip("db/migrations_stage35_5_courier_integration.sql has not been applied")
	}
	registerCourierAdapter(nd47118aCourier{})
	registerCourierAdapter(noLookup47118a{})
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE doctype='LogisticsBooking' AND id LIKE 'TEST-47118A-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code IN ('courier:nd47118a','courier:nolookup47118a')`)
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, p := range []string{"nd47118a", "nolookup47118a"} {
		if err := SaveCourierCredential("default", p, map[string]string{"api_token": "test-only", "pickup_name": "WH-47118A"}); err != nil {
			t.Fatal(err)
		}
	}
	origURL := delhiveryBaseURL
	t.Cleanup(func() { delhiveryBaseURL = origURL; resetBreaker("courier:delhivery", "courier:nolookup47118a") })
	resetBreaker("courier:delhivery", "courier:nolookup47118a")
	return schema, func(id string, extra map[string]interface{}) {
		t.Helper()
		data := map[string]interface{}{"code": id, "order_id": "TEST-ORDER-47118A", "destination_pincode": "560001", "status": "AWB Assigned"}
		for k, v := range extra {
			data[k] = v
		}
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ($1,'LogisticsBooking',$2,'AWB Assigned','system')`, id, raw); err != nil {
			t.Fatal(err)
		}
	}
}

func bookingFields47118a(t *testing.T, schema, id string) map[string]string {
	t.Helper()
	var raw []byte
	if err := db.DB.QueryRow(`SELECT data FROM `+schema+`.documents WHERE id=$1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var data map[string]interface{}
	_ = json.Unmarshal(raw, &data)
	out := map[string]string{}
	for _, k := range []string{"provider_awb", "awb_allocation_claimed_at", "awb_booking_state", "courier_reference", "awb_booking_provider", "awb_resolution"} {
		out[k] = courierDataString(data, k)
	}
	return out
}

func wantCode47118a(t *testing.T, label string, err error, code, contains string) {
	t.Helper()
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != code || !strings.Contains(verr.Message, contains) {
		t.Fatalf("%s: want %s containing %q, got %v", label, code, contains, err)
	}
	if strings.Contains(verr.Message, "127.0.0.1") {
		t.Fatalf("%s: the operator's line leaks the address: %q", label, verr.Message)
	}
}

func TestCourierTimeoutAfterSendKeepsClaimAndRetryAdoptsAWB(t *testing.T) {
	schema, newBooking := courier47118aSetup(t)
	fake, base := newFakeDelhivery(t)
	delhiveryBaseURL = base
	const id = "TEST-47118A-ADOPT"
	newBooking(id, nil)

	// The courier books it, then the answer never comes.
	fake.mode.Store(fakeBookThenHang)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_, err := AllocateCourierAWB(ctx, "default", "nd47118a", id, CourierShipmentRequest{RecipientName: "A", WeightGrams: 500})
	cancel()
	wantCode47118a(t, "timeout after send", err, CourierUnreachableCode, "may have received the request")
	f := bookingFields47118a(t, schema, id)
	if f["awb_allocation_claimed_at"] == "" || f["awb_booking_state"] != "uncertain" || f["provider_awb"] != "" || f["courier_reference"] != id || f["awb_booking_provider"] != "nd47118a" {
		t.Fatalf("timeout after send: want the claim kept as uncertain with reference %s, got %v", id, f)
	}
	if got := fake.orders; len(got) != 1 || got[0] != id {
		t.Fatalf("the courier should have been sent the booking's reference as its order id: %v", got)
	}

	// The retry asks first, finds the shipment and adopts it: no second booking.
	fake.mode.Store(fakeBookOK)
	awb, err := AllocateCourierAWB(context.Background(), "default", "nd47118a", id, CourierShipmentRequest{WeightGrams: 500})
	if err != nil || awb.AWB != "FAKE-AWB-1" {
		t.Fatalf("retry: want the AWB booked during the timeout, got %#v %v", awb, err)
	}
	if fake.creates.Load() != 1 || fake.lookups.Load() != 1 {
		t.Fatalf("retry: want 0 extra booking calls and 1 lookup, got creates=%d lookups=%d", fake.creates.Load(), fake.lookups.Load())
	}
	f = bookingFields47118a(t, schema, id)
	if f["provider_awb"] != "FAKE-AWB-1" || f["awb_allocation_claimed_at"] != "" || f["awb_booking_state"] != "" {
		t.Fatalf("adopted: %v", f)
	}
	again, err := AllocateCourierAWB(context.Background(), "default", "nd47118a", id, CourierShipmentRequest{})
	if err != nil || again.AWB != "FAKE-AWB-1" || fake.creates.Load() != 1 || fake.lookups.Load() != 1 {
		t.Fatalf("a booked booking answers its AWB with no call: %#v %v (creates=%d lookups=%d)", again, err, fake.creates.Load(), fake.lookups.Load())
	}
}

func TestCourierUncertainRetryLookupNoneBooksOnce(t *testing.T) {
	schema, newBooking := courier47118aSetup(t)
	fake, base := newFakeDelhivery(t)
	delhiveryBaseURL = base

	// A 5xx after the request was sent: uncertain, the courier booked nothing.
	const id = "TEST-47118A-NONE"
	newBooking(id, nil)
	fake.mode.Store(fakeBookFailNoTrace)
	_, err := AllocateCourierAWB(context.Background(), "default", "nd47118a", id, CourierShipmentRequest{WeightGrams: 500})
	wantCode47118a(t, "5xx", err, CourierUnreachableCode, "HTTP 503")
	if f := bookingFields47118a(t, schema, id); f["awb_booking_state"] != "uncertain" {
		t.Fatalf("5xx: want uncertain, got %v", f)
	}
	fake.mode.Store(fakeBookOK)
	awb, err := AllocateCourierAWB(context.Background(), "default", "nd47118a", id, CourierShipmentRequest{WeightGrams: 500})
	if err != nil || awb.AWB == "" || fake.creates.Load() != 2 || fake.lookups.Load() != 1 {
		t.Fatalf("lookup none: want exactly one more booking call after one lookup, got %#v %v (creates=%d lookups=%d)", awb, err, fake.creates.Load(), fake.lookups.Load())
	}

	// A claim left by a process that stopped mid-call is treated the same.
	const stale = "TEST-47118A-STALE"
	newBooking(stale, map[string]interface{}{"awb_allocation_claimed_at": time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano), "courier_reference": stale})
	awb, err = AllocateCourierAWB(context.Background(), "default", "nd47118a", stale, CourierShipmentRequest{WeightGrams: 500})
	if err != nil || awb.AWB == "" || fake.creates.Load() != 3 || fake.lookups.Load() != 2 {
		t.Fatalf("stale claim: want one lookup then one booking, got %#v %v (creates=%d lookups=%d)", awb, err, fake.creates.Load(), fake.lookups.Load())
	}
	// A fresh claim (a call in flight) is not looked up or booked again.
	const inflight = "TEST-47118A-INFLIGHT"
	newBooking(inflight, map[string]interface{}{"awb_allocation_claimed_at": time.Now().UTC().Format(time.RFC3339Nano)})
	if _, err := AllocateCourierAWB(context.Background(), "default", "nd47118a", inflight, CourierShipmentRequest{}); err == nil || !strings.Contains(err.Error(), "already in progress") || fake.creates.Load() != 3 {
		t.Fatalf("in flight: want 'already in progress' and no call, got %v (creates=%d)", err, fake.creates.Load())
	}
}

func TestCourierNoLookupRefusesRetryUntilResolved(t *testing.T) {
	schema, newBooking := courier47118aSetup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := noLookupCalls.Add(1)
		_, _ = io.ReadAll(r.Body) // read to the end, so the server notices the caller leaving
		if noLookupHang.Load() {
			<-r.Context().Done()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"awb": "NL-AWB-" + strconv.Itoa(int(n))})
	}))
	defer srv.Close()
	noLookupURL = srv.URL
	noLookupCalls.Store(0)

	const id = "TEST-47118A-NOLOOKUP"
	newBooking(id, nil)
	noLookupHang.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_, err := AllocateCourierAWB(ctx, "default", "nolookup47118a", id, CourierShipmentRequest{})
	cancel()
	wantCode47118a(t, "no lookup, timeout", err, CourierUnconfirmedCode, "Find reference "+id)
	noLookupHang.Store(false)
	_, err = AllocateCourierAWB(context.Background(), "default", "nolookup47118a", id, CourierShipmentRequest{})
	wantCode47118a(t, "no lookup, retry", err, CourierUnconfirmedCode, "may already have this shipment")
	if noLookupCalls.Load() != 1 {
		t.Fatalf("the refused retry must not call the courier: %d calls", noLookupCalls.Load())
	}

	if _, err := ResolveUnconfirmedCourierBooking("default", "nolookup47118a", id, "not_booked", "", "TEST-USER-47118A"); err != nil {
		t.Fatal(err)
	}
	if f := bookingFields47118a(t, schema, id); f["awb_allocation_claimed_at"] != "" || f["awb_booking_state"] != "" || f["awb_resolution"] != "not_booked" || f["courier_reference"] != id {
		t.Fatalf("not booked: claim released, reference kept: %v", f)
	}
	var audited int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.audit_logs WHERE action='COURIER_BOOKING_RESOLVED' AND user_id='TEST-USER-47118A' AND details LIKE '%booking=` + id + ` %'`).Scan(&audited)
	if audited != 1 {
		t.Fatalf("the resolution is audited once, found %d", audited)
	}
	_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.audit_logs WHERE user_id='TEST-USER-47118A'`)
	awb, err := AllocateCourierAWB(context.Background(), "default", "nolookup47118a", id, CourierShipmentRequest{})
	if err != nil || awb.AWB != "NL-AWB-2" || noLookupCalls.Load() != 2 {
		t.Fatalf("after not booked the retry books: %#v %v (calls %d)", awb, err, noLookupCalls.Load())
	}

	// "Booked" records the AWB from the portal with no call; one AWB cannot
	// be on two bookings.
	const second = "TEST-47118A-NOLOOKUP-2"
	newBooking(second, map[string]interface{}{"awb_allocation_claimed_at": time.Now().UTC().Format(time.RFC3339Nano), "awb_booking_state": "uncertain", "awb_booking_provider": "nolookup47118a", "courier_reference": second})
	if _, err := ResolveUnconfirmedCourierBooking("default", "nolookup47118a", second, "booked", "", "TEST-USER-47118A"); err == nil {
		t.Fatal("booked with no AWB must be refused")
	}
	var guarded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname=$1 AND indexname='idx_logistics_booking_provider_awb_unique')`, schema).Scan(&guarded)
	if guarded {
		if _, err := ResolveUnconfirmedCourierBooking("default", "nolookup47118a", second, "booked", "NL-AWB-2", "TEST-USER-47118A"); err == nil || !strings.Contains(err.Error(), "already on another booking") {
			t.Fatalf("an AWB already on another booking must be refused, got %v", err)
		}
	}
	if awb, err := ResolveUnconfirmedCourierBooking("default", "nolookup47118a", second, "booked", "PORTAL-AWB-9", "TEST-USER-47118A"); err != nil || awb != "PORTAL-AWB-9" {
		t.Fatalf("booked: %q %v", awb, err)
	}
	if f := bookingFields47118a(t, schema, second); f["provider_awb"] != "PORTAL-AWB-9" || f["awb_allocation_claimed_at"] != "" || noLookupCalls.Load() != 2 {
		t.Fatalf("booked from the portal: %v (calls %d)", f, noLookupCalls.Load())
	}
	if _, err := ResolveUnconfirmedCourierBooking("default", "nolookup47118a", second, "not_booked", "", "TEST-USER-47118A"); err == nil {
		t.Fatal("a booking with an AWB has nothing to resolve")
	}
	_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.audit_logs WHERE user_id='TEST-USER-47118A'`)
}

func TestCourierRefusedBeforeSendRetriesFreely(t *testing.T) {
	schema, newBooking := courier47118aSetup(t)
	fake, base := newFakeDelhivery(t)
	const id = "TEST-47118A-REFUSED"
	newBooking(id, nil)

	l := httptest.NewServer(http.NotFoundHandler())
	refused := l.URL
	l.Close()
	delhiveryBaseURL = refused
	_, err := AllocateCourierAWB(context.Background(), "default", "nd47118a", id, CourierShipmentRequest{})
	wantCode47118a(t, "refused", err, CourierUnreachableCode, "could not be reached - nothing was sent")
	if f := bookingFields47118a(t, schema, id); f["awb_allocation_claimed_at"] != "" || f["awb_booking_state"] != "" || f["courier_reference"] != id {
		t.Fatalf("refused before send: claim released, reference kept: %v", f)
	}
	delhiveryBaseURL = base
	awb, err := AllocateCourierAWB(context.Background(), "default", "nd47118a", id, CourierShipmentRequest{})
	if err != nil || awb.AWB == "" || fake.lookups.Load() != 0 || fake.creates.Load() != 1 {
		t.Fatalf("plain retry: want one booking call and no lookup, got %#v %v (creates=%d lookups=%d)", awb, err, fake.creates.Load(), fake.lookups.Load())
	}
}

func TestCourierConcurrentRetriesBookAtMostOnce(t *testing.T) {
	schema, newBooking := courier47118aSetup(t)
	fake, base := newFakeDelhivery(t)
	delhiveryBaseURL = base
	fake.lookupDelay = 300 * time.Millisecond
	fake.createDelay = 300 * time.Millisecond

	race := func(label, id string, wantLookups int32) {
		t.Helper()
		before := fake.creates.Load()
		lookupsBefore := fake.lookups.Load()
		var wg sync.WaitGroup
		start := make(chan struct{})
		awbs := make([]string, 2)
		errs := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				a, err := AllocateCourierAWB(context.Background(), "default", "nd47118a", id, CourierShipmentRequest{WeightGrams: 500})
				awbs[i], errs[i] = a.AWB, err
			}(i)
		}
		close(start)
		wg.Wait()
		if got := fake.creates.Load() - before; got != 1 {
			t.Fatalf("%s: want exactly one booking call, got %d (%v / %v)", label, got, errs[0], errs[1])
		}
		if got := fake.lookups.Load() - lookupsBefore; got != wantLookups {
			t.Fatalf("%s: want %d lookups, got %d", label, wantLookups, got)
		}
		ok := 0
		for i := range errs {
			if errs[i] == nil && awbs[i] != "" {
				ok++
			} else if errs[i] == nil || !strings.Contains(errs[i].Error(), "already in progress") {
				t.Fatalf("%s: the other call must be told it is in progress, got %q %v", label, awbs[i], errs[i])
			}
		}
		if ok == 0 || bookingFields47118a(t, schema, id)["provider_awb"] == "" {
			t.Fatalf("%s: one call books it: %v / %v", label, errs[0], errs[1])
		}
	}

	const uncertain = "TEST-47118A-RACE-RETRY"
	newBooking(uncertain, map[string]interface{}{"awb_allocation_claimed_at": time.Now().UTC().Format(time.RFC3339Nano), "awb_booking_state": "uncertain", "awb_booking_provider": "nd47118a", "courier_reference": uncertain})
	race("two retries of an uncertain booking", uncertain, 1)
	const fresh = "TEST-47118A-RACE-FIRST"
	newBooking(fresh, nil)
	race("two first attempts", fresh, 0)
}

func TestCourierParcelHoldsOneLiveAWB(t *testing.T) {
	schema, newBooking := courier47118aSetup(t)
	fake, base := newFakeDelhivery(t)
	delhiveryBaseURL = base
	const first, second, pkg = "TEST-47118A-PKG-1", "TEST-47118A-PKG-2", "TEST-PKG-47118A"
	newBooking(first, map[string]interface{}{"shipping_package_id": pkg})
	newBooking(second, map[string]interface{}{"shipping_package_id": pkg})
	if awb, err := AllocateCourierAWB(context.Background(), "default", "nd47118a", first, CourierShipmentRequest{}); err != nil || awb.AWB == "" {
		t.Fatalf("first booking of the parcel: %#v %v", awb, err)
	}
	if _, err := AllocateCourierAWB(context.Background(), "default", "nd47118a", second, CourierShipmentRequest{}); err == nil || !strings.Contains(err.Error(), "already has courier AWB") || fake.creates.Load() != 1 {
		t.Fatalf("a second booking of the same parcel must be refused before the courier is called, got %v (creates=%d)", err, fake.creates.Load())
	}
	if f := bookingFields47118a(t, schema, second); f["awb_allocation_claimed_at"] != "" {
		t.Fatalf("the refused booking keeps no claim: %v", f)
	}
	var guarded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname=$1 AND indexname='idx_logistics_booking_package_live_awb')`, schema).Scan(&guarded)
	if !guarded {
		t.Skip("db/migrations_stage47_11_8a_courier_booking_guard.sql has not been applied")
	}
	// The database refuses it too, whatever writes it.
	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET data = data || '{"provider_awb":"OTHER-AWB"}'::jsonb WHERE id=$1`, second); err == nil || !strings.Contains(err.Error(), "idx_logistics_booking_package_live_awb") {
		t.Fatalf("the index must refuse a second live AWB for one parcel, got %v", err)
	}
}
