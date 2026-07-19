package engines

import (
	"bytes"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// Ops alerting (Stage 17.10). Posts to a Slack-compatible incoming webhook
// (Slack itself, or Microsoft Teams' classic "Incoming Webhook" connector -
// both accept a simple {"text": ...} payload) configured via
// OPS_ALERT_WEBHOOK_URL. Unset by default so dev/test environments never
// need it configured - a missing webhook just logs locally and is a no-op,
// never blocks the caller that triggered it.
//
// Deliberately sends only severity/source/a truncated message to the
// external destination - never a full stack trace or request body, since
// that payload leaves this process for a third-party service. Full detail
// stays in system_error_logs / the Log Hub, one hop away via the
// correlation id already in the log line next to it.

var opsAlertHTTPClient = &http.Client{Timeout: 5 * time.Second}

type slackWebhookPayload struct {
	Text string `json:"text"`
}

// SendOpsAlert posts a short alert to the configured webhook. Fire-and-forget:
// runs in its own goroutine so a slow or unreachable webhook never adds
// latency to the request or worker tick that triggered it.
func SendOpsAlert(severity, source, message string) {
	webhookURL := os.Getenv("OPS_ALERT_WEBHOOK_URL")
	if webhookURL == "" {
		log.Printf("[ALERT] (no OPS_ALERT_WEBHOOK_URL configured, not sent) [%s] %s: %s", severity, source, message)
		return
	}
	text := fmt.Sprintf(":rotating_light: [%s] %s: %s", severity, source, truncateForAlert(message))
	go postOpsAlert(webhookURL, text)
}

func truncateForAlert(s string) string {
	const maxLen = 300
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func postOpsAlert(webhookURL, text string) {
	body, err := json.Marshal(slackWebhookPayload{Text: text})
	if err != nil {
		log.Printf("[ALERT] failed to marshal payload: %v", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[ALERT] failed to build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := opsAlertHTTPClient.Do(req)
	if err != nil {
		log.Printf("[ALERT] webhook delivery failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("[ALERT] webhook returned HTTP %d", resp.StatusCode)
	}
}

// alertMonitorState tracks the last sustained-error-rate alert sent per
// tenant schema, so a schema stuck above threshold triggers one alert per
// cooldown window rather than one every poll tick.
var alertMonitorState = struct {
	sync.Mutex
	lastAlertAt map[string]time.Time
}{lastAlertAt: map[string]time.Time{}}

// StartAlertMonitor polls system_error_logs per tenant schema and alerts
// once per cooldown window if the row count within `window` reaches
// `threshold`. Counts every logged error regardless of its severity label
// (call sites across this codebase use PANIC alongside module-specific
// labels like APPROVAL_RESET_FAILED - see engines/logs.go's LogSystemError
// callers - so filtering to a fixed severity set would miss real failures).
// This is the "sustained error rate" alert; a single PANIC still alerts
// immediately and separately via LogSystemError itself.
func StartAlertMonitor(pollInterval, window time.Duration, threshold int) {
	ticker := time.NewTicker(pollInterval)
	go func() {
		for range ticker.C {
			if db.DB == nil {
				continue
			}
			schemas, err := listTenantSchemas()
			if err != nil {
				log.Printf("[ALERT-MONITOR] failed to list tenant schemas: %v", err)
				continue
			}
			for _, schema := range schemas {
				checkErrorRate(schema, window, threshold)
			}
		}
	}()
}

func checkErrorRate(schema string, window time.Duration, threshold int) {
	cutoff := time.Now().Add(-window)
	var count int
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.system_error_logs WHERE created_at > $1`, schema), cutoff).Scan(&count)
	if err != nil || count < threshold {
		return
	}

	alertMonitorState.Lock()
	last, seen := alertMonitorState.lastAlertAt[schema]
	shouldAlert := !seen || time.Since(last) > window
	if shouldAlert {
		alertMonitorState.lastAlertAt[schema] = time.Now()
	}
	alertMonitorState.Unlock()

	if shouldAlert {
		SendOpsAlert("SUSTAINED_ERROR_RATE", schema, fmt.Sprintf("%d errors logged in the last %s", count, window))
	}
}
