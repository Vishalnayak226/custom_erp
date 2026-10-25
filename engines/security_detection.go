package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
)

// Stage 49.11.4 / 49.11.5: actionable detections over the security events the
// audit log already holds (49.11.1's taxonomy). No second event store and no
// SIEM: one bounded read of the last hour per client per sweep, the rules
// evaluated in Go, and a fired detection written back as ONE `SECURITY_ALERT`
// audit row. That row is at once the review queue, the dedup record and the
// evidence pointer - it names the matching rows' action, actor, window and
// correlation ids rather than copying them (49.18.3).
//
// Severity decides delivery, not colour: URGENT also pages through
// SendOpsAlert; REVIEW only queues for the client's administrator
// (Activity Log -> Security). Every rule names an owner, a first response
// and the runbook section to open, so an alert is never just a red word.

const (
	DetectionUrgent     = "URGENT"
	DetectionReview     = "REVIEW"
	SecurityAlertAction = "SECURITY_ALERT"
	// securityDetectionReadCap bounds the one read per client per sweep. A
	// burst bigger than this is itself far past every threshold below.
	securityDetectionReadCap = 5000
)

// SecurityDetectionRule is one tested detection (49.11.4) with its alert
// engineering (49.11.5): severity, owner, threshold, window, dedup window and
// first response.
type SecurityDetectionRule struct {
	Key           string `json:"key"`
	Title         string `json:"title"`
	Severity      string `json:"severity"`
	Owner         string `json:"owner"`
	Threshold     int    `json:"threshold"`
	WindowMinutes int    `json:"window_minutes"`
	DedupMinutes  int    `json:"dedup_minutes"`
	FirstResponse string `json:"first_response"`
	Runbook       string `json:"runbook"`

	// Rules counted from the audit rows read once per sweep.
	actions  []string
	statuses []string // empty = any status
	byActor  bool     // one count per actor, else one per client
	// Rules that need their own query (disabled users, audit evidence).
	special func(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error)
}

type detectionHit struct {
	Subject      string
	Count        int
	Correlations []string
}

