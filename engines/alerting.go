package engines

import (
	"bytes"
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
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
// stays in system_error_logs / the Activity Log, one hop away via the
// correlation id already in the log line next to it.

var opsAlertHTTPClient = &http.Client{Timeout: 5 * time.Second}

type slackWebhookPayload struct {
	Text string `json:"text"`
}

// SendOpsAlert posts a short alert to the configured webhook. Fire-and-forget:
// runs in its own goroutine so a slow or unreachable webhook never adds
// latency to the request or worker tick that triggered it.
func SendOpsAlert(severity, source, message string) {
	severity = redactAndBoundLogField(severity, 32)
	source = redactAndBoundLogField(source, 160)
	message = truncateForAlert(redactAndBoundLogField(message, 4*1024))
	webhookURL := os.Getenv("OPS_ALERT_WEBHOOK_URL")
	if webhookURL == "" {
		// OBS-0215 (Stage 25.5): "Alert webhook missing" - exactly this
		// no-op path. Log-only (there's no HTTP request/tenant context at
		// the point most callers of SendOpsAlert fire from - background
		// workers, panic recovery - to attach a coded API response to).
		log.Printf("[OBS-0215] (no %s configured, not sent) [%s] %s: %s%s", OpsAlertWebhookEnv, severity, source, message, opsAlertResponderSuffix())
		return
	}
	if !ExternalSideEffectsEnabled() {
		// Stage 47.0.5/47.11.6 Gate 0: OPS_ALERT_WEBHOOK_URL configured
		// against a shared dev/staging channel must not actually post just
		// because a regression/abuse test or a developer triggered this path.
		log.Printf("[OPS-ALERT] (external side effects OFF - not sent) [%s] %s: %s", severity, source, truncateForAlert(message))
		return
	}
	text := fmt.Sprintf(":rotating_light: [%s] %s: %s%s", severity, source, message, opsAlertResponderSuffix())
	go postOpsAlert(webhookURL, text)
}

// OpsAlertResponderEnv and OpsAlertWebhookEnv are the two variables that turn
// local alerting into delivered alerting (BLD-052). Named constants rather
// than inline strings so the admin guide, the drill and the readiness-style
// status readout below cannot drift from what the code actually reads.
const (
	OpsAlertWebhookEnv   = "OPS_ALERT_WEBHOOK_URL"
	OpsAlertResponderEnv = "OPS_ALERT_RESPONDER"
)

// opsAlertResponderSuffix names the accountable responder on every alert.
//
// BLD-052's Done bar is about an incident reaching an *intended responder*,
// not merely reaching a channel. An alert posted into a room nobody owns is
// the failure mode this guards: when OPS_ALERT_RESPONDER is unset the alert
// says so in its own text, so an unowned alerting setup is visible in the
// channel itself rather than discovered during an incident.
func opsAlertResponderSuffix() string {
	responder := redactAndBoundLogField(strings.TrimSpace(os.Getenv(OpsAlertResponderEnv)), 120)
	if responder == "" {
		return " | responder: UNASSIGNED (set " + OpsAlertResponderEnv + ")"
	}
	return " | responder: " + responder
}

// OpsAlertDelivery describes whether alerts can actually reach anyone. Used
// by the local drill to assert the one-variable claim, and safe to surface to
// an operator: it reports whether a URL is configured, never the URL itself
// (a Slack/Teams webhook URL is a bearer credential).
type OpsAlertDelivery struct {
	WebhookConfigured     bool     `json:"webhook_configured"`
	ResponderConfigured   bool     `json:"responder_configured"`
	Responder             string   `json:"responder,omitempty"`
	ExternalEffectsOn     bool     `json:"external_side_effects_enabled"`
	DeliveryWouldBeSent   bool     `json:"delivery_would_be_sent"`
	UnconfiguredVariables []string `json:"unconfigured_variables,omitempty"`
}

// OpsAlertDeliveryStatus reports the current delivery posture.
func OpsAlertDeliveryStatus() OpsAlertDelivery {
	webhook := strings.TrimSpace(os.Getenv(OpsAlertWebhookEnv))
	responder := redactAndBoundLogField(strings.TrimSpace(os.Getenv(OpsAlertResponderEnv)), 120)
	external := ExternalSideEffectsEnabled()
	status := OpsAlertDelivery{
		WebhookConfigured:   webhook != "",
		ResponderConfigured: responder != "",
		Responder:           responder,
		ExternalEffectsOn:   external,
		DeliveryWouldBeSent: webhook != "" && external,
	}
	if webhook == "" {
		status.UnconfiguredVariables = append(status.UnconfiguredVariables, OpsAlertWebhookEnv)
	}
	if responder == "" {
		status.UnconfiguredVariables = append(status.UnconfiguredVariables, OpsAlertResponderEnv)
	}
	return status
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

var queueAlertState = struct {
	sync.Mutex
	lastAlertAt map[string]time.Time
}{lastAlertAt: map[string]time.Time{}}

const queueAlertCooldown = 5 * time.Minute

// StartQueueSaturationMonitor watches durable async job backlog per tenant.
// Thresholds come from the existing per-tenant settings registry; the monitor
// adds no service, queue or external dependency. Delivery stays behind the
// same OPS_ALERT_WEBHOOK_URL / external-side-effects gate as all other alerts.
func StartQueueSaturationMonitor(ctx context.Context, pollInterval time.Duration) {
	if pollInterval <= 0 {
		pollInterval = time.Minute
	}
	ticker := time.NewTicker(pollInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[QUEUE-MONITOR] failed to list tenant schemas: %v", redactAndBoundLogField(err.Error(), maxSystemErrorMessageBytes))
					continue
				}
				for _, schema := range schemas {
					if ctx.Err() != nil {
						return
					}
					checkJobQueueSaturation(schema)
				}
			}
		}
	}()
}

