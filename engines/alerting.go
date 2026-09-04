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
	webhookURL := os.Getenv("OPS_ALERT_WEBHOOK_URL")
	if webhookURL == "" {
		// OBS-0215 (Stage 25.5): "Alert webhook missing" - exactly this
		// no-op path. Log-only (there's no HTTP request/tenant context at
		// the point most callers of SendOpsAlert fire from - background
		// workers, panic recovery - to attach a coded API response to).
		log.Printf("[OBS-0215] (no OPS_ALERT_WEBHOOK_URL configured, not sent) [%s] %s: %s", severity, source, message)
		return
	}
	if !ExternalSideEffectsEnabled() {
		// Stage 47.0.5/47.11.6 Gate 0: OPS_ALERT_WEBHOOK_URL configured
		// against a shared dev/staging channel must not actually post just
		// because a regression/abuse test or a developer triggered this path.
		log.Printf("[OPS-ALERT] (external side effects OFF - not sent) [%s] %s: %s", severity, source, truncateForAlert(message))
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
		message = fmt.Sprintf("no nightly backup found in %s at all - the backup cron is not producing files", fresh.Dir)
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