// SecurityDetection is one fired detection.
type SecurityDetection struct {
	Rule     string `json:"rule"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Subject  string `json:"subject"`
	Count    int    `json:"count"`
	Details  string `json:"details"`
}

const runbookDoc = "docs/operations/incident_runbook.md"

var securityDetectionRules = []SecurityDetectionRule{
	{Key: "password_spray", Title: "Password spray or distributed guessing", Severity: DetectionUrgent,
		Owner: "Client administrator", Threshold: 1, WindowMinutes: 15, DedupMinutes: 60,
		FirstResponse: "Read the sign-in rows it names; make sure each targeted account has 2FA; sign out everywhere any account that may be known.",
		Runbook:       runbookDoc + " §7.1", actions: []string{"LOGIN_ABUSE"}},
	{Key: "failed_signin_burst", Title: "Many wrong passwords", Severity: DetectionReview,
		Owner: "Client administrator", Threshold: 25, WindowMinutes: 15, DedupMinutes: 60,
		FirstResponse: "Check whether one person forgot a password or many accounts are being tried; the latter is §7.1.",
		Runbook:       runbookDoc + " §7.1", actions: []string{"USERAC-0021"}},
	{Key: "account_lockouts", Title: "Several accounts locked", Severity: DetectionReview,
		Owner: "Client administrator", Threshold: 3, WindowMinutes: 60, DedupMinutes: 120,
		FirstResponse: "Ask the locked users whether they mistyped; unlock only after they confirm.",
		Runbook:       runbookDoc + " §7.1", actions: []string{"LOGIN"}, statuses: []string{"ACCOUNT_LOCKED"}},
	{Key: "access_denied_burst", Title: "One user refused again and again", Severity: DetectionReview,
		Owner: "Client administrator", Threshold: 10, WindowMinutes: 10, DedupMinutes: 60,
		FirstResponse: "Ask the user what they were doing; a role that is too narrow looks the same as someone probing other records.",
		Runbook:       runbookDoc + " §7.3", actions: []string{"GLOBAL-0011"}, byActor: true},
	{Key: "other_client_session", Title: "A session from another client used here", Severity: DetectionUrgent,
		Owner: "Platform operator", Threshold: 1, WindowMinutes: 60, DedupMinutes: 60,
		FirstResponse: "The request was refused. Find the session's own client in the details and sign that user out everywhere.",
		Runbook:       runbookDoc + " §7.3", actions: []string{"TENANT_ROUTING"}, statuses: []string{"REFUSED"}},
	{Key: "privilege_change_burst", Title: "Many role or user changes by one person", Severity: DetectionReview,
		Owner: "Client owner", Threshold: 5, WindowMinutes: 10, DedupMinutes: 60,
		FirstResponse: "Confirm the changes with the person who made them; undo any you did not expect and sign that admin out everywhere.",
		Runbook:       runbookDoc + " §7.2", actions: []string{"ROLE_PERMISSIONS", "USER_MANAGEMENT"}, byActor: true},
	{Key: "export_burst", Title: "Many exports by one user", Severity: DetectionUrgent,
		Owner: "Client administrator", Threshold: 20, WindowMinutes: 10, DedupMinutes: 60,
		FirstResponse: "Ask the user; if it is not expected, freeze exports and sign the user out everywhere.",
		Runbook:       runbookDoc + " §7.6", actions: []string{"DATA_EXPORT"}, statuses: []string{"SUCCESS"}, byActor: true},
	{Key: "till_reversal_burst", Title: "Many cancels, voids or price overrides by one user", Severity: DetectionReview,
		Owner: "Store manager", Threshold: 10, WindowMinutes: 60, DedupMinutes: 240,
		FirstResponse: "Review the bills with the cashier and their manager before the till closes.",
		Runbook:       runbookDoc + " §7.7", actions: []string{"POS_BILL_CANCELLED", "POS_SALE_VOIDED", "POS_PRICE_OVERRIDE"}, byActor: true},
	{Key: "job_dead_letter_storm", Title: "Background jobs giving up", Severity: DetectionReview,
		Owner: "Platform operator", Threshold: 5, WindowMinutes: 60, DedupMinutes: 240,
		FirstResponse: "Open Activity Log -> Async Jobs; fix the cause before retrying, never retry a poison job in a loop.",
		Runbook:       runbookDoc + " §7.5", actions: []string{"JOB"}, statuses: []string{"DEAD_LETTERED"}},
	{Key: "disabled_user_activity", Title: "A disabled user acted after being disabled", Severity: DetectionUrgent,
		Owner: "Platform operator", Threshold: 1, WindowMinutes: 15, DedupMinutes: 60,
		FirstResponse: "A disabled session should be dead. Sign the user out everywhere and treat it as a control failure.",
		Runbook:       runbookDoc + " §7.2", special: detectDisabledUserActivity},
	{Key: "audit_verify_failed", Title: "Audit evidence failed or could not be verified", Severity: DetectionUrgent,
		Owner: "Platform operator", Threshold: 1, WindowMinutes: 120, DedupMinutes: 240,
		FirstResponse: "Preserve the database as it is (backup first), then read the verifier's row ids; do not repair rows by hand.",
		Runbook:       runbookDoc + " §7.8", special: detectAuditVerifyFailure},
	{Key: "audit_checkpoint_overdue", Title: "Audit checkpoints stopped while events waited", Severity: DetectionUrgent,
		Owner: "Platform operator", Threshold: 1, WindowMinutes: 180, DedupMinutes: 240,
		FirstResponse: "The hourly checkpoint job is not running: check the job runner and the scheduler leader. A silent verifier is a failed control.",
		Runbook:       runbookDoc + " §7.8", special: detectAuditCheckpointOverdue},
}

// SecurityDetectionRules returns the rule catalog (for the Security pane and
// the drill test).
func SecurityDetectionRules() []SecurityDetectionRule {
	out := make([]SecurityDetectionRule, len(securityDetectionRules))
	copy(out, securityDetectionRules)
	return out
}

// utcStamp formats t the way LogAuditEvent stores created_at: naive UTC. Only
// signed rows (LogAuditEvent / WriteAuditEvidenceTx) are compared with it -
// the trigger-written rows are session-local and none of them is watched.
func utcStamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.999999")
}

type watchedAuditRow struct {
	actor, action, status, correlation string
	at                                 time.Time
}

// RunSecurityDetections evaluates every rule for one client at now, fires the
// new detections and returns them. Safe to call repeatedly: a detection whose
// rule and subject already fired inside the rule's dedup window is skipped,
// and the record of that is the SECURITY_ALERT row itself, so dedup survives
// a restart.
func RunSecurityDetections(tenantID string, now time.Time) ([]SecurityDetection, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := readWatchedAuditRows(schema, now)
	if err != nil {
		return nil, err
	}
	var fired []SecurityDetection
	for _, rule := range securityDetectionRules {
		var hits []detectionHit
		if rule.special != nil {
			if hits, err = rule.special(schema, now, rule); err != nil {
				log.Printf("[SECURITY-DETECT] %s %s: %v", schema, rule.Key, err)
				continue
			}
		} else {
			hits = countWatchedRows(rows, rule, now)
		}
		for _, hit := range hits {
			if hit.Count < rule.Threshold {
				continue
			}
			det, isNew, err := fireDetection(tenantID, schema, rule, hit, now)
			if err != nil {
				log.Printf("[SECURITY-DETECT] %s %s: %v", schema, rule.Key, err)
				continue
			}
			if isNew {
				fired = append(fired, det)
			}
		}
	}
	return fired, nil
}

func readWatchedAuditRows(schema string, now time.Time) ([]watchedAuditRow, error) {
	maxWindow := 0
	actionSet := map[string]bool{}
	for _, rule := range securityDetectionRules {
		if rule.special != nil {
			continue
		}
		if rule.WindowMinutes > maxWindow {
			maxWindow = rule.WindowMinutes
		}
		for _, a := range rule.actions {
			actionSet[a] = true
		}
	}
	actions := make([]string, 0, len(actionSet))
	for a := range actionSet {
		actions = append(actions, a)
	}
	sort.Strings(actions)
	since := now.Add(-time.Duration(maxWindow) * time.Minute)
	res, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(user_id, ''), action, COALESCE(status, ''), created_at, COALESCE(correlation_id, '')
		FROM %s.audit_logs
		WHERE created_at > $1::timestamp AND action = ANY($2) AND signature IS NOT NULL
		ORDER BY created_at DESC LIMIT %d`, schema, securityDetectionReadCap),
		utcStamp(since), pq.Array(actions))
	if err != nil {
		return nil, err
	}
	defer res.Close()
	var out []watchedAuditRow
	for res.Next() {
		var r watchedAuditRow
		if err := res.Scan(&r.actor, &r.action, &r.status, &r.at, &r.correlation); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, res.Err()
}

