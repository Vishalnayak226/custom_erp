package engines

import (
	"custom_erp/db"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stage 49.11.5 - scheduled test alert through the webhook.

type testAlertSink struct {
	mu     sync.Mutex
	texts  []string
	status int
}

func (s *testAlertSink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var p slackWebhookPayload
	_ = json.Unmarshal(b, &p)
	s.mu.Lock()
	s.texts = append(s.texts, p.Text)
	st := s.status
	s.mu.Unlock()
	w.WriteHeader(st)
}

func (s *testAlertSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.texts)
}

func testAlertFixture(t *testing.T, status int) *testAlertSink {
	t.Helper()
	db.InitDB(testConnStr())
	sink := &testAlertSink{status: status}
	srv := httptest.NewServer(sink)
	t.Cleanup(srv.Close)
	t.Setenv(OpsAlertWebhookEnv, srv.URL)
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1")
	clear := func() {
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1`, TestAlertAction)
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = 'SECURITY_ALERT' AND details LIKE '%test_alert_failed%'`)
	}
	clear()
	t.Cleanup(clear)
	return sink
}

func TestStage49115TestAlertOffSendsNothing(t *testing.T) {
	sink := testAlertFixture(t, 200)
	tuningSetting(t, SettingSecurityTestAlertHours, "0")
	if sent, err := RunScheduledTestAlert("default", time.Now()); sent || err != nil || sink.count() != 0 {
		t.Fatalf("setting off: sent %v err %v posts %d", sent, err, sink.count())
	}
	st, err := GetSecurityPosture("default")
	if err != nil || st.TestAlert.EveryHours != 0 || st.TestAlert.LastDelivered != nil {
		t.Errorf("posture with the setting off: %+v err %v", st.TestAlert, err)
	}
}

func TestStage49115TestAlertDeliveredAndPaced(t *testing.T) {
	sink := testAlertFixture(t, 200)
	tuningSetting(t, SettingSecurityTestAlertHours, "24")
	now := time.Now()
	if sent, err := RunScheduledTestAlert("default", now); !sent || err != nil || sink.count() != 1 {
		t.Fatalf("first run: sent %v err %v posts %d", sent, err, sink.count())
	}
	if !strings.Contains(sink.texts[0], "[TEST]") || strings.Contains(strings.ToUpper(sink.texts[0]), "URGENT") {
		t.Errorf("message is not marked as a test: %q", sink.texts[0])
	}
	if sent, _ := RunScheduledTestAlert("default", now.Add(2*time.Hour)); sent || sink.count() != 1 {
		t.Errorf("sent again inside the interval (posts %d)", sink.count())
	}
	if sent, _ := RunScheduledTestAlert("default", now.Add(25*time.Hour)); !sent || sink.count() != 2 {
		t.Errorf("not sent after the interval (posts %d)", sink.count())
	}
	st, err := GetSecurityPosture("default")
	if err != nil || st.TestAlert.LastDelivered == nil || st.TestAlert.LastFailed || st.TestAlert.EveryHours != 24 {
		t.Errorf("posture: %+v err %v", st.TestAlert, err)
	}
	if fired, _ := RunSecurityDetections("default", time.Now()); func() bool {
		for _, f := range fired {
			if f.Rule == "test_alert_failed" {
				return true
			}
		}
		return false
	}() {
		t.Error("a delivered test raised the missing-signal alert")
	}
}

func TestStage49115FailedDeliveryRaisesTheAlert(t *testing.T) {
	sink := testAlertFixture(t, 500)
	tuningSetting(t, SettingSecurityTestAlertHours, "24")
	now := time.Now()
	if sent, err := RunScheduledTestAlert("default", now); !sent || err != nil {
		t.Fatalf("failing run: sent %v err %v", sent, err)
	}
	if sent, _ := RunScheduledTestAlert("default", now.Add(10*time.Minute)); sent {
		t.Error("retried inside the hour")
	}
	st, _ := GetSecurityPosture("default")
	if st == nil || !st.TestAlert.LastFailed || st.TestAlert.LastDelivered != nil {
		t.Errorf("posture after a failure: %+v", st)
	}
	fired, err := RunSecurityDetections("default", time.Now())
	got := 0
	for _, f := range fired {
		if f.Rule == "test_alert_failed" {
			got++
		}
	}
	if err != nil || got != 1 {
		t.Errorf("missing-signal alert fired %d times (err %v), want 1", got, err)
	}
	// The hourly retry recovers once the webhook answers again.
	sink.mu.Lock()
	sink.status = 200
	sink.mu.Unlock()
	if sent, _ := RunScheduledTestAlert("default", now.Add(70*time.Minute)); !sent {
		t.Error("no retry an hour after the failure")
	}
	if st, _ = GetSecurityPosture("default"); st.TestAlert.LastFailed || st.TestAlert.LastDelivered == nil {
		t.Errorf("posture after recovery: %+v", st.TestAlert)
	}
}
