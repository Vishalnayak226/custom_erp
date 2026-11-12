package engines

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 49.11.5: a scheduled test alert through the same webhook the urgent
// detections use, to prove it still delivers. Off by default
// (security.test_alert_hours = Off). Each attempt is a signed audit row
// (SECURITY_TEST_ALERT, DELIVERED or FAILED), so "last delivered" survives a
// restart and the Security pane can show it beside "Checks last ran". A failed
// delivery is picked up by the test_alert_failed detection, which queues for
// review (it cannot page through the channel that just failed). The message is
// marked TEST and never goes through SendOpsAlert's urgent path.
const (
	SettingSecurityTestAlertHours = "security.test_alert_hours"
	TestAlertAction               = "SECURITY_TEST_ALERT"
	testAlertDelivered            = "DELIVERED"
	testAlertFailed               = "FAILED"
	testAlertRetryAfter           = time.Hour
)

// postOpsAlertSync is postOpsAlert that tells the caller how it went.
func postOpsAlertSync(webhookURL, text string) error {
	body, err := json.Marshal(slackWebhookPayload{Text: text})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not build the request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := opsAlertHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("the webhook could not be reached")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("the webhook answered HTTP %d", resp.StatusCode)
	}
	return nil
}

// RunScheduledTestAlert sends this client's test alert when it is due and
// reports whether it tried. A no-op while the setting is off, while no webhook
// is configured (the pane already says "Not set up") or while external side
// effects are off.
func RunScheduledTestAlert(tenantID string, now time.Time) (bool, error) {
	hours := GetSettingInt(tenantID, SettingSecurityTestAlertHours)
	if hours <= 0 {
		return false, nil
	}
	webhook := strings.TrimSpace(os.Getenv(OpsAlertWebhookEnv))
	if webhook == "" || !ExternalSideEffectsEnabled() {
		return false, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var lastAt time.Time
	var lastStatus string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT created_at, COALESCE(status, '') FROM %s.audit_logs
		WHERE action = $1 AND signature IS NOT NULL ORDER BY created_at DESC LIMIT 1`, schema), TestAlertAction).Scan(&lastAt, &lastStatus)
	if err == nil {
		wait := time.Duration(hours) * time.Hour
		if lastStatus == testAlertFailed {
			wait = testAlertRetryAfter
		}
		if lastAt.UTC().Add(wait).After(now.UTC()) {
			return false, nil
		}
	}
	text := fmt.Sprintf(":white_check_mark: [TEST] Scheduled alert delivery check from %s. No action needed.%s", schema, opsAlertResponderSuffix())
	if perr := postOpsAlertSync(webhook, text); perr != nil {
		LogAuditEvent(tenantID, "system", TestAlertAction, testAlertFailed, "Test alert not delivered: "+perr.Error())
		return true, nil
	}
	LogAuditEvent(tenantID, "system", TestAlertAction, testAlertDelivered, "Test alert delivered")
	return true, nil
}

// TestAlertState is what the Security pane shows: the setting, when a test
// last got through, and whether the latest attempt failed.
type TestAlertState struct {
	EveryHours    int        `json:"every_hours"`
	LastDelivered *time.Time `json:"last_delivered,omitempty"`
	LastFailed    bool       `json:"last_failed"`
}

func testAlertState(tenantID, schema string) TestAlertState {
	st := TestAlertState{EveryHours: GetSettingInt(tenantID, SettingSecurityTestAlertHours)}
	var at time.Time
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT created_at FROM %s.audit_logs
		WHERE action = $1 AND status = $2 AND signature IS NOT NULL ORDER BY created_at DESC LIMIT 1`, schema),
		TestAlertAction, testAlertDelivered).Scan(&at); err == nil {
		at = at.UTC()
		st.LastDelivered = &at
	}
	var lastStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(status, '') FROM %s.audit_logs
		WHERE action = $1 AND signature IS NOT NULL ORDER BY created_at DESC LIMIT 1`, schema), TestAlertAction).Scan(&lastStatus); err == nil {
		st.LastFailed = lastStatus == testAlertFailed
	}
	return st
}
