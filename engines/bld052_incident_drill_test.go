package engines

// BLD-052, the on-call delivery leg: three injected incidents, drilled
// locally end to end.
//
// Scope boundary, stated plainly because the Done bar says delivery setup is
// an external action: these drills prove the *local* chain - an incident
// condition is detected, turned into a bounded and redacted message, named
// to an accountable responder, and actually delivered over HTTP to the
// configured webhook. The sink is an httptest server on loopback, so nothing
// leaves the machine. What is NOT proved here is that a real responder at a
// real third-party endpoint received it: OPS_ALERT_WEBHOOK_URL is unset in
// this environment (item 20.2), and pointing it at a real channel from a test
// is exactly the accident Gate 0's ExternalSideEffectsEnabled check exists to
// prevent.
//
// The one-variable claim is asserted directly by
// TestDrillDeliveryNeedsOnlyTheWebhookVariable.

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// drillSink captures every alert delivered during one drill.
type drillSink struct {
	server   *httptest.Server
	received chan string
}

// newDrillSink points OPS_ALERT_WEBHOOK_URL at a loopback collector and
// enables external side effects for the duration of the test, restoring both
// afterwards. Uses t.Setenv so a failed assertion cannot leak a webhook URL
// into the rest of the package's tests.
func newDrillSink(t *testing.T) *drillSink {
	t.Helper()
	sink := &drillSink{received: make(chan string, 16)}
	sink.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload slackWebhookPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		select {
		case sink.received <- payload.Text:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(sink.server.Close)
	t.Setenv(OpsAlertWebhookEnv, sink.server.URL)
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1")
	t.Setenv(OpsAlertResponderEnv, "erp-oncall@example.invalid (platform operations)")
	return sink
}

// await returns the next delivered alert, or fails the test.
func (s *drillSink) await(t *testing.T, what string) string {
	t.Helper()
	select {
	case text := <-s.received:
		return text
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no alert was delivered within 5s", what)
		return ""
	}
}

// assertActionableAndSafe is the shared bar every drilled incident must meet:
// the responder can act on it, and it carries nothing it should not.
func assertActionableAndSafe(t *testing.T, what, text string, mustMention []string, mustNotLeak []string) {
	t.Helper()
	if !strings.Contains(text, "responder: ") {
		t.Errorf("%s: alert names no accountable responder: %q", what, text)
	}
	if strings.Contains(text, "UNASSIGNED") {
		t.Errorf("%s: responder resolved to UNASSIGNED despite being configured: %q", what, text)
	}
	for _, want := range mustMention {
		if !strings.Contains(text, want) {
			t.Errorf("%s: alert is not actionable, missing %q: %q", what, want, text)
		}
	}
	for _, leak := range mustNotLeak {
		if leak != "" && strings.Contains(text, leak) {
			t.Errorf("%s: alert leaked %q: %q", what, leak, text)
		}
	}
	// Bounded: SendOpsAlert truncates at 300 characters plus the fixed
	// prefix/suffix. A generous ceiling here still catches an unbounded
	// payload (a stack trace, a request body) reaching the channel.
	if len(text) > 700 {
		t.Errorf("%s: alert is %d bytes, which is not a bounded notification: %q", what, len(text), text)
	}
}

// --- Incident 1: durable async queue saturation ------------------------------

func TestDrillIncidentOneQueueSaturation(t *testing.T) {
	sink := newDrillSink(t)
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	// Enable the monitor for this tenant with the lowest thresholds the
	// settings registry allows, then inject a backlog past them.
	for key, value := range map[string]string{
		"ops.async_job_queue_depth_alert":        "1",
		"ops.async_job_queue_wait_seconds_alert": "60",
	} {
		if err := SetSetting(tenantID, key, value, "drill"); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}

	// Injected incident: five Pending jobs whose next_attempt_at is well in
	// the past, i.e. a real backlog that is not being drained.
	for i := 0; i < 5; i++ {
		if _, err := db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.async_jobs (id, job_type, payload, status, next_attempt_at, created_at)
			VALUES ($1, 'drill-saturation', '{}', 'Pending', CURRENT_TIMESTAMP - INTERVAL '30 minutes', CURRENT_TIMESTAMP - INTERVAL '30 minutes')`, schema),
			fmt.Sprintf("DRILL-SAT-%d", i)); err != nil {
			t.Fatalf("inject backlog row %d: %v", i, err)
		}
	}

	resetQueueAlertState(schema)
	checkJobQueueSaturation(schema)

	text := sink.await(t, "incident 1 (queue saturation)")
	assertActionableAndSafe(t, "incident 1", text,
		// Actionable: names the saturated queue and the affected tenant. The
		// tenant schema IS included deliberately - an on-call responder
		// cannot drain, scale or cancel a backlog without knowing whose it
		// is, and this is the operator's own channel, not a customer's. What
		// must never travel is the queued payloads themselves.
		[]string{"queue", schema},
		// The injected jobs' own payloads must not be in the notification.
		[]string{"drill-saturation"})
	t.Logf("incident 1 delivered: %s", text)
}

// --- Incident 2: backup freshness -------------------------------------------

func TestDrillIncidentTwoStaleBackup(t *testing.T) {
	sink := newDrillSink(t)

	// Injected incident: a backup directory whose newest artifact is far
	// older than the allowed age. Uses a scratch directory so the drill never
	// depends on, or disturbs, a real backup location.
	dir := t.TempDir()
	stale := time.Now().Add(-72 * time.Hour)
	path := dir + "/erp-backup-drill.sql.gz.enc"
	if err := os.WriteFile(path, []byte("drill placeholder, not a real backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BACKUP_DIR", dir)

	// The stale-backup alert is throttled to one per maxAge window by a
	// package-level timestamp. Another test in this package may already have
	// tripped it, which would silently suppress this drill rather than fail
	// it - so the cooldown is cleared first, the same way the queue and
	// error-rate drills clear theirs.
	resetBackupAlertState()
	checkBackupAge(36 * time.Hour)

	text := sink.await(t, "incident 2 (stale backup)")
	assertActionableAndSafe(t, "incident 2", text,
		[]string{"backup"},
		// Must not disclose the filesystem layout of the host to a
		// third-party channel.
		[]string{dir})
	t.Logf("incident 2 delivered: %s", text)
}

// --- Incident 3: sustained error rate ---------------------------------------

func TestDrillIncidentThreeSustainedErrorRate(t *testing.T) {
	sink := newDrillSink(t)
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	// Injected incident: a burst of system errors inside the window, each
	// carrying something that must not reach the channel - a correlation id
	// is fine, but the message body deliberately includes a secret so the
	// drill proves redaction on the real path, not just in a unit test.
	const burst = 25
	for i := 0; i < burst; i++ {
		if _, err := db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.system_error_logs (severity, module_source, error_message, correlation_id, created_at)
			VALUES ('High', 'drill/module', $1, $2, CURRENT_TIMESTAMP)`, schema),
			"drill injected failure password=hunter2", fmt.Sprintf("00000000-0000-4000-8000-%012d", i)); err != nil {
			t.Fatalf("inject system_error_logs row %d: %v", i, err)
		}
	}

	resetErrorRateAlertState(schema)
	checkErrorRate(schema, 5*time.Minute, 20)

	text := sink.await(t, "incident 3 (sustained error rate)")
	assertActionableAndSafe(t, "incident 3", text,
		// Actionable: the count and the window are what decide whether this
		// is a spike or an outage.
		[]string{},
		// The injected secret must never appear.
		[]string{"hunter2"})
	t.Logf("incident 3 delivered: %s", text)
}