func countWatchedRows(rows []watchedAuditRow, rule SecurityDetectionRule, now time.Time) []detectionHit {
	since := now.Add(-time.Duration(rule.WindowMinutes) * time.Minute).UTC()
	bySubject := map[string]*detectionHit{}
	var order []string
	for _, r := range rows {
		if !r.at.After(since) || !containsString(rule.actions, r.action) {
			continue
		}
		if len(rule.statuses) > 0 && !containsString(rule.statuses, r.status) {
			continue
		}
		subject := "client"
		if rule.byActor {
			subject = r.actor
			if subject == "" {
				subject = "(no user)"
			}
		}
		h := bySubject[subject]
		if h == nil {
			h = &detectionHit{Subject: subject}
			bySubject[subject] = h
			order = append(order, subject)
		}
		h.Count++
		if r.correlation != "" && len(h.Correlations) < 3 {
			h.Correlations = append(h.Correlations, r.correlation)
		}
	}
	out := make([]detectionHit, 0, len(order))
	for _, s := range order {
		out = append(out, *bySubject[s])
	}
	return out
}

// detectDisabledUserActivity: a signed event by a user whose account is not
// Active, written after the audit row that disabled them. A disabled user's
// session is refused by the live user-state check, so any hit is a control
// failure, not a user mistake. Their failed sign-ins are left out: trying to
// sign in after being disabled is expected.
func detectDisabledUserActivity(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error) {
	since := now.Add(-time.Duration(rule.WindowMinutes) * time.Minute)
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT u.username, COUNT(*),
		       COALESCE((array_agg(a.correlation_id) FILTER (WHERE a.correlation_id IS NOT NULL))[1:3], '{}')
		FROM %[1]s.audit_logs a
		JOIN %[1]s.users u ON (a.user_id = u.username OR a.user_id = u.id::text)
		WHERE a.created_at > $1::timestamp AND a.signature IS NOT NULL
		  AND COALESCE(u.status, '') <> 'Active'
		  AND a.action NOT IN ('USERAC-0021', 'LOGIN', 'SECURITY_ALERT')
		  AND a.created_at > COALESCE((
		        SELECT MAX(s.created_at) FROM %[1]s.audit_logs s
		        WHERE s.action = 'USER_MANAGEMENT' AND s.status = 'USER_STATUS_CHANGED'
		          AND s.details = 'Set user ' || u.id::text || ' status to ' || u.status), 'infinity'::timestamp)
		GROUP BY u.username`, schema), utcStamp(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []detectionHit
	for rows.Next() {
		var h detectionHit
		if err := rows.Scan(&h.Subject, &h.Count, pq.Array(&h.Correlations)); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// detectAuditVerifyFailure: the scheduled verifier logs CRITICAL under
// module "audit.verify" both when evidence fails and when it could not run.
func detectAuditVerifyFailure(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error) {
	since := now.Add(-time.Duration(rule.WindowMinutes) * time.Minute)
	var n int
	// system_error_logs.created_at is session-local (column default), unlike
	// the signed audit rows - compare the way checkErrorRate does.
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.system_error_logs
		WHERE module_source = 'audit.verify' AND created_at > ($1::timestamptz AT TIME ZONE current_setting('TimeZone'))`, schema),
		since).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	return []detectionHit{{Subject: "audit evidence", Count: n}}, nil
}

