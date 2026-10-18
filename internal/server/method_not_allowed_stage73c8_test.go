package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 73.C.8: a 405 says the method is not supported (GLOBAL-0303), not
// "Correct the field format" (GLOBAL-0002).
func TestStage73C8MethodNotAllowedCode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAPIErrorGeneric(rec, httptest.NewRequest(http.MethodPatch, "/api/v1/orders", nil), http.StatusMethodNotAllowed, "Method not allowed")
	var body struct {
		Code       string `json:"code"`
		UserAction string `json:"user_action"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec.Code != http.StatusMethodNotAllowed || body.Code != "GLOBAL-0303" {
		t.Fatalf("want 405 GLOBAL-0303, got %d %q (%q)", rec.Code, body.Code, body.UserAction)
	}
}

// 73.C.8: the router's own 405 (a known /api/ address, the wrong method)
// becomes the envelope with an Allow header; an unknown address stays a 404,
// and a matching method is served.
func TestStage73C8RouterMethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/things", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.Handle("/", onlyReadMethods(http.NotFoundHandler())) // the static catch-all, as in routes.go
	h := apiMethodNotAllowed(mux)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/v1/things", nil))
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if rec.Code != http.StatusMethodNotAllowed || body.Code != "GLOBAL-0303" || rec.Header().Get("Allow") != "POST" {
		t.Fatalf("wrong method: want 405 GLOBAL-0303 Allow POST, got %d %q Allow %q", rec.Code, body.Code, rec.Header().Get("Allow"))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nothing-here", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown address: want 404, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/things", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("right method: want it served (204), got %d", rec.Code)
	}
}