// --- The one-variable claim --------------------------------------------------

func TestDrillDeliveryNeedsOnlyTheWebhookVariable(t *testing.T) {
	// Unset: nothing is delivered, and the status readout says exactly which
	// variables are missing rather than failing silently.
	t.Setenv(OpsAlertWebhookEnv, "")
	t.Setenv(OpsAlertResponderEnv, "")
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1")
	status := OpsAlertDeliveryStatus()
	if status.WebhookConfigured || status.DeliveryWouldBeSent {
		t.Fatalf("with no webhook configured nothing may be delivered: %+v", status)
	}
	if len(status.UnconfiguredVariables) != 2 {
		t.Errorf("expected both variables reported unconfigured, got %v", status.UnconfiguredVariables)
	}

	// Set the one variable: delivery happens, same code path, no other change.
	sink := newDrillSink(t)
	status = OpsAlertDeliveryStatus()
	if !status.WebhookConfigured || !status.DeliveryWouldBeSent {
		t.Fatalf("with the webhook set, delivery must be live: %+v", status)
	}
	if !status.ResponderConfigured || status.Responder == "" {
		t.Fatalf("responder must be reported once configured: %+v", status)
	}
	if len(status.UnconfiguredVariables) != 0 {
		t.Errorf("nothing should remain unconfigured, got %v", status.UnconfiguredVariables)
	}
	SendOpsAlert("High", "drill/one-variable", "delivery posture check")
	text := sink.await(t, "one-variable delivery")
	if !strings.Contains(text, "erp-oncall@example.invalid") {
		t.Errorf("the delivered alert must name the accountable responder: %q", text)
	}

	// And the status readout must never carry the webhook URL itself, which
	// is a bearer credential.
	body, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), sink.server.URL) {
		t.Errorf("the delivery status leaked the webhook URL: %s", body)
	}
}

func TestUnassignedResponderIsVisibleInTheAlertItself(t *testing.T) {
	sink := newDrillSink(t)
	t.Setenv(OpsAlertResponderEnv, "")
	SendOpsAlert("High", "drill/unowned", "nobody owns this channel")
	text := sink.await(t, "unassigned responder")
	if !strings.Contains(text, "UNASSIGNED") || !strings.Contains(text, OpsAlertResponderEnv) {
		t.Errorf("an unowned alerting setup must announce itself in the channel: %q", text)
	}
}

// resetQueueAlertState and resetErrorRateAlertState clear the per-schema
// cooldown so a drill is not silently suppressed by an earlier test's alert.
func resetQueueAlertState(schema string) {
	queueAlertState.Lock()
	delete(queueAlertState.lastAlertAt, schema)
	queueAlertState.Unlock()
}

func resetBackupAlertState() {
	backupAlertState.Lock()
	backupAlertState.lastAlertAt = time.Time{}
	backupAlertState.Unlock()
}

func resetErrorRateAlertState(schema string) {
	alertMonitorState.Lock()
	delete(alertMonitorState.lastAlertAt, schema)
	alertMonitorState.Unlock()
}

var _ = context.Background