// detectAuditCheckpointOverdue is 47.7.7's "alert on MISSING verification":
// signed rows older than the window that no checkpoint covers yet. An idle
// client writes no rows and so is never overdue - the old "no checkpoint for
// N hours" test would page every quiet client.
func detectAuditCheckpointOverdue(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error) {
	oldest, err := oldestUnsealedAuditRow(schema)
	if err != nil || oldest == nil || !oldest.Before(now.Add(-time.Duration(rule.WindowMinutes)*time.Minute)) {
		return nil, err
	}
	hours := int(now.Sub(*oldest).Hours())
	return []detectionHit{{Subject: "audit evidence", Count: max(hours, 1)}}, nil
}

// oldestUnsealedAuditRow is the time of the first signed row after the last
// checkpoint, or nil when every signed row is sealed. One index step on seq,
// not a count: a client that never checkpointed has every row unsealed.
func oldestUnsealedAuditRow(schema string) (*time.Time, error) {
	var at time.Time
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT created_at FROM %[1]s.audit_logs
		WHERE seq > COALESCE((SELECT MAX(to_seq) FROM %[1]s.audit_checkpoints), 0) AND signature IS NOT NULL
		ORDER BY seq LIMIT 1`, schema)).Scan(&at)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &at, nil
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func detectionPrefix(rule, subject string) string {
	return "rule=" + rule + " subject=" + subject + ":"
}

func fireDetection(tenantID, schema string, rule SecurityDetectionRule, hit detectionHit, now time.Time) (SecurityDetection, bool, error) {
	prefix := detectionPrefix(rule.Key, hit.Subject)
	var exists bool
	dedupSince := now.Add(-time.Duration(rule.DedupMinutes) * time.Minute)
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.audit_logs
		WHERE action = $1 AND details LIKE $2 AND created_at > $3::timestamp)`, schema),
		SecurityAlertAction, likeEscape(prefix)+"%", utcStamp(dedupSince)).Scan(&exists); err != nil {
		return SecurityDetection{}, false, err
	}
	if exists {
		return SecurityDetection{}, false, nil
	}
	evidence := ""
	if len(rule.actions) > 0 {
		evidence = " Evidence: audit rows " + strings.Join(rule.actions, "/")
		if len(rule.statuses) > 0 {
			evidence += " " + strings.Join(rule.statuses, "/")
		}
		if rule.byActor {
			evidence += " by " + hit.Subject
		}
		evidence += " since " + now.Add(-time.Duration(rule.WindowMinutes)*time.Minute).UTC().Format("2006-01-02 15:04") + " UTC."
	}
	if len(hit.Correlations) > 0 {
		evidence += " Correlation " + strings.Join(hit.Correlations, ", ") + "."
	}
	details := fmt.Sprintf("%s %d in %d min - %s. Owner: %s. First response: %s Runbook: %s.%s",
		prefix, hit.Count, rule.WindowMinutes, rule.Title, rule.Owner, rule.FirstResponse, rule.Runbook, evidence)
	LogAuditEvent(tenantID, "system", SecurityAlertAction, rule.Severity, details)
	if rule.Severity == DetectionUrgent {
		SendOpsAlert("SECURITY_DETECTION", schema, rule.Title+" ("+hit.Subject+"): "+rule.FirstResponse)
	}
	return SecurityDetection{Rule: rule.Key, Title: rule.Title, Severity: rule.Severity,
		Subject: hit.Subject, Count: hit.Count, Details: details}, true, nil
}

// securitySweepState records when the sweep last finished, so the Security
// pane can show a detector that stopped - silence must be visible (49.11.5).
var securitySweepState struct {
	sync.Mutex
	last time.Time
}

// LastSecuritySweep is when this server last finished a detection sweep
// (zero if never, or if another server is the scheduler leader).
func LastSecuritySweep() time.Time {
	securitySweepState.Lock()
	defer securitySweepState.Unlock()
	return securitySweepState.last
}

// StartSecurityDetectionSweep runs every rule for every client on interval.
func StartSecurityDetectionSweep(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() || db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[SECURITY-DETECT] failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					if _, err := RunSecurityDetections(schemaToTenantID(schema), time.Now()); err != nil {
						log.Printf("[SECURITY-DETECT] %s: %v", schema, err)
					}
				}
				securitySweepState.Lock()
				securitySweepState.last = time.Now()
				securitySweepState.Unlock()
			}
		}
	}()
}