func queueSaturationMessage(pending int, oldestReadyAge time.Duration, depthLimit int, waitLimit time.Duration) string {
	if pending < depthLimit && oldestReadyAge < waitLimit {
		return ""
	}
	return fmt.Sprintf("async-job queue backlog: %d pending; oldest ready job %.0fs (limits %d jobs / %s). Inspect Async Jobs for failed or leased work and restore worker/database capacity.",
		pending, oldestReadyAge.Seconds(), depthLimit, waitLimit)
}

func checkJobQueueSaturation(schema string) {
	depthLimit := GetSettingIntForSchema(schema, "ops.async_job_queue_depth_alert")
	waitLimit := time.Duration(GetSettingIntForSchema(schema, "ops.async_job_queue_wait_seconds_alert")) * time.Second
	if depthLimit <= 0 || waitLimit <= 0 {
		return
	}
	var pending int
	var oldestSeconds float64
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*)::int,
		       COALESCE(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP - MIN(next_attempt_at)), 0)
		FROM %s.async_jobs
		WHERE status = 'Pending' AND next_attempt_at <= CURRENT_TIMESTAMP`, schema)).Scan(&pending, &oldestSeconds)
	if err != nil {
		log.Printf("[QUEUE-MONITOR] unable to inspect tenant queue %s: %s", schema, redactAndBoundLogField(err.Error(), maxSystemErrorMessageBytes))
		return
	}
	message := queueSaturationMessage(pending, time.Duration(oldestSeconds*float64(time.Second)), depthLimit, waitLimit)
	queueAlertState.Lock()
	last := queueAlertState.lastAlertAt[schema]
	shouldAlert := message != "" && (last.IsZero() || time.Since(last) >= queueAlertCooldown)
	if shouldAlert {
		queueAlertState.lastAlertAt[schema] = time.Now()
	} else if message == "" {
		delete(queueAlertState.lastAlertAt, schema)
	}
	queueAlertState.Unlock()
	if shouldAlert {
		SendOpsAlert("QUEUE_BACKLOG", schema, message)
	}
}

// StartAlertMonitor polls system_error_logs per tenant schema and alerts
// once per cooldown window if the row count within `window` reaches
// `threshold`. Counts every logged error regardless of its severity label
// (call sites across this codebase use PANIC alongside module-specific
// labels like APPROVAL_RESET_FAILED - see engines/logs.go's LogSystemError
// callers - so filtering to a fixed severity set would miss real failures).
// This is the "sustained error rate" alert; a single PANIC still alerts
// immediately and separately via LogSystemError itself.
func StartAlertMonitor(ctx context.Context, pollInterval, window time.Duration, threshold int) {
	ticker := time.NewTicker(pollInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
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
		}
	}()
}

// BackupFreshness reports the age of the newest whole-database backup.
//
// Deliberately NOT surfaced over HTTP: the only status endpoint this server
// has (/api/v1/health) is public by design, and backup paths plus a backup
// schedule are exactly the reconnaissance an unauthenticated caller should not
// be handed. The alert channel is the delivery mechanism; this type is
// exported so a future authenticated admin screen can reuse the check rather
// than reimplement it.
type BackupFreshness struct {
	// Configured is false when there is no backup directory on this machine -
	// every dev box and CI runner. Callers must treat that as "not applicable"
	// rather than "stale", or local runs report a permanent false alarm.
	Configured bool      `json:"configured"`
	Dir        string    `json:"dir,omitempty"`
	Newest     time.Time `json:"newest,omitempty"`
	AgeHours   float64   `json:"age_hours,omitempty"`
	Found      bool      `json:"found"`
}

// backupDir resolves where deploy/backup.sh writes, using the same
// BACKUP_DIR-or-/opt/erp/backups default the script itself uses so the two
// cannot disagree about which directory is being watched.
func backupDir() string {
	if dir := os.Getenv("BACKUP_DIR"); dir != "" {
		return dir
	}
	return "/opt/erp/backups"
}

// CheckBackupFreshness stats the newest nightly backup. It looks only at
// `custom_erp_*.dump.enc` - the whole-database nightly - deliberately ignoring
// on-demand `tenant_*` exports (26.1.6), since one tenant export does not mean
// the nightly ran.
func CheckBackupFreshness() BackupFreshness {
	dir := backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		// No directory = not a machine that takes backups. Silent by design.
		return BackupFreshness{Configured: false, Dir: dir}
	}
	result := BackupFreshness{Configured: true, Dir: dir}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "custom_erp_") || !strings.HasSuffix(name, ".dump.enc") {
			continue
		}
		info, errInfo := entry.Info()
		if errInfo != nil {
			continue
		}
		if !result.Found || info.ModTime().After(result.Newest) {
			result.Found = true
			result.Newest = info.ModTime()
		}
	}
	if result.Found {
		result.AgeHours = time.Since(result.Newest).Hours()
	}
	return result
}

// StartBackupFreshnessMonitor alerts when the newest nightly backup is older
// than maxAge (Stage 43.2).
//
// This is the half deploy/backup.sh structurally cannot cover: that script
// alerts when a run starts and fails, but a cron entry that was never
// installed, or a job the scheduler never fired, produces no run and therefore
// no failure to report. 26.11.7 found exactly that on production - nightly
// backups had never been running at all, and the silence was
// indistinguishable from success. Watching the artifact's age instead of the
// job's exit status is what makes absence detectable.
//
// No-ops entirely where no backup directory exists, so dev machines and CI
// stay quiet; and no-ops in delivery (logging only) until
// OPS_ALERT_WEBHOOK_URL is set, matching SendOpsAlert.
func StartBackupFreshnessMonitor(ctx context.Context, pollInterval, maxAge time.Duration) {
	if fresh := CheckBackupFreshness(); !fresh.Configured {
		log.Printf("[BACKUP-MONITOR] no backup directory at %s - monitor disabled on this machine", fresh.Dir)
		return
	}
	ticker := time.NewTicker(pollInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkBackupAge(maxAge)
			}
		}
	}()
}

// backupAlertState throttles the stale-backup alert to one per maxAge window.
// Without it a stale backup would alert on every poll tick, and an alert
// channel that cries every minute is one people mute - which would defeat the
// point of wiring it up at all.
var backupAlertState = struct {
	sync.Mutex
	lastAlertAt time.Time
}{}

func checkBackupAge(maxAge time.Duration) {
	fresh := CheckBackupFreshness()
	if !fresh.Configured {
		return
	}
	var message string
	switch {
	case !fresh.Found:
		// BLD-052: deliberately does NOT interpolate fresh.Dir. This message
		// is delivered to an external chat webhook, and the backup directory
		// is an absolute host path (found by the incident drill, which caught
		// the full "C:\Users\...\AppData\Local\Temp\..." path arriving in the
		// payload). The responder configured BACKUP_DIR and does not learn
		// anything from being told it back; a third party reading the channel
		// learns the host's filesystem layout. The path is still available
		// locally via CheckBackupFreshness for anyone diagnosing on the box.
		message = "no nightly backup found in the configured backup directory at all - the backup cron is not producing files"
	case time.Since(fresh.Newest) > maxAge:
		message = fmt.Sprintf("newest nightly backup is %.1fh old (limit %s), taken %s", fresh.AgeHours, maxAge, fresh.Newest.UTC().Format(time.RFC3339))
	default:
		return
	}

	backupAlertState.Lock()
	shouldAlert := backupAlertState.lastAlertAt.IsZero() || time.Since(backupAlertState.lastAlertAt) > maxAge
	if shouldAlert {
		backupAlertState.lastAlertAt = time.Now()
	}
	backupAlertState.Unlock()

	if shouldAlert {
		SendOpsAlert("BACKUP_STALE", "backup-monitor", message)
	}
}

func checkErrorRate(schema string, window time.Duration, threshold int) {
	cutoff := time.Now().Add(-window)
	var count int
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.system_error_logs WHERE created_at > ($1::timestamptz AT TIME ZONE current_setting('TimeZone'))`, schema), cutoff).Scan(&count)
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
