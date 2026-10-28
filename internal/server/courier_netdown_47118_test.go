package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 47.11.8: what the operator's screen receives when a courier is down, and
// that every other courier failure keeps the answer it always had.
func TestCourierErrorEnvelope47118(t *testing.T) {
	db.InitDB(testConnStr())
	decode := func(t *testing.T, rec *httptest.ResponseRecorder) apiErrorBody {
		t.Helper()
		var body apiErrorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("not an error envelope: %s", rec.Body.String())
		}
		return body
	}
	call := func(err error) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		writeCourierError(rec, httptest.NewRequest(http.MethodPost, "/api/v1/courier/delhivery/awb", nil), err)
		return rec
	}

	line := "delhivery could not be reached - nothing was sent and nothing was changed here. Try again shortly."
	rec := call(&engines.ValidationError{Code: engines.CourierUnreachableCode, Message: line})
	body := decode(t, rec)
	if rec.Code != http.StatusServiceUnavailable || body.Code != engines.CourierUnreachableCode || !body.Retryable || body.Detail != line || body.UserAction == "" {
		t.Fatalf("courier down: want 503 %s retryable with the engine's line, got %d %+v", engines.CourierUnreachableCode, rec.Code, body)
	}

	// Unchanged: a plain engine error, a breaker-open or switched-off
	// refusal (CONN-0225) - all the 422 and verbatim message they had.
	for _, err := range []error{
		errors.New(`booking B1 cannot allocate an AWB from status "Handed Over"`),
		&engines.ValidationError{Code: "CONN-0225", Message: `circuit breaker open for "courier:delhivery" - too many recent failures, not attempting this call`},
	} {
		rec := call(err)
		body := decode(t, rec)
		if rec.Code != http.StatusUnprocessableEntity || body.Code != "GLOBAL-0002" || body.Error != err.Error() {
			t.Fatalf("%q: want the unchanged 422 GLOBAL-0002, got %d %+v", err, rec.Code, body)
		}
	}
}
