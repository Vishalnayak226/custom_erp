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

// 47.11.8b: pickup scheduling and cancel keep nothing outstanding twice at
// the courier, and OMS → Ship "Book" refuses a second local booking for a
// task. The couriers here answer over loopback HTTP, so the real outbound
// path (request written or not) decides whether a claim is kept.

// fakeStepCourier counts pickup and cancel calls and can hang after reading
// a request, the timeout-after-send case.
type fakeStepCourier struct {
	mu        sync.Mutex
	pickups   atomic.Int32
	cancels   atomic.Int32
	hang      atomic.Bool
	scheduled map[string]string // AWB -> pickup reference
	cancelled map[string]bool
	url       string
}

func newFakeStepCourier(t *testing.T) *fakeStepCourier {
	f := &fakeStepCourier{scheduled: map[string]string{}, cancelled: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			AWB string `json:"awb"`
		}
		_ = json.Unmarshal(body, &req)
		switch {
		case strings.HasSuffix(r.URL.Path, "/pickup"):
			n := f.pickups.Add(1)
			ref := "PR-" + strconv.Itoa(int(n))
			f.mu.Lock()
			f.scheduled[req.AWB] = ref
			f.mu.Unlock()
			if f.hang.Load() {
				<-r.Context().Done()
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"reference": ref})
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			f.cancels.Add(1)
			f.mu.Lock()
			f.cancelled[req.AWB] = true
			f.mu.Unlock()
			if f.hang.Load() {
				<-r.Context().Done()
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

// step47118bCourier cannot ask the courier about a pickup or a cancel;
// stepLookup47118bCourier can.
type step47118bCourier struct{ f *fakeStepCourier }
type stepLookup47118bCourier struct{ step47118bCourier }

func (step47118bCourier) Code() string       { return "step47118b" }
func (stepLookup47118bCourier) Code() string { return "steplk47118b" }
func (c step47118bCourier) call(ctx context.Context, path, awb string) ([]byte, error) {
	status, body, err := doConnectorRequest(ctx, 3*time.Second, http.MethodPost, c.f.url+path, map[string]string{"Content-Type": "application/json"}, []byte(`{"awb":"`+awb+`"}`), "courier:"+c.Code())
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &ConnectorHTTPError{Platform: c.Code(), Status: status}
	}
	return body, nil
}
func (step47118bCourier) AllocateAWB(context.Context, map[string]string, CourierShipmentRequest) (CourierAWB, error) {
	return CourierAWB{}, errors.New("not used")
}
func (c step47118bCourier) SchedulePickup(ctx context.Context, _ map[string]string, req CourierPickupRequest) (string, error) {
	body, err := c.call(ctx, "/pickup", req.AWBs[0])
	if err != nil {
		return "", err
	}
	var out struct {
		Reference string `json:"reference"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	return out.Reference, nil
}
func (c step47118bCourier) CancelShipment(ctx context.Context, _ map[string]string, awb string) error {
	_, err := c.call(ctx, "/cancel", awb)
	return err
}
func (step47118bCourier) Rates(context.Context, map[string]string, CourierRateRequest) ([]CourierRate, error) {
	return nil, nil
}
func (step47118bCourier) ParseTrackingWebhook([]byte) (CourierTrackingEvent, error) {
	return CourierTrackingEvent{}, errors.New("not used")
}
func (c stepLookup47118bCourier) FindPickup(_ context.Context, _ map[string]string, req CourierPickupRequest) (string, bool, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	ref, ok := c.f.scheduled[req.AWBs[0]]
	return ref, ok, nil
}
func (c stepLookup47118bCourier) ShipmentCancelled(_ context.Context, _ map[string]string, awb string) (bool, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	return c.f.cancelled[awb], nil
}

func courier47118bSetup(t *testing.T) (string, *fakeStepCourier, func(id string, extra map[string]interface{})) {
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
	f := newFakeStepCourier(t)
	registerCourierAdapter(step47118bCourier{f: f})
	registerCourierAdapter(stepLookup47118bCourier{step47118bCourier{f: f}})
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE doctype='LogisticsBooking' AND (id LIKE 'TEST-47118B-%' OR data->>'fulfillment_task_id' LIKE 'TEST-47118B-%')`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code IN ('courier:step47118b','courier:steplk47118b')`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.audit_logs WHERE user_id='TEST-USER-47118B'`)
		resetBreaker("courier:step47118b", "courier:steplk47118b")
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, p := range []string{"step47118b", "steplk47118b"} {
		if err := SaveCourierCredential("default", p, map[string]string{"api_token": "test-only"}); err != nil {
			t.Fatal(err)
		}
	}
	return schema, f, func(id string, extra map[string]interface{}) {
		t.Helper()
		data := map[string]interface{}{"code": id, "order_id": "TEST-ORDER-47118B", "destination_pincode": "560001", "status": "AWB Assigned", "provider_awb": "AWB-" + id, "awb_number": "AWB-" + id}
		for k, v := range extra {
			data[k] = v
		}
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ($1,'LogisticsBooking',$2,'AWB Assigned','system')`, id, raw); err != nil {
			t.Fatal(err)
		}
	}
}

func stepFields47118b(t *testing.T, schema, id string) map[string]string {
	t.Helper()
	var raw []byte
	if err := db.DB.QueryRow(`SELECT data FROM `+schema+`.documents WHERE id=$1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var data map[string]interface{}
	_ = json.Unmarshal(raw, &data)
	out := map[string]string{}
	for _, k := range []string{"pickup_claimed_at", "pickup_state", "pickup_reference", "pickup_resolution", "cancel_claimed_at", "cancel_state", "courier_cancelled_at", "cancel_resolution"} {
		out[k] = courierDataString(data, k)
	}
	return out
}

func TestStage47118bPickupTimeoutKeepsClaimAndWaitsForResolve(t *testing.T) {
	schema, f, newBooking := courier47118bSetup(t)
	const id = "TEST-47118B-PICKUP"
	newBooking(id, nil)
	when := time.Now().Add(24 * time.Hour)

	f.hang.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_, err := ScheduleCourierPickup(ctx, "default", "step47118b", id, "WH", when)
	cancel()
	wantCode47118a(t, "pickup, timeout after send", err, CourierStepUnconfirmedCode, "may have scheduled this pickup")
	if fl := stepFields47118b(t, schema, id); fl["pickup_claimed_at"] == "" || fl["pickup_state"] != "uncertain" || fl["pickup_reference"] != "" {
		t.Fatalf("the claim is kept as uncertain: %v", fl)
	}
	f.hang.Store(false)
	_, err = ScheduleCourierPickup(context.Background(), "default", "step47118b", id, "WH", when)
	wantCode47118a(t, "pickup, retry", err, CourierStepUnconfirmedCode, "resolve it here as scheduled or not_scheduled")
	if f.pickups.Load() != 1 {
		t.Fatalf("the refused retry must not call the courier: %d calls", f.pickups.Load())
	}
	if err := ResolveUnconfirmedCourierStep("default", "step47118b", id, "pickup", "booked", "", "TEST-USER-47118B"); err == nil {
		t.Fatal("a booking outcome is not a pickup outcome")
	}
	if err := ResolveUnconfirmedCourierStep("default", "step47118b", id, "pickup", "not_scheduled", "", "TEST-USER-47118B"); err != nil {
		t.Fatal(err)
	}
	if fl := stepFields47118b(t, schema, id); fl["pickup_claimed_at"] != "" || fl["pickup_state"] != "" || fl["pickup_resolution"] != "not_scheduled" {
		t.Fatalf("not scheduled: claim released: %v", fl)
	}
	ref, err := ScheduleCourierPickup(context.Background(), "default", "step47118b", id, "WH", when)
	if err != nil || ref != "PR-2" || f.pickups.Load() != 2 {
		t.Fatalf("after not scheduled the retry schedules: %q %v (calls %d)", ref, err, f.pickups.Load())
	}
	// Scheduled once: a repeat answers from the record.
	ref, err = ScheduleCourierPickup(context.Background(), "default", "step47118b", id, "WH", when)
	if err != nil || ref != "PR-2" || f.pickups.Load() != 2 {
		t.Fatalf("a scheduled pickup is not scheduled again: %q %v (calls %d)", ref, err, f.pickups.Load())
	}
	if fl := stepFields47118b(t, schema, id); fl["pickup_claimed_at"] != "" || fl["pickup_reference"] != "PR-2" {
		t.Fatalf("recorded: %v", fl)
	}
	var audited int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.audit_logs WHERE action='COURIER_PICKUP_RESOLVED' AND user_id='TEST-USER-47118B' AND details LIKE '%booking=` + id + ` %'`).Scan(&audited)
	if audited != 1 {
		t.Fatalf("the resolution is audited once, found %d", audited)
	}

	// "scheduled" from the portal records the reference with no call.
	const second = "TEST-47118B-PICKUP-2"
	newBooking(second, map[string]interface{}{"pickup_claimed_at": time.Now().UTC().Format(time.RFC3339Nano), "pickup_state": "uncertain"})
	if err := ResolveUnconfirmedCourierStep("default", "step47118b", second, "pickup", "scheduled", "PORTAL-7", "TEST-USER-47118B"); err != nil {
		t.Fatal(err)
	}
	ref, err = ScheduleCourierPickup(context.Background(), "default", "step47118b", second, "WH", when)
	if err != nil || ref != "PORTAL-7" || f.pickups.Load() != 2 {
		t.Fatalf("scheduled from the portal: %q %v (calls %d)", ref, err, f.pickups.Load())
	}
	if err := ResolveUnconfirmedCourierStep("default", "step47118b", second, "pickup", "not_scheduled", "", "TEST-USER-47118B"); err == nil {
		t.Fatal("a scheduled pickup has nothing to resolve")
	}
}

func TestStage47118bPickupRetryAsksCourierFirst(t *testing.T) {
	schema, f, newBooking := courier47118bSetup(t)
	const id = "TEST-47118B-PICKUP-LK"
	newBooking(id, nil)
	when := time.Now().Add(24 * time.Hour)

	f.hang.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_, err := ScheduleCourierPickup(ctx, "default", "steplk47118b", id, "WH", when)
	cancel()
	wantCode47118a(t, "pickup with lookup, timeout", err, CourierUnreachableCode, "the retry asks steplk47118b first")
	f.hang.Store(false)
	ref, err := ScheduleCourierPickup(context.Background(), "default", "steplk47118b", id, "WH", when)
	if err != nil || ref != "PR-1" || f.pickups.Load() != 1 {
		t.Fatalf("the retry adopts the pickup the courier has: %q %v (calls %d)", ref, err, f.pickups.Load())
	}
	if fl := stepFields47118b(t, schema, id); fl["pickup_claimed_at"] != "" || fl["pickup_state"] != "" || fl["pickup_reference"] != "PR-1" {
		t.Fatalf("adopted and claim dropped: %v", fl)
	}
}

func TestStage47118bCancelTimeoutKeepsClaimAndRefusedBeforeSendRetries(t *testing.T) {
	schema, f, newBooking := courier47118bSetup(t)
	const id = "TEST-47118B-CANCEL"
	newBooking(id, nil)

	f.hang.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := CancelCourierShipment(ctx, "default", "step47118b", id)
	cancel()
	wantCode47118a(t, "cancel, timeout after send", err, CourierStepUnconfirmedCode, "may have cancelled this shipment")
	f.hang.Store(false)
	err = CancelCourierShipment(context.Background(), "default", "step47118b", id)
	wantCode47118a(t, "cancel, retry", err, CourierStepUnconfirmedCode, "resolve it here as cancelled or not_cancelled")
	if f.cancels.Load() != 1 {
		t.Fatalf("the refused retry must not call the courier: %d calls", f.cancels.Load())
	}
	if err := ResolveUnconfirmedCourierStep("default", "step47118b", id, "cancel", "cancelled", "", "TEST-USER-47118B"); err != nil {
		t.Fatal(err)
	}
	fl := stepFields47118b(t, schema, id)
	if fl["courier_cancelled_at"] == "" || fl["cancel_claimed_at"] != "" || fl["cancel_resolution"] != "cancelled" {
		t.Fatalf("cancelled from the portal: %v", fl)
	}
	if err := CancelCourierShipment(context.Background(), "default", "step47118b", id); err != nil || f.cancels.Load() != 1 {
		t.Fatalf("a cancelled shipment is not cancelled again: %v (calls %d)", err, f.cancels.Load())
	}

	// With a lookup the retry asks first and adopts the cancel the courier has.
	const lk = "TEST-47118B-CANCEL-LK"
	newBooking(lk, nil)
	f.hang.Store(true)
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	err = CancelCourierShipment(ctx, "default", "steplk47118b", lk)
	cancel()
	wantCode47118a(t, "cancel with lookup, timeout", err, CourierUnreachableCode, "the retry asks steplk47118b first")
	f.hang.Store(false)
	if err := CancelCourierShipment(context.Background(), "default", "steplk47118b", lk); err != nil || f.cancels.Load() != 2 {
		t.Fatalf("the retry adopts the courier's cancel: %v (calls %d)", err, f.cancels.Load())
	}
	if fl := stepFields47118b(t, schema, lk); fl["courier_cancelled_at"] == "" || fl["cancel_claimed_at"] != "" {
		t.Fatalf("adopted: %v", fl)
	}

	// Refused before anything was sent: the claim is released and a plain
	// retry cancels.
	const down = "TEST-47118B-CANCEL-DOWN"
	newBooking(down, nil)
	live := f.url
	f.url = "http://127.0.0.1:9" // nothing listens
	err = CancelCourierShipment(context.Background(), "default", "step47118b", down)
	f.url = live
	wantCode47118a(t, "cancel, refused before send", err, CourierUnreachableCode, "nothing was sent")
	if fl := stepFields47118b(t, schema, down); fl["cancel_claimed_at"] != "" || fl["cancel_state"] != "" {
		t.Fatalf("nothing outstanding: claim released: %v", fl)
	}
	resetBreaker("courier:step47118b")
	if err := CancelCourierShipment(context.Background(), "default", "step47118b", down); err != nil || f.cancels.Load() != 3 {
		t.Fatalf("the retry cancels: %v (calls %d)", err, f.cancels.Load())
	}
}

func TestStage47118bTaskHoldsOneLiveBooking(t *testing.T) {
	schema, _, _ := courier47118bSetup(t)
	const task = "TEST-47118B-TASK-1"
	first, err := CreateLogisticsBooking("default", "TEST-ORDER-47118B", task, "TestCourier47118B", "", "560001", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateLogisticsBooking("default", "TEST-ORDER-47118B", task, "TestCourier47118B", "", "560001", 0)
	wantCode47118a(t, "second booking for a booked task", err, CourierTaskBookedCode, "already booked as "+first)

	// Cancelled at the courier: the task can be booked again; a Cancelled
	// booking likewise.
	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET data = data || '{"courier_cancelled_at":"2026-10-29T00:00:00Z"}' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	second, err := CreateLogisticsBooking("default", "TEST-ORDER-47118B", task, "TestCourier47118B", "", "560001", 0)
	if err != nil {
		t.Fatalf("after a courier cancel the task books again: %v", err)
	}
	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET status='Cancelled' WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateLogisticsBooking("default", "TEST-ORDER-47118B", task, "TestCourier47118B", "", "560001", 0); err != nil {
		t.Fatalf("after Cancelled the task books again: %v", err)
	}
	// A booking with no task is not held to another's.
	if _, err := CreateLogisticsBooking("default", "TEST-ORDER-47118B", "", "TestCourier47118B", "", "560001", 0); err != nil {
		t.Fatal(err)
	}
	_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype='LogisticsBooking' AND data->>'order_id'=$1`, "TEST-ORDER-47118B")

	// Two "Book" clicks at once: exactly one booking.
	const race = "TEST-47118B-TASK-RACE"
	var wg sync.WaitGroup
	var booked, refused atomic.Int32
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := CreateLogisticsBooking("default", "TEST-ORDER-47118B", race, "TestCourier47118B", "", "560001", 0)
			var verr *ValidationError
			switch {
			case err == nil:
				booked.Add(1)
			case errors.As(err, &verr) && verr.Code == CourierTaskBookedCode:
				refused.Add(1)
			default:
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	if booked.Load() != 1 || refused.Load() != 3 {
		t.Fatalf("four at once: booked %d refused %d", booked.Load(), refused.Load())
	}
	_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype='LogisticsBooking' AND data->>'order_id'=$1`, "TEST-ORDER-47118B")
}
