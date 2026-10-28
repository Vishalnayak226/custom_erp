package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 47.11.8a: a booking the courier may already have is answered with its own
// code, and resolving it needs update on LogisticsBooking on top of the
// courier routes' own gates.
func TestCourierResolveBooking47118a(t *testing.T) {
	db.InitDB(testConnStr())
	if _, ok := routeCapabilities["POST /api/v1/marketplace/couriers/{provider}/resolve"]; !ok {
		t.Fatal("the resolve route has no route_capabilities entry")
	}

	line := "delhivery did not answer - it may have booked this shipment. Find reference LOG-1 on the delhivery portal, then enter its AWB here or mark it not booked."
	rec := httptest.NewRecorder()
	writeCourierError(rec, httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/couriers/delhivery/awb", nil), &engines.ValidationError{Code: engines.CourierUnconfirmedCode, Message: line})
	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not an error envelope: %s", rec.Body.String())
	}
	if rec.Code != http.StatusConflict || body.Code != engines.CourierUnconfirmedCode || body.Retryable || body.Detail != line || body.UserAction == "" {
		t.Fatalf("unconfirmed: want 409 %s with the engine's line, got %d %+v", engines.CourierUnconfirmedCode, rec.Code, body)
	}

	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const id, role, user = "TEST-47118A-ROUTE", "TEST-ROLE-47118A", "TEST-USER-47118A"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id=$1`, id)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.role_permissions WHERE role=$1`, role)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.audit_logs WHERE user_id=$1`, user)
	}
	cleanup()
	t.Cleanup(cleanup)
	data, _ := json.Marshal(map[string]interface{}{"code": id, "order_id": "TEST-ORDER-47118A", "status": "AWB Assigned", "courier_reference": id,
		"awb_allocation_claimed_at": time.Now().UTC().Format(time.RFC3339Nano), "awb_booking_state": "uncertain", "awb_booking_provider": "delhivery"})
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ($1,'LogisticsBooking',$2,'AWB Assigned','system')`, id, data); err != nil {
		t.Fatal(err)
	}
	call := func(role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/couriers/delhivery/resolve", strings.NewReader(`{"booking_id":"`+id+`","outcome":"not_booked"}`))
		req.SetPathValue("provider", "delhivery")
		req.Header.Set("Resolved-Tenant-ID", "default")
		req.Header.Set("Resolved-Role", role)
		req.Header.Set("Resolved-User-ID", user)
		rec := httptest.NewRecorder()
		handleCourierResolveBooking(rec, req)
		return rec
	}
	state := func() string {
		var s string
		_ = db.DB.QueryRow(`SELECT COALESCE(data->>'awb_booking_state','') FROM `+schema+`.documents WHERE id=$1`, id).Scan(&s)
		return s
	}

	if rec := call(role); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "GLOBAL-0011") || state() != "uncertain" {
		t.Fatalf("a role without update on LogisticsBooking: want 403 GLOBAL-0011 and no change, got %d %s (state %q)", rec.Code, rec.Body.String(), state())
	}
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES ($1,'LogisticsBooking',TRUE,FALSE,TRUE,FALSE)`, role); err != nil {
		t.Fatal(err)
	}
	if rec := call(role); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"outcome":"not_booked"`) || state() != "" {
		t.Fatalf("with update on LogisticsBooking: want 200 and the claim released, got %d %s (state %q)", rec.Code, rec.Body.String(), state())
	}
	if rec := call(role); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "no unconfirmed courier booking") {
		t.Fatalf("nothing left to resolve: want 422, got %d %s", rec.Code, rec.Body.String())
	}
}
