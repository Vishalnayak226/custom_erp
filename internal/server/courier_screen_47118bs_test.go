package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 47.11.8b (screen): the routes OMS Ship's Pickup, Cancel and Resolve buttons
// call. A pickup or cancel the courier may already have done is answered 409
// INTEGR-4720; the booking row the list reads shows it as held (pickup_state /
// cancel_state = uncertain, no reference), and Resolve clears it so the next
// press is answered from the record with no call to the courier.
func TestStage47118bScreenRoutesShowAndResolveAHeldStep(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"POST /api/v1/marketplace/couriers/{provider}/pickup", "POST /api/v1/marketplace/couriers/{provider}/cancel", "POST /api/v1/marketplace/couriers/{provider}/resolve"} {
		if _, ok := routeCapabilities[k]; !ok {
			t.Fatalf("%s has no route_capabilities entry", k)
		}
	}
	var existing int
	_ = db.DB.QueryRow(`SELECT count(*) FROM ` + schema + `.channel_credentials WHERE channel_code = 'courier:delhivery'`).Scan(&existing)
	const pick, canc = "TEST-47118BS-PICK", "TEST-47118BS-CANC"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id IN ($1,$2)`, pick, canc)
		if existing == 0 {
			_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code = 'courier:delhivery'`)
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	if existing == 0 {
		if err := engines.SaveCourierCredential("default", "delhivery", map[string]string{"api_key": "test-only"}); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(id string, extra map[string]interface{}) {
		data := map[string]interface{}{"code": id, "order_id": "TEST-ORDER-47118BS", "status": "AWB Assigned", "carrier": "delhivery", "awb_booking_provider": "delhivery",
			"provider_awb": "AWB-" + id, "awb_number": "AWB-" + id, "courier_reference": id}
		for k, v := range extra {
			data[k] = v
		}
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ($1,'LogisticsBooking',$2,'AWB Assigned','system')`, id, raw); err != nil {
			t.Fatal(err)
		}
	}
	claimed := time.Now().UTC().Format(time.RFC3339Nano)
	seed(pick, map[string]interface{}{"pickup_claimed_at": claimed, "pickup_state": "uncertain"})
	seed(canc, map[string]interface{}{"cancel_claimed_at": claimed, "cancel_state": "uncertain"})

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/marketplace/couriers/{provider}/pickup", apiMiddleware(handleCourierPickup))
	mux.HandleFunc("POST /api/v1/marketplace/couriers/{provider}/cancel", apiMiddleware(handleCourierCancel))
	mux.HandleFunc("POST /api/v1/marketplace/couriers/{provider}/resolve", apiMiddleware(handleCourierResolveBooking))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	do := func(method, path string, body interface{}) *httptest.ResponseRecorder {
		return doRequestFromIP(t, mux.ServeHTTP, method, path, token, body, mfaTestIP())
	}
	row := func(id string) map[string]interface{} {
		rec := do("GET", "/api/v1/doc/LogisticsBooking/"+id, nil)
		var m map[string]interface{}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &m) != nil {
			t.Fatalf("read %s: %d %s", id, rec.Code, rec.Body.String())
		}
		if d, ok := m["data"].(map[string]interface{}); ok {
			return d
		}
		return m
	}
	errCode := func(rec *httptest.ResponseRecorder) string {
		var b apiErrorBody
		_ = json.Unmarshal(rec.Body.Bytes(), &b)
		return b.Code
	}

	// Held pickup: refused, shown held in the list payload.
	rec := do("POST", "/api/v1/marketplace/couriers/delhivery/pickup", map[string]string{"booking_id": pick, "pickup_at": "2030-01-01T10:00:00Z"})
	if rec.Code != http.StatusConflict || errCode(rec) != engines.CourierStepUnconfirmedCode {
		t.Fatalf("held pickup: want 409 %s, got %d %s", engines.CourierStepUnconfirmedCode, rec.Code, rec.Body.String())
	}
	if r := row(pick); r["pickup_state"] != "uncertain" || r["pickup_reference"] != nil {
		t.Fatalf("list shows %v, want a held pickup", r)
	}
	// Resolve (scheduled, with the portal's reference), then Pickup answers from the record.
	rec = do("POST", "/api/v1/marketplace/couriers/delhivery/resolve", map[string]string{"booking_id": pick, "step": "pickup", "outcome": "scheduled", "awb": "PICKUP-REF-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve pickup: %d %s", rec.Code, rec.Body.String())
	}
	if r := row(pick); r["pickup_reference"] != "PICKUP-REF-1" || r["pickup_state"] != nil {
		t.Fatalf("after resolve the list shows %v", r)
	}
	rec = do("POST", "/api/v1/marketplace/couriers/delhivery/pickup", map[string]string{"booking_id": pick, "pickup_at": "2030-01-01T10:00:00Z"})
	if rec.Code != http.StatusOK {
		t.Fatalf("pickup after resolve: %d %s", rec.Code, rec.Body.String())
	}

	// Held cancel: refused, resolved as not cancelled, no longer held.
	rec = do("POST", "/api/v1/marketplace/couriers/delhivery/cancel", map[string]string{"booking_id": canc})
	if rec.Code != http.StatusConflict || errCode(rec) != engines.CourierStepUnconfirmedCode {
		t.Fatalf("held cancel: want 409 %s, got %d %s", engines.CourierStepUnconfirmedCode, rec.Code, rec.Body.String())
	}
	if r := row(canc); r["cancel_state"] != "uncertain" || r["courier_cancelled_at"] != nil {
		t.Fatalf("list shows %v, want a held cancel", r)
	}
	rec = do("POST", "/api/v1/marketplace/couriers/delhivery/resolve", map[string]string{"booking_id": canc, "step": "cancel", "outcome": "not_cancelled"})
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve cancel: %d %s", rec.Code, rec.Body.String())
	}
	if r := row(canc); r["cancel_state"] != nil {
		t.Fatalf("after resolve the list shows %v", r)
	}
}
