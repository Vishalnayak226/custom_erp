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

// Stage 49.8.8 (second slice) - five failure modes of an integration, each
// against a loopback fake courier (or the print log), asserting where the
// record ends up:
//
//   - provider compromise: an answer that fails validation is refused, logged
//     and never recorded as done; a bad webhook signature changes nothing
//   - credential revoked while a command is in flight
//   - DNS failure: nothing sent, claim released, the next try is allowed
//   - printer unavailable: an inactive printer is not offered, a failed job
//     is in the print log with its reason
//   - provider success, local commit failed: the booking ends held as
//     uncertain (not "being sent"), so a retry asks the courier first or
//     Resolve finishes it, and the courier is never called twice

const fm4988Provider = "fm4988"

type fm4988Fake struct {
	mu       sync.Mutex
	pickups  atomic.Int32
	cancels  atomic.Int32
	shape    atomic.Bool // answer 200 with no reference
	onCall   func()      // runs inside the request, before the answer
	schedule map[string]string
	url      string
}

var (
	fm4988Cur   atomic.Pointer[fm4988Fake]
	fm4988URLNo atomic.Pointer[string] // overrides the URL (DNS test)
)

type fm4988Courier struct{}
type fm4988LookupCourier struct{ fm4988Courier }

func (fm4988Courier) Code() string       { return fm4988Provider }
func (fm4988LookupCourier) Code() string { return fm4988Provider + "lk" }
func (fm4988Courier) base() string {
	if u := fm4988URLNo.Load(); u != nil {
		return *u
	}
	return fm4988Cur.Load().url
}
func (c fm4988Courier) call(ctx context.Context, path, awb string) ([]byte, error) {
	status, body, err := doConnectorRequest(ctx, 3*time.Second, http.MethodPost, c.base()+path, map[string]string{"Content-Type": "application/json"}, []byte(`{"awb":"`+awb+`"}`), "courier:"+fm4988Provider)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &ConnectorHTTPError{Platform: fm4988Provider, Status: status}
	}
	return body, nil
}
func (fm4988Courier) AllocateAWB(context.Context, map[string]string, CourierShipmentRequest) (CourierAWB, error) {
	return CourierAWB{}, errors.New("not used")
}
func (c fm4988Courier) SchedulePickup(ctx context.Context, _ map[string]string, req CourierPickupRequest) (string, error) {
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
	return out.Reference, nil // an answer without one is left to the caller to refuse
}
func (c fm4988Courier) CancelShipment(ctx context.Context, _ map[string]string, awb string) error {
	_, err := c.call(ctx, "/cancel", awb)
	return err
}
func (fm4988Courier) Rates(context.Context, map[string]string, CourierRateRequest) ([]CourierRate, error) {
	return nil, nil
}
func (fm4988Courier) ParseTrackingWebhook([]byte) (CourierTrackingEvent, error) {
	return CourierTrackingEvent{}, errors.New("not used")
}
func (c fm4988LookupCourier) FindPickup(_ context.Context, _ map[string]string, req CourierPickupRequest) (string, bool, error) {
	f := fm4988Cur.Load()
	f.mu.Lock()
	defer f.mu.Unlock()
	ref, ok := f.schedule[req.AWBs[0]]
	return ref, ok, nil
}

