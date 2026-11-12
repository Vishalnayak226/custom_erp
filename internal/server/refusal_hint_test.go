package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStage140144HintFollowsTheKindOfRefusal: GLOBAL-0002's "Correct the
// field format." stays on a format error and is replaced by advice that fits
// under a delete, already-exists, packed-order and return-routing refusal.
func TestStage140144HintFollowsTheKindOfRefusal(t *testing.T) {
	const formatHint = "Correct the field format."
	hintOf := func(write func(w http.ResponseWriter, r *http.Request)) string {
		rec := httptest.NewRecorder()
		write(rec, httptest.NewRequest(http.MethodPost, "/api/v1/doc/Item", nil))
		var body struct {
			UserAction string `json:"user_action"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body.UserAction
	}
	generic := func(msg string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, msg)
		}
	}
	detail := func(msg string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) { writeAPIErrorDetail(w, r, "GLOBAL-0002", "", msg) }
	}
	kinds := []struct {
		name, msg, want string
	}{
		{"delete refused", "An approved or posted document cannot be deleted - reverse it instead", "instead of deleting"},
		{"already exists", `"INV-1" already exists - open that record to change it, or use a different number.`, "existing record"},
		{"packed order cancel", "Part of this order is already packed, so it cannot be cancelled", "raise a return"},
		{"return routing", "return_location is required (no return location could be auto-routed for this request)", "return location"},
	}
	for _, k := range kinds {
		for path, w := range map[string]func(http.ResponseWriter, *http.Request){"generic": generic(k.msg), "detail": detail(k.msg)} {
			got := hintOf(w)
			if got == formatHint || !strings.Contains(got, k.want) {
				t.Errorf("%s (%s): hint %q, want one containing %q", k.name, path, got, k.want)
			}
		}
	}
	for _, msg := range []string{"credit_limit must be a number", "Invalid date: expected YYYY-MM-DD"} {
		if got := hintOf(generic(msg)); got != formatHint {
			t.Errorf("format error %q lost its hint: %q", msg, got)
		}
		if got := hintOf(detail(msg)); got != formatHint {
			t.Errorf("format error %q (detail) lost its hint: %q", msg, got)
		}
	}
	// Another code keeps its own hint whatever the text says.
	if got := hintOf(func(w http.ResponseWriter, r *http.Request) {
		writeAPIErrorDetail(w, r, "MASTER-1321", "", "already exists")
	}); got != "Use the existing record, change these values, or make that record inactive first." {
		t.Errorf("another code's hint changed: %q", got)
	}
}
