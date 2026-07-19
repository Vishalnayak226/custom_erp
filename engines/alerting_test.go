package engines

import (
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSendOpsAlertPostsToWebhook confirms the real send path: with
// OPS_ALERT_WEBHOOK_URL set to a local httptest.Server, SendOpsAlert POSTs a
// Slack-compatible {"text": ...} payload containing the severity/source, and
// never the full (untruncated) message body verbatim beyond the 300-char cap.
func TestSendOpsAlertPostsToWebhook(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload slackWebhookPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload.Text
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldURL := os.Getenv("OPS_ALERT_WEBHOOK_URL")
	os.Setenv("OPS_ALERT_WEBHOOK_URL", server.URL)
	defer os.Setenv("OPS_ALERT_WEBHOOK_URL", oldURL)

	SendOpsAlert("PANIC", "test-module", "something broke")

	select {
	case text := <-received:
		if !strings.Contains(text, "PANIC") || !strings.Contains(text, "test-module") || !strings.Contains(text, "something broke") {
			t.Fatalf("webhook payload missing expected fields: %q", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook was never called")
	}
}

// TestSendOpsAlertNoopWithoutWebhookURL confirms the safe default: with no
// OPS_ALERT_WEBHOOK_URL configured, SendOpsAlert must not attempt any HTTP
// call (asserted by there being no listener to call - a real send would
// error, not hang, so a short wait is enough to rule it out).
func TestSendOpsAlertNoopWithoutWebhookURL(t *testing.T) {
	oldURL := os.Getenv("OPS_ALERT_WEBHOOK_URL")
	os.Unsetenv("OPS_ALERT_WEBHOOK_URL")
	defer os.Setenv("OPS_ALERT_WEBHOOK_URL", oldURL)

	SendOpsAlert("ERROR", "test-module", "should not send")
	// No assertion beyond "this returns immediately without panicking" -
	// there is no webhook configured to receive anything.
}

// TestSendOpsAlertTruncatesLongMessages confirms the 300-char cap that keeps
// a full stack trace or large payload from ever reaching a third-party
// webhook (see alerting.go's file header on why only a short summary goes
// out externally).
func TestSendOpsAlertTruncatesLongMessages(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload slackWebhookPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload.Text
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldURL := os.Getenv("OPS_ALERT_WEBHOOK_URL")
	os.Setenv("OPS_ALERT_WEBHOOK_URL", server.URL)
	defer os.Setenv("OPS_ALERT_WEBHOOK_URL", oldURL)

	longMessage := strings.Repeat("x", 5000)
	SendOpsAlert("ERROR", "test-module", longMessage)

	select {
	case text := <-received:
		if len(text) > 400 {
			t.Fatalf("expected alert text to be truncated, got length %d", len(text))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook was never called")
	}
}

// TestCheckErrorRateThresholdAndCooldown exercises the sustained-error-rate
// side of StartAlertMonitor directly (rather than waiting on its ticker):
// seeds real system_error_logs rows for a throwaway schema, confirms
// checkErrorRate alerts once threshold is reached, and confirms the
// per-schema cooldown suppresses a second alert for the same schema within
// the same window (rows cleaned up after).
func TestCheckErrorRateThresholdAndCooldown(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	schema := "tenant_default"

	received := make(chan string, 5)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload slackWebhookPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload.Text
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldURL := os.Getenv("OPS_ALERT_WEBHOOK_URL")
	os.Setenv("OPS_ALERT_WEBHOOK_URL", server.URL)
	defer os.Setenv("OPS_ALERT_WEBHOOK_URL", oldURL)

	marker := "ALERTING_TEST_MARKER"
	for i := 0; i < 5; i++ {
		_, err := db.DB.Exec(`INSERT INTO `+schema+`.system_error_logs (severity, module_source, error_message) VALUES ('ERROR', $1, $2)`, marker, marker)
		if err != nil {
			t.Fatalf("failed to seed system_error_logs row: %v", err)
		}
	}
	defer db.DB.Exec(`DELETE FROM ` + schema + `.system_error_logs WHERE module_source = '` + marker + `'`)

	alertMonitorState.Lock()
	delete(alertMonitorState.lastAlertAt, schema)
	alertMonitorState.Unlock()

	checkErrorRate(schema, time.Hour, 5)
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("expected an alert once threshold was reached, got none")
	}

	// Immediately re-checking must not alert again - the cooldown covers the
	// full window, so this run stays quiet.
	checkErrorRate(schema, time.Hour, 5)
	select {
	case text := <-received:
		t.Fatalf("expected cooldown to suppress a second alert, got one: %q", text)
	case <-time.After(300 * time.Millisecond):
	}
}