func fm4988Setup(t *testing.T) (schema string, f *fm4988Fake, newBooking func(id string)) {
	t.Helper()
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.doctype_meta WHERE name='LogisticsBooking')`).Scan(&ready); err != nil || !ready {
		t.Skip("LogisticsBooking is not migrated")
	}
	f = &fm4988Fake{schedule: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			AWB string `json:"awb"`
		}
		_ = json.Unmarshal(body, &req)
		if f.onCall != nil {
			f.onCall()
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/pickup"):
			n := f.pickups.Add(1)
			ref := "PR-" + strconv.Itoa(int(n))
			f.mu.Lock()
			f.schedule[req.AWB] = ref
			f.mu.Unlock()
			if f.shape.Load() {
				_ = json.NewEncoder(w).Encode(map[string]bool{"unexpected": true})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"reference": ref})
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			f.cancels.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	fm4988Cur.Store(f)
	registerCourierAdapter(fm4988Courier{})
	registerCourierAdapter(fm4988LookupCourier{})
	cleanup := func() {
		fm4988URLNo.Store(nil)
		_, _ = db.DB.Exec(`DROP TRIGGER IF EXISTS fm4988_block ON ` + schema + `.documents`)
		_, _ = db.DB.Exec(`DROP FUNCTION IF EXISTS ` + schema + `.fm4988_block()`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE doctype IN ('LogisticsBooking','Printer') AND id LIKE 'TEST-FM4988-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code IN ('courier:fm4988','courier:fm4988lk')`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.print_job_log WHERE document_ref LIKE 'TEST-FM4988-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.audit_logs WHERE details LIKE '%TEST-FM4988-%'`)
		resetBreaker("courier:fm4988", "courier:fm4988lk")
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, p := range []string{fm4988Provider, fm4988Provider + "lk"} {
		if err := SaveCourierCredential("default", p, map[string]string{"api_token": "test-only", "webhook_secret": "wh-secret"}); err != nil {
			t.Fatal(err)
		}
	}
	return schema, f, func(id string) {
		t.Helper()
		data := map[string]interface{}{"code": id, "order_id": "TEST-ORDER-FM4988", "destination_pincode": "560001", "status": "AWB Assigned", "provider_awb": "AWB-" + id, "awb_number": "AWB-" + id}
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ($1,'LogisticsBooking',$2,'AWB Assigned','system')`, id, raw); err != nil {
			t.Fatal(err)
		}
	}
}

func fm4988Fields(t *testing.T, schema, id string) map[string]string {
	t.Helper()
	var raw []byte
	if err := db.DB.QueryRow(`SELECT data FROM `+schema+`.documents WHERE id=$1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var data map[string]interface{}
	_ = json.Unmarshal(raw, &data)
	out := map[string]string{}
	for _, k := range []string{"pickup_claimed_at", "pickup_state", "pickup_reference", "cancel_claimed_at", "cancel_state", "courier_cancelled_at"} {
		out[k] = courierDataString(data, k)
	}
	return out
}

func fm4988Audited(schema, action, contains string) bool {
	var n int
	_ = db.DB.QueryRow(`SELECT count(*) FROM `+schema+`.audit_logs WHERE action=$1 AND details LIKE $2`, action, "%"+contains+"%").Scan(&n)
	return n > 0
}

// fm4988BlockCommit makes the write of a step's result on the test bookings
// fail, as a full disk or a dropped connection would after the courier has
// already answered. The uncertain marker (no result key) still goes through.
func fm4988BlockCommit(t *testing.T, schema string) {
	t.Helper()
	for _, q := range []string{
		`CREATE OR REPLACE FUNCTION ` + schema + `.fm4988_block() RETURNS trigger AS $$ BEGIN
			IF NEW.id LIKE 'TEST-FM4988-%' AND (NEW.data ? 'pickup_reference' OR NEW.data ? 'courier_cancelled_at') AND NOT (OLD.data ? 'pickup_reference' OR OLD.data ? 'courier_cancelled_at')
			THEN RAISE EXCEPTION 'fm4988: commit refused'; END IF; RETURN NEW; END; $$ LANGUAGE plpgsql`,
		`CREATE TRIGGER fm4988_block BEFORE UPDATE ON ` + schema + `.documents FOR EACH ROW EXECUTE FUNCTION ` + schema + `.fm4988_block()`,
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// Provider compromise: a 200 that does not carry what was asked for is not a
// pickup. It is refused, logged, the booking is held (the courier may have
// acted), and nothing is recorded as scheduled.
func TestStage4988ProviderAnswerThatFailsValidationIsRefusedAndLogged(t *testing.T) {
	schema, f, newBooking := fm4988Setup(t)
	const id = "TEST-FM4988-SHAPE"
	newBooking(id)
	f.shape.Store(true)
	_, err := ScheduleCourierPickup(context.Background(), "default", fm4988Provider, id, "WH", time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("an answer with no pickup reference was accepted as a scheduled pickup")
	}
	if fl := fm4988Fields(t, schema, id); fl["pickup_reference"] != "" || fl["pickup_state"] != "uncertain" {
		t.Fatalf("an unusable answer must leave the pickup held, got %v", fl)
	}
	if !fm4988Audited(schema, "COURIER_PICKUP_UNCERTAIN", id) {
		t.Fatal("the refused answer is not in the audit log")
	}
	// The next press does not send a second pickup: it waits for Resolve.
	_, err = ScheduleCourierPickup(context.Background(), "default", fm4988Provider, id, "WH", time.Now().Add(time.Hour))
	wantCode47118a(t, "retry", err, CourierStepUnconfirmedCode, "Check booking")
	if f.pickups.Load() != 1 {
		t.Fatalf("the courier was called %d times, want 1", f.pickups.Load())
	}

	// A webhook signed with the wrong secret, or not signed, is refused.
	body := []byte(`{"awb":"x"}`)
	for _, sig := range []string{"", "zz", strings.Repeat("0", 64)} {
		if err := VerifyCourierWebhook("default", fm4988Provider, sig, body); err == nil {
			t.Fatalf("signature %q was accepted", sig)
		}
	}
}

// Revocation mid-command: the credential goes while the courier is working
// on the pickup. The courier did it, so the record says so; the next command
// is refused locally and never reaches the courier.
func TestStage4988CredentialRevokedMidCommand(t *testing.T) {
	schema, f, newBooking := fm4988Setup(t)
	const id = "TEST-FM4988-REVOKE"
	newBooking(id)
	f.onCall = func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code = 'courier:fm4988'`)
	}
	ref, err := ScheduleCourierPickup(context.Background(), "default", fm4988Provider, id, "WH", time.Now().Add(time.Hour))
	if err != nil || ref == "" {
		t.Fatalf("the pickup the courier made must be recorded, got %q %v", ref, err)
	}
	if fl := fm4988Fields(t, schema, id); fl["pickup_reference"] != ref || fl["pickup_claimed_at"] != "" {
		t.Fatalf("record after revocation: %v", fl)
	}
	f.onCall = nil
	if err := CancelCourierShipment(context.Background(), "default", fm4988Provider, id); err == nil || f.cancels.Load() != 0 {
		t.Fatalf("a cancel after revocation must be refused before any call (calls=%d, err=%v)", f.cancels.Load(), err)
	}
	if fl := fm4988Fields(t, schema, id); fl["cancel_claimed_at"] != "" || fl["cancel_state"] != "" {
		t.Fatalf("a refused cancel must leave no claim: %v", fl)
	}
}

// DNS failure: the name does not resolve, so nothing was sent. The operator
// gets the bounded unreachable answer, the claim is released and the same
// press works as soon as the name resolves.
func TestStage4988DNSFailureSendsNothingAndAllowsTheNextTry(t *testing.T) {
	schema, f, newBooking := fm4988Setup(t)
	const id = "TEST-FM4988-DNS"
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1") // so the guard lets a non-loopback name through to the resolver
	newBooking(id)
	bad := "http://courier-does-not-exist.invalid"
	fm4988URLNo.Store(&bad)
	_, err := ScheduleCourierPickup(context.Background(), "default", fm4988Provider, id, "WH", time.Now().Add(time.Hour))
	wantCode47118a(t, "dns", err, CourierUnreachableCode, "nothing was sent")
	if fl := fm4988Fields(t, schema, id); fl["pickup_claimed_at"] != "" || fl["pickup_state"] != "" {
		t.Fatalf("nothing was sent, so no claim may stay: %v", fl)
	}
	fm4988URLNo.Store(nil)
	resetBreaker("courier:fm4988")
	if ref, err := ScheduleCourierPickup(context.Background(), "default", fm4988Provider, id, "WH", time.Now().Add(time.Hour)); err != nil || ref == "" || f.pickups.Load() != 1 {
		t.Fatalf("the next try must go through once: %q %v calls=%d", ref, err, f.pickups.Load())
	}
}

// Printer unavailable: the browser prints, the server keeps the list and the
// log. An inactive printer is not offered; a job that failed is in the log
// with its reason.
func TestStage4988PrinterUnavailableIsNotOfferedAndAFailureIsLogged(t *testing.T) {
	schema, _, _ := fm4988Setup(t)
	raw, _ := json.Marshal(map[string]interface{}{"code": "TEST-FM4988-PRN", "name": "Down", "qz_printer_name": "Zebra", "print_role": "Shipping Label", "status": "Inactive"})
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ('TEST-FM4988-PRN','Printer',$1,'Inactive','system')`, raw); err != nil {
		t.Fatal(err)
	}
	printers, err := ListQZPrinters("default")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range printers {
		if p.Code == "TEST-FM4988-PRN" {
			t.Fatal("an inactive printer is offered for printing")
		}
	}
	if err := LogPrintJob("default", "Shipping Label", "TEST-FM4988-LBL", "TEST-FM4988-PRN", "Zebra", "ZPL", 1, "tester", "Failed", "printer offline"); err != nil {
		t.Fatal(err)
	}
	log, err := GetPrintJobLog("default", 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range log {
		if e.DocumentRef == "TEST-FM4988-LBL" && e.Status == "Failed" && e.ErrorDetail == "printer offline" {
			found = true
		}
	}
	if !found {
		t.Fatal("the failed job is not in the print log with its reason")
	}
}

// Provider success, local commit failed: the courier scheduled the pickup but
// the result could not be saved. The booking must end held as uncertain, not
// "being sent", so a retry or Resolve finishes it and the courier is never
// asked twice.
func TestStage4988ProviderSuccessLocalCommitFailedPickup(t *testing.T) {
	schema, f, newBooking := fm4988Setup(t)
	const id = "TEST-FM4988-COMMIT"
	newBooking(id)
	fm4988BlockCommit(t, schema)
	_, err := ScheduleCourierPickup(context.Background(), "default", fm4988Provider, id, "WH", time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("a pickup that could not be saved was reported as saved")
	}
	if f.pickups.Load() != 1 {
		t.Fatalf("courier called %d times", f.pickups.Load())
	}
	if fl := fm4988Fields(t, schema, id); fl["pickup_state"] != "uncertain" || fl["pickup_reference"] != "" {
		t.Fatalf("the booking must be held as uncertain, got %v", fl)
	}
	if !fm4988Audited(schema, "COURIER_PICKUP_NOT_RECORDED", id) {
		t.Fatal("the unsaved pickup is not in the audit log")
	}
	// Retry: refused for Resolve (this courier cannot be asked), no second call.
	_, err = ScheduleCourierPickup(context.Background(), "default", fm4988Provider, id, "WH", time.Now().Add(time.Hour))
	wantCode47118a(t, "retry", err, CourierStepUnconfirmedCode, "Check booking")
	if f.pickups.Load() != 1 {
		t.Fatalf("retry called the courier again (%d)", f.pickups.Load())
	}
	// Resolve finishes it once the store works again.
	_, _ = db.DB.Exec(`DROP TRIGGER fm4988_block ON ` + schema + `.documents`)
	if err := ResolveUnconfirmedCourierStep("default", fm4988Provider, id, "pickup", "scheduled", "PR-1", "tester"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ref, err := ScheduleCourierPickup(context.Background(), "default", fm4988Provider, id, "WH", time.Now().Add(time.Hour)); err != nil || ref != "PR-1" || f.pickups.Load() != 1 {
		t.Fatalf("after resolve the answer comes from the record: %q %v calls=%d", ref, err, f.pickups.Load())
	}
}

// The same failure for a courier that can be asked: the retry finds the
// pickup it already has and records it, with no second call.
func TestStage4988ProviderSuccessLocalCommitFailedRetryAdopts(t *testing.T) {
	schema, f, newBooking := fm4988Setup(t)
	const id = "TEST-FM4988-ADOPT"
	newBooking(id)
	fm4988BlockCommit(t, schema)
	if _, err := ScheduleCourierPickup(context.Background(), "default", fm4988Provider+"lk", id, "WH", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("a pickup that could not be saved was reported as saved")
	}
	_, _ = db.DB.Exec(`DROP TRIGGER fm4988_block ON ` + schema + `.documents`)
	ref, err := ScheduleCourierPickup(context.Background(), "default", fm4988Provider+"lk", id, "WH", time.Now().Add(time.Hour))
	if err != nil || ref != "PR-1" || f.pickups.Load() != 1 {
		t.Fatalf("retry must adopt the existing pickup: %q %v calls=%d", ref, err, f.pickups.Load())
	}
}

// And for a cancel: the courier cancelled, the save failed; no second cancel.
func TestStage4988ProviderSuccessLocalCommitFailedCancel(t *testing.T) {
	schema, f, newBooking := fm4988Setup(t)
	const id = "TEST-FM4988-CANCEL"
	newBooking(id)
	fm4988BlockCommit(t, schema)
	if err := CancelCourierShipment(context.Background(), "default", fm4988Provider, id); err == nil {
		t.Fatal("a cancel that could not be saved was reported as saved")
	}
	if fl := fm4988Fields(t, schema, id); fl["cancel_state"] != "uncertain" || fl["courier_cancelled_at"] != "" {
		t.Fatalf("the booking must be held as uncertain, got %v", fl)
	}
	err := CancelCourierShipment(context.Background(), "default", fm4988Provider, id)
	wantCode47118a(t, "retry", err, CourierStepUnconfirmedCode, "Check booking")
	if f.cancels.Load() != 1 {
		t.Fatalf("courier cancelled %d times", f.cancels.Load())
	}
}
